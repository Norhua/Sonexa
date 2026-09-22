package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type Result struct {
	Key    store.CoverCatalogKey
	Images []store.CoverImage
}

type WebSource struct {
	ID        string
	HTTP      *http.Client
	Limiters  *provider.Limiters
	UserAgent string
	Gate      func() error
}

func NewWebSource(id, userAgent string, limiters *provider.Limiters, gate func() error) *WebSource {
	limiters.Configure(provider.ProviderID("cover-"+id), 1, time.Second)
	return &WebSource{ID: id, HTTP: &http.Client{Timeout: 30 * time.Second}, Limiters: limiters, UserAgent: userAgent, Gate: gate}
}

func (s *WebSource) Search(ctx context.Context, q provider.Query) ([]Result, error) {
	term := strings.TrimSpace(strings.Join([]string{q.Title, q.Artist, q.Album}, " "))
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
	limit := q.Limit
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	var req *http.Request
	var err error
	switch s.ID {
	case "qq":
		params := url.Values{"w": {term}, "format": {"json"}, "p": {"1"}, "n": {strconv.Itoa(limit)}, "t": {"0"}}
		req, err = http.NewRequestWithContext(ctx, "GET", "https://c.y.qq.com/soso/fcgi-bin/client_search_cp?"+params.Encode(), nil)
	case "163":
		params := url.Values{"s": {term}, "type": {"10"}, "limit": {strconv.Itoa(limit)}, "offset": {"0"}, "total": {"true"}}
		req, err = http.NewRequestWithContext(ctx, "POST", "https://music.163.com/api/search/get/web", strings.NewReader(params.Encode()))
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
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s 搜索返回 HTTP %d", s.ID, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return nil, fmt.Errorf("来源响应不可读或过大")
	}
	if s.ID == "qq" {
		return ParseQQ(body)
	}
	return Parse163(body)
}

func ParseQQ(body []byte) ([]Result, error) {
	var response struct {
		Code *int `json:"code"`
		Data *struct {
			Song *struct {
				List []struct {
					AlbumMID  string `json:"albummid"`
					AlbumName string `json:"albumname"`
					Singer    []struct {
						Name string `json:"name"`
					} `json:"singer"`
				} `json:"list"`
			} `json:"song"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Code == nil || *response.Code != 0 || response.Data == nil || response.Data.Song == nil || response.Data.Song.List == nil {
		return nil, fmt.Errorf("QQ 搜索响应异常或来源不可用")
	}
	out := []Result{}
	seen := map[string]bool{}
	for _, song := range response.Data.Song.List {
		if song.AlbumMID == "" || seen[song.AlbumMID] {
			continue
		}
		seen[song.AlbumMID] = true
		for _, r := range song.AlbumMID {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				return nil, fmt.Errorf("QQ 专辑身份无效")
			}
		}
		artists := []string{}
		for _, singer := range song.Singer {
			artists = append(artists, singer.Name)
		}
		image := store.CoverImage{SourceImageID: song.AlbumMID, PrimaryType: "Front", Types: []string{"Front"}, Front: true, Title: song.AlbumName, Artist: strings.Join(artists, " / ")}
		for _, size := range []int{300, 500, 800} {
			image.Variants = append(image.Variants, store.CoverVariant{Spec: strconv.Itoa(size), Constructed: true,
				URL: fmt.Sprintf("https://y.qq.com/music/photo_new/T002R%dx%dM000%s.jpg", size, size, song.AlbumMID)})
		}
		out = append(out, Result{Key: store.CoverCatalogKey{Source: "qq", ObjectType: "album", SourceRef: song.AlbumMID}, Images: []store.CoverImage{image}})
	}
	return out, nil
}

func Parse163(body []byte) ([]Result, error) {
	var response struct {
		Code   int `json:"code"`
		Result *struct {
			AlbumCount *int `json:"albumCount"`
			Albums     []struct {
				ID     json.Number `json:"id"`
				Name   string      `json:"name"`
				PicURL string      `json:"picUrl"`
				Artist struct {
					Name string `json:"name"`
				} `json:"artist"`
			} `json:"albums"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Code != 200 || response.Result == nil || (response.Result.Albums == nil && (response.Result.AlbumCount == nil || *response.Result.AlbumCount != 0)) {
		return nil, fmt.Errorf("网易云搜索响应异常或来源不可用")
	}
	out := []Result{}
	seen := map[string]bool{}
	for _, album := range response.Result.Albums {
		if album.ID == "" || album.PicURL == "" || seen[album.ID.String()] {
			continue
		}
		seen[album.ID.String()] = true
		u, err := url.Parse(album.PicURL)
		if err != nil || (!strings.HasSuffix(u.Hostname(), ".music.126.net") && u.Hostname() != "music.126.net") {
			return nil, fmt.Errorf("网易云封面地址无效")
		}
		u.Scheme = "https"
		image := store.CoverImage{SourceImageID: album.ID.String(), Title: album.Name, Artist: album.Artist.Name, PrimaryType: "Front", Types: []string{"Front"}, Front: true, OriginalURL: u.String()}
		image.Variants = append(image.Variants, store.CoverVariant{Spec: "original", URL: u.String()})
		for _, size := range []int{250, 500} {
			resized := *u
			q := resized.Query()
			q.Set("param", fmt.Sprintf("%dy%d", size, size))
			resized.RawQuery = q.Encode()
			image.Variants = append(image.Variants, store.CoverVariant{Spec: strconv.Itoa(size), URL: resized.String(), Constructed: true})
		}
		out = append(out, Result{Key: store.CoverCatalogKey{Source: "163", ObjectType: "album", SourceRef: album.ID.String()}, Images: []store.CoverImage{image}})
	}
	return out, nil
}
