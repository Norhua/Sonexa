package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/norhua/sonexa/internal/provider/netease"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type Searcher interface {
	Search(context.Context, provider.Query) ([]Result, error)
}

type WebSource struct {
	ID        string
	HTTP      *http.Client
	Limiters  *provider.Limiters
	UserAgent string
	Gate      func() error
	Retry     provider.RetryPolicy
}

func NewWebSource(id, userAgent string, limiters *provider.Limiters, gate func() error) *WebSource {
	limiters.Configure(provider.ProviderID("cover-"+id), 1, time.Second)
	return &WebSource{ID: id, HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return fmt.Errorf("不安全的来源重定向")
		}
		if req.URL.Hostname() != "u.y.qq.com" && req.URL.Hostname() != "music.163.com" {
			return fmt.Errorf("来源重定向超出允许域名")
		}
		if gate != nil {
			return gate()
		}
		return nil
	}}, Limiters: limiters, UserAgent: userAgent, Gate: gate, Retry: provider.TransportRetryPolicy()}
}

func (s *WebSource) Search(ctx context.Context, q provider.Query) ([]Result, error) {
	var last error
	for attempt := 1; attempt <= max(s.Retry.MaxAttempts, 1); attempt++ {
		results, err := s.searchOnce(ctx, q)
		if err == nil {
			if len(results) > MaxResults {
				results = results[:MaxResults]
			}
			return results, nil
		}
		last = err
		if !provider.IsRetryable(err) || attempt >= s.Retry.MaxAttempts {
			break
		}
		timer := time.NewTimer(s.Retry.JitteredBackoff(attempt, err))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, last
}

// searchQuery 是 QQ/网易云按歌曲搜索时的检索词：歌曲名优先，缺失时退回专辑名。
// 专辑搜索会把歌曲名当作专辑名的一部分而全部落空，因此两家都按歌曲搜索后按专辑聚合。
func searchQuery(q provider.Query) string {
	head := strings.TrimSpace(q.Title)
	if head == "" {
		head = strings.TrimSpace(q.Album)
	}
	return strings.TrimSpace(strings.Join([]string{head, strings.TrimSpace(q.Artist)}, " "))
}

func (s *WebSource) searchOnce(ctx context.Context, q provider.Query) ([]Result, error) {
	term := searchQuery(q)
	if term == "" {
		return nil, fmt.Errorf("请输入封面搜索条件")
	}
	if s.Gate != nil {
		if err := s.Gate(); err != nil {
			return nil, err
		}
	}
	if err := s.Limiters.Wait(ctx, provider.ProviderID("cover-"+s.ID)); err != nil {
		return nil, err
	}
	// 歌曲级结果按专辑去重后远少于条数，多取一些再过滤。
	const fetchLimit = 30
	var req *http.Request
	var err error
	switch s.ID {
	case "qq":
		payload, _ := json.Marshal(map[string]any{
			"comm": map[string]any{"ct": "19", "cv": "1859", "uin": "0"},
			"req": map[string]any{"module": "music.search.SearchCgiService", "method": "DoSearchForQQMusicDesktop",
				"param": map[string]any{"query": term, "search_type": 0, "num_per_page": fetchLimit, "page_num": 1}},
		})
		req, err = http.NewRequestWithContext(ctx, "POST", "https://u.y.qq.com/cgi-bin/musicu.fcg", bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	case "163":
		payload, _ := json.Marshal(map[string]string{"s": term, "type": "1", "limit": strconv.Itoa(fetchLimit), "offset": "0"})
		var form url.Values
		form, err = netease.Form(payload)
		if err == nil {
			req, err = http.NewRequestWithContext(ctx, "POST", "https://music.163.com/weapi/cloudsearch/pc", strings.NewReader(form.Encode()))
		}
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	default:
		return nil, fmt.Errorf("不支持的封面来源")
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.UserAgent)
	if s.ID == "qq" {
		req.Header.Set("Referer", "https://y.qq.com/")
	} else {
		req.Header.Set("Referer", "https://music.163.com/")
	}
	if s.Gate != nil {
		if err := s.Gate(); err != nil {
			return nil, err
		}
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		delay := time.Duration(seconds) * time.Second
		if when, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && time.Until(when) > delay {
			delay = time.Until(when)
		}
		return nil, &provider.HTTPError{StatusCode: resp.StatusCode, Status: fmt.Sprintf("%s HTTP %d", s.ID, resp.StatusCode), RetryAfter: delay}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return nil, fmt.Errorf("来源响应不可读或过大")
	}
	var results []Result
	if s.ID == "qq" {
		results, err = ParseQQ(body, q)
	} else {
		results, err = Parse163(body, q)
	}
	if err == nil && q.Limit > 0 && len(results) > q.Limit {
		results = results[:q.Limit]
	}
	return results, err
}

func ParseQQ(body []byte, q provider.Query) ([]Result, error) {
	var response struct {
		Code *int `json:"code"`
		Req  *struct {
			Code *int `json:"code"`
			Data *struct {
				Body *struct {
					Song *struct {
						List []struct {
							Name  string `json:"name"`
							Album struct {
								MID  string `json:"mid"`
								Name string `json:"name"`
							} `json:"album"`
							Singer []struct {
								Name string `json:"name"`
							} `json:"singer"`
						} `json:"list"`
					} `json:"song"`
				} `json:"body"`
			} `json:"data"`
		} `json:"req"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	r := response.Req
	if response.Code == nil || *response.Code != 0 || r == nil || r.Code == nil || *r.Code != 0 || r.Data == nil || r.Data.Body == nil ||
		r.Data.Body.Song == nil || r.Data.Body.Song.List == nil {
		return nil, fmt.Errorf("QQ 搜索响应异常或来源不可用")
	}
	out := []Result{}
	seen := map[string]bool{}
	for _, song := range r.Data.Body.Song.List {
		mid := song.Album.MID
		if mid == "" || seen[mid] {
			continue
		}
		for _, c := range mid {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
				return nil, fmt.Errorf("QQ 专辑身份无效")
			}
		}
		artists := []string{}
		for _, singer := range song.Singer {
			artists = append(artists, singer.Name)
		}
		artist := strings.Join(artists, " / ")
		if !Relevant(q, song.Name, artist, song.Album.Name) {
			continue
		}
		seen[mid] = true
		images := qqmusic.CoverImages(mid, song.Album.Name, artist)
		if images == nil {
			continue
		}
		out = append(out, Result{Input: &domain.CoverCatalogInput{Key: store.CoverCatalogKey{Source: "qq", ObjectType: "album", SourceRef: mid}, Images: images}})
	}
	return out, nil
}

func Parse163(body []byte, q provider.Query) ([]Result, error) {
	var response struct {
		Code   int `json:"code"`
		Result *struct {
			SongCount *int `json:"songCount"`
			Songs     []struct {
				Name string `json:"name"`
				Al   struct {
					ID     json.Number `json:"id"`
					Name   string      `json:"name"`
					PicURL string      `json:"picUrl"`
				} `json:"al"`
				Ar []struct {
					Name string `json:"name"`
				} `json:"ar"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Code != 200 || response.Result == nil || (response.Result.Songs == nil && (response.Result.SongCount == nil || *response.Result.SongCount != 0)) {
		return nil, fmt.Errorf("网易云搜索响应异常或来源不可用")
	}
	out := []Result{}
	seen := map[string]bool{}
	for _, song := range response.Result.Songs {
		album := song.Al
		if album.PicURL == "" {
			continue
		}
		artists := []string{}
		for _, a := range song.Ar {
			artists = append(artists, a.Name)
		}
		artist := strings.Join(artists, " / ")
		if !Relevant(q, song.Name, artist, album.Name) {
			continue
		}
		u, err := url.Parse(album.PicURL)
		if err != nil || (!strings.HasSuffix(u.Hostname(), ".music.126.net") && u.Hostname() != "music.126.net") {
			continue
		}
		// 用户上传的歌曲专辑 id 为 0，此时以图片 id（地址末段）作为来源身份。
		ref := album.ID.String()
		if ref == "" || ref == "0" {
			ref = "pic-" + strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path))
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		u.Scheme = "https"
		u.RawQuery = ""
		image := store.CoverImage{SourceImageID: ref, Title: album.Name, Artist: artist, PrimaryType: "Front", Types: []string{"Front"}, Front: true, OriginalURL: u.String()}
		image.Variants = append(image.Variants, store.CoverVariant{Spec: "original", URL: u.String()})
		for _, size := range []int{250, 500} {
			resized := *u
			resized.RawQuery = url.Values{"param": {fmt.Sprintf("%dy%d", size, size)}}.Encode()
			image.Variants = append(image.Variants, store.CoverVariant{Spec: strconv.Itoa(size), URL: resized.String(), Constructed: true})
		}
		out = append(out, Result{Input: &domain.CoverCatalogInput{Key: store.CoverCatalogKey{Source: "163", ObjectType: "album", SourceRef: ref}, Images: []store.CoverImage{image}}})
	}
	return out, nil
}
