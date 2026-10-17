package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

type forbiddenLyricCache struct{ t *testing.T }

func (c forbiddenLyricCache) GetCache(string, string, string) (*provider.CacheEntry, error) {
	c.t.Error("歌词读取响应缓存")
	return nil, provider.ErrCacheMiss
}
func (c forbiddenLyricCache) PutCache(string, string, string, []byte, time.Duration) error {
	c.t.Error("歌词写入响应缓存")
	return nil
}
func TestAllLyricsCapabilitiesUseNoStore(t *testing.T) {
	for _, source := range []string{"163", "qq", "kugou", "kuwo"} {
		t.Run(source, func(t *testing.T) {
			client := provider.NewClient(nil, forbiddenLyricCache{t}, "test")
			client.Retry.MaxAttempts = 1
			calls := 0
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})
			s := sourceService(client)
			_, _ = s.Search(context.Background(), source, lyrics.Query{Title: "song"})
			_, _ = s.Fetch(context.Background(), lyrics.Candidate{Source: source, Ref: "native"})
			if calls < 2 {
				t.Fatal("未覆盖搜索和获取")
			}
		})
	}
}
