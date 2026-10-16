// Package lyrics provides explicit, track-scoped lyric search. Song matches are
// not lyric matches: text and format are only known after Fetch succeeds.
package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/netease"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

const MaxResults = 20
const MaxTextBytes = 2 << 20

type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func Sources() []Source {
	return []Source{{"163", "网易云音乐"}, {"qq", "QQ 音乐"}, {"kugou", "酷狗音乐"}, {"kuwo", "酷我音乐"}}
}
func ValidSource(id string) bool {
	for _, s := range Sources() {
		if s.ID == id {
			return true
		}
	}
	return false
}

type Query struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
}
type Candidate struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Ref        string `json:"-"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int    `json:"durationMs"`
}
type Document struct {
	Text   string `json:"text"`
	Format string `json:"format"`
}

type Service struct{ Client *provider.Client }

func (s Service) request(ctx context.Context, source, base, path string, params map[string]string, payload any) ([]byte, error) {
	id := provider.ProviderID("lyrics-" + source)
	if source == "qq" {
		id = provider.QQMusic
	}
	// 网易云与封面搜索共用桶；其他新来源默认 1 req/s。
	if source == "163" {
		id = provider.ProviderID("cover-163")
	}
	r := provider.GetRequest{Provider: id, BaseURL: base, Endpoint: path, Params: params, SkipCache: true, MaxResponseBytes: 4 << 20, Headers: map[string]string{}}
	switch source {
	case "163":
		r.Headers["Referer"] = "https://music.163.com/"
	case "qq":
		r.Headers["Referer"] = "https://y.qq.com/"
	case "kuwo":
		r.Headers["Referer"] = "https://www.kuwo.cn/"
	}
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		r.Body = data
		if source == "163" {
			form, err := netease.Form(data)
			if err != nil {
				return nil, err
			}
			r.Body = []byte(form.Encode())
			r.Headers["Content-Type"] = "application/x-www-form-urlencoded"
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	// Preserve shared transport/limiters without weakening TLS or allowing a
	// changed endpoint to redirect requests to arbitrary hosts.
	client := *s.Client
	httpClient := *s.Client.HTTP
	previousRedirect := httpClient.CheckRedirect
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		allowed := map[string][]string{
			"163": {"music.163.com"}, "qq": {"u.y.qq.com", "c.y.qq.com"},
			"kugou": {"mobileservice.kugou.com", "lyrics.kugou.com"},
			"kuwo":  {"search.kuwo.cn", "m.kuwo.cn"},
		}
		validHost := false
		for _, host := range allowed[source] {
			if req.URL.Host == host {
				validHost = true
			}
		}
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil || !validHost {
			return fmt.Errorf("歌词来源重定向超出允许范围")
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	client.HTTP = &httpClient
	return client.Get(ctx, r)
}
func (s Service) Search(ctx context.Context, source string, q Query) ([]Candidate, error) {
	if !ValidSource(source) {
		return nil, fmt.Errorf("未知歌词来源")
	}
	term := strings.TrimSpace(q.Title + " " + q.Artist)
	if term == "" {
		term = strings.TrimSpace(q.Album)
	}
	if term == "" {
		return nil, fmt.Errorf("请输入歌词搜索条件")
	}
	if len(term)+len(q.Album) > 2000 {
		return nil, fmt.Errorf("搜索条件过长")
	}
	var body []byte
	var err error
	switch source {
	case "163":
		body, err = s.request(ctx, source, "https://music.163.com", "/weapi/cloudsearch/pc", nil, map[string]any{"s": term, "type": 1, "limit": MaxResults, "offset": 0, "total": true, "csrf_token": ""})
	case "qq":
		body, err = s.request(ctx, source, "https://u.y.qq.com", "/cgi-bin/musicu.fcg", nil, map[string]any{"comm": map[string]string{"ct": "19", "cv": "1859", "uin": "0"}, "req": map[string]any{"module": "music.search.SearchCgiService", "method": "DoSearchForQQMusicDesktop", "param": map[string]any{"query": term, "search_type": 0, "num_per_page": MaxResults, "page_num": 1}}})
	case "kugou":
		body, err = s.request(ctx, source, "https://mobileservice.kugou.com", "/api/v3/search/song", map[string]string{"keyword": term, "page": "1", "pagesize": "20"}, nil)
	case "kuwo":
		body, err = s.request(ctx, source, "https://search.kuwo.cn", "/r.s", map[string]string{"all": term, "ft": "music", "pn": "0", "rn": "20", "rformat": "json", "encoding": "utf8", "pcjson": "1"}, nil)
	}
	if err != nil {
		return nil, err
	}
	return parseSearch(source, body, q.Album)
}
func (s Service) Fetch(ctx context.Context, c Candidate) (*Document, error) {
	var body []byte
	var err error
	switch c.Source {
	case "163":
		body, err = s.request(ctx, c.Source, "https://music.163.com", "/weapi/song/lyric", nil, map[string]any{"id": c.Ref, "lv": -1, "kv": -1, "tv": -1, "csrf_token": ""})
	case "qq":
		body, err = s.request(ctx, c.Source, "https://c.y.qq.com", "/lyric/fcgi-bin/fcg_query_lyric_new.fcg", map[string]string{"songmid": c.Ref, "g_tk": "5381", "loginUin": "0", "hostUin": "0", "format": "json", "inCharset": "utf8", "outCharset": "utf-8", "notice": "0", "platform": "yqq", "needNewCode": "0", "nobase64": "1"}, nil)
	case "kuwo":
		body, err = s.request(ctx, c.Source, "https://m.kuwo.cn", "/newh5/singles/songinfoandlrc", map[string]string{"musicId": c.Ref}, nil)
	case "kugou":
		return s.fetchKugou(ctx, c)
	default:
		return nil, fmt.Errorf("未知歌词来源")
	}
	if err != nil {
		return nil, err
	}
	return parseDocument(c.Source, body)
}
func value(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func parseQQSongs(body []byte) ([]Candidate, error) {
	songs, err := qqmusic.ParseSearch(body)
	if err != nil {
		return nil, err
	}
	out := []Candidate{}
	for _, s := range songs {
		d := 0
		if s.DurationMS != nil {
			d = *s.DurationMS
		}
		out = append(out, Candidate{Ref: s.MatchRef, Title: value(s.Title), Artist: value(s.ArtistName), Album: value(s.AlbumTitle), DurationMS: d})
	}
	return out, nil
}
