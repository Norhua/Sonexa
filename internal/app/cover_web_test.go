package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/netease"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

const qqEmpty = `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[]}}}}}`
const neteaseEmpty = `{"code":200,"result":{"songCount":0}}`

func webCoverSource(id string, c *provider.Client) sources.Provider {
	if id == "qq" {
		return qqmusic.New(c, qqmusic.Config{}, nil)
	}
	return netease.New(c)
}
func replayCovers(id string, body []byte, q sources.Query) ([]sources.Result, error) {
	c := provider.NewClient(nil, nil, "test")
	c.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	return webCoverSource(id, c).SearchCovers(context.Background(), q)
}
func TestWebSourceFixturesAndErrors(t *testing.T) {
	for _, id := range []string{"qq", "163"} {
		t.Run(id, func(t *testing.T) {
			body, err := os.ReadFile("../../testdata/golden/responses/covers/" + id + "_search.json")
			if err != nil {
				t.Fatal(err)
			}
			want := 3
			if id == "qq" {
				want = len(qqmusic.CoverSizes)
			}
			results, err := replayCovers(id, body, sources.Query{Title: "晴天", Artist: "周杰伦"})
			if err != nil || len(results) != 1 || len(results[0].Input.Images[0].Variants) != want {
				t.Fatal(results, err)
			}
			for _, v := range results[0].Input.Images[0].Variants {
				if v.Width != nil {
					t.Fatal("构造规格伪装真实像素")
				}
			}
			for _, bad := range []string{"broken", `{}`, `{"code":403}`, `{"code":0,"req":{"code":0,"data":{}}}`} {
				if _, err := replayCovers(id, []byte(bad), sources.Query{Title: "song"}); err == nil {
					t.Fatal(bad)
				}
			}
			empty := qqEmpty
			if id == "163" {
				empty = neteaseEmpty
			}
			if rows, err := replayCovers(id, []byte(empty), sources.Query{Title: "song"}); err != nil || len(rows) != 0 {
				t.Fatal("合法空响应失败", err)
			}
			c := provider.NewClient(nil, nil, "Sonexa/test")
			c.Retry.MaxAttempts = 1
			c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("User-Agent") != "Sonexa/test" {
					t.Fatal("UA 未携带")
				}
				if id == "qq" {
					data, _ := io.ReadAll(r.Body)
					if r.Method != "POST" || r.URL.Path != "/cgi-bin/musicu.fcg" || !strings.Contains(string(data), `"num_per_page":30`) {
						t.Fatal("QQ 请求参数")
					}
				}
				if id == "163" {
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if r.Method != "POST" || r.URL.Path != "/weapi/cloudsearch/pc" || r.Form.Get("params") == "" || len(r.Form.Get("encSecKey")) != 256 {
						t.Fatal("163 请求参数")
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})
			adapter := webCoverSource(id, c)
			if _, err := adapter.SearchCovers(context.Background(), sources.Query{Artist: "周杰伦", Limit: 100}); err != nil {
				t.Fatal(err)
			}
			for _, status := range []int{403, 429, 503} {
				c.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("error"))}, nil
				})
				if _, err := adapter.SearchCovers(context.Background(), sources.Query{Title: "x"}); err == nil {
					t.Fatal(status)
				}
			}
		})
	}
}
func TestWebSourceRetryAfterAndCancellation(t *testing.T) {
	for _, id := range []string{"qq", "163"} {
		t.Run(id, func(t *testing.T) {
			c := provider.NewClient(provider.NewLimiters(), nil, "test")
			c.Limiters.Configure(provider.QuotaID(id), 1000, time.Second)
			c.Retry = provider.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}
			calls := 0
			start := time.Now()
			c.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				status, body := 200, qqEmpty
				if id == "163" {
					body = neteaseEmpty
				}
				header := make(http.Header)
				if calls == 1 {
					status = 429
					header.Set("Retry-After", "1")
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			adapter := webCoverSource(id, c)
			if _, err := adapter.SearchCovers(context.Background(), sources.Query{Title: "x"}); err != nil {
				t.Fatal(err)
			}
			if calls != 2 || time.Since(start) < time.Second {
				t.Fatal("忽略 Retry-After")
			}
			ctx := provider.WithRequestGate(context.Background(), func() error { return fmt.Errorf("disabled") })
			if _, err := adapter.SearchCovers(ctx, sources.Query{Title: "x"}); err == nil || calls != 2 {
				t.Fatal("门禁失效")
			}
		})
	}
}
func TestWebSourceQuotasAreIndependentAndSharedAcrossInstances(t *testing.T) {
	limiters := provider.NewLimiters()
	arrivals := make(chan string, 4)
	makeSource := func(id string) sources.Provider {
		c := provider.NewClient(limiters, nil, "test")
		c.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
			arrivals <- id
			body := qqEmpty
			if id == "163" {
				body = neteaseEmpty
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		return webCoverSource(id, c)
	}
	qq, ne := makeSource("qq"), makeSource("163")
	q := sources.Query{Title: "song"}
	if _, err := qq.SearchCovers(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	<-arrivals
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := ne.SearchCovers(ctx, q); err != nil {
		t.Fatal(err)
	}
	<-arrivals
	if _, err := makeSource("qq").SearchCovers(ctx, q); err == nil {
		t.Fatal("新实例重置配额")
	}
	select {
	case id := <-arrivals:
		t.Fatal("额外请求", id)
	default:
	}
}
