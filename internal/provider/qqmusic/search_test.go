package qqmusic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

type protocolTransport func(*http.Request) (*http.Response, error)

func (f protocolTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMetadataSearchRequestPolicy(t *testing.T) {
	for _, tc := range []struct {
		titleOnly        bool
		limit, wantLimit int
		term             string
	}{
		{false, 0, 25, "song artist album"}, {true, 50, 50, "song"}, {false, 51, 25, "song artist album"}, {false, -1, 25, "song artist album"},
	} {
		client := provider.NewClient(nil, nil, "test")
		calls := 0
		client.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			var payload struct {
				Comm map[string]string
				Req  struct {
					Module, Method string
					Param          struct {
						Query      string
						Limit      int `json:"num_per_page"`
						Page       int `json:"page_num"`
						SearchType int `json:"search_type"`
					}
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if r.Method != "POST" || r.URL.Path != musicuPath || r.Header.Get("Referer") != "https://y.qq.com/" || payload.Comm["ct"] != "19" || payload.Req.Module != "music.search.SearchCgiService" || payload.Req.Method != "DoSearchForQQMusicDesktop" || payload.Req.Param.Query != tc.term || payload.Req.Param.Limit != tc.wantLimit || payload.Req.Param.Page != 1 || payload.Req.Param.SearchType != 0 {
				t.Fatalf("request=%+v", payload)
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[]}}}}}`))}, nil
		})
		p := New(client, Config{}, nil)
		if rows, err := p.SearchTracks(context.Background(), provider.Query{Title: "song", Artist: "artist", Album: "album", TitleOnly: tc.titleOnly, Limit: tc.limit}); err != nil || len(rows) != 0 || calls != 1 {
			t.Fatalf("rows=%v calls=%d err=%v", rows, calls, err)
		}
	}
}

func TestSongProtocolAndMetadataProjectionAreSeparate(t *testing.T) {
	body := []byte(`{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[
		{"name":"cover only","album":{"id":1,"mid":"000BDJvT2aWQYZ"}},
		{"name":"song only","mid":"003RcjkV1XjQQy","album":{"id":0}},
		{"name":"complete","mid":"0039MnYb0qxYhV","album":{"id":2,"mid":"000MkMni19ClKG"}}
	]}}}}}`)
	songs, err := parseSongs(body)
	if err != nil || len(songs) != 3 || songs[0].Album.MID == "" {
		t.Fatalf("raw projection=%+v err=%v", songs, err)
	}
	rows, err := ParseSearch(body)
	if err != nil || len(rows) != 2 || rows[0].ReleaseRef != nil || rows[1].ReleaseRef == nil {
		t.Fatalf("metadata=%+v err=%v", rows, err)
	}
	for _, body := range []string{`{}`, `{"code":1}`, `{"code":0,"req":{"code":2}}`, `{"code":0,"req":{"code":0,"data":{"body":{"song":{}}}}}`, `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":null}}}}}`} {
		if _, err := parseSongs([]byte(body)); err == nil {
			t.Fatal("invalid protocol accepted", body)
		}
	}
}
