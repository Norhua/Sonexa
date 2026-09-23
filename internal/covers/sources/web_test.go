package sources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWebSourceFixturesAndErrors(t *testing.T) {
	for _, id := range []string{"qq", "163"} {
		t.Run(id, func(t *testing.T) {
			body, err := os.ReadFile("../../../testdata/golden/responses/covers/" + id + "_search.json")
			if err != nil {
				t.Fatal(err)
			}
			parse := ParseQQ
			if id == "163" {
				parse = Parse163
			}
			results, err := parse(body)
			if err != nil || len(results) != 1 || len(results[0].Images[0].Variants) != 3 {
				t.Fatalf("%+v %v", results, err)
			}
			for _, variant := range results[0].Images[0].Variants {
				if variant.Width != nil {
					t.Fatal("构造规格伪装真实像素")
				}
			}
			for _, bad := range []string{"broken", `{}`, `{"code":403}`, `{"code":0,"data":{}}`} {
				if _, err := parse([]byte(bad)); err == nil {
					t.Fatal(bad)
				}
			}
			empty := `{"code":0,"data":{"song":{"list":[]}}}`
			if id == "163" {
				empty = `{"code":200,"result":{"albumCount":0}}`
			}
			if rows, err := parse([]byte(empty)); err != nil || len(rows) != 0 {
				t.Fatal("有效空响应失败")
			}
			adapter := NewWebSource(id, "Sonexa/test", provider.NewLimiters(), nil)
			adapter.Retry = provider.RetryPolicy{MaxAttempts: 1}
			adapter.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("User-Agent") != "Sonexa/test" {
					t.Fatal("UA 未携带")
				}
				if id == "qq" && (r.Method != "GET" || r.URL.Query().Get("n") != "20") {
					t.Fatal("QQ 请求参数")
				}
				if id == "163" {
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if r.Method != "POST" || r.Form.Get("type") != "10" {
						t.Fatal("163 请求参数")
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
			})
			if _, err := adapter.Search(context.Background(), provider.Query{Artist: "周杰伦", Limit: 100}); err != nil {
				t.Fatal(err)
			}
			for _, status := range []int{403, 429, 503} {
				adapter.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("error")), Header: make(http.Header)}, nil
				})
				if _, err := adapter.Search(context.Background(), provider.Query{Title: "x"}); err == nil {
					t.Fatal(status)
				}
			}
		})
	}
}

func TestWebSourceRetryAfterAndCancellation(t *testing.T) {
	for _, id := range []string{"qq", "163"} {
		t.Run(id, func(t *testing.T) {
			adapter := NewWebSource(id, "test", provider.NewLimiters(), nil)
			adapter.Limiters.Configure(provider.ProviderID("cover-"+id), 1000, time.Second)
			adapter.Retry = provider.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}
			calls := 0
			start := time.Now()
			adapter.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
				calls++
				status, body := 200, `{"code":0,"data":{"song":{"list":[]}}}`
				if id == "163" {
					body = `{"code":200,"result":{"albumCount":0}}`
				}
				header := make(http.Header)
				if calls == 1 {
					status = 429
					header.Set("Retry-After", "1")
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: header}, nil
			})
			if _, err := adapter.Search(context.Background(), provider.Query{Title: "x"}); err != nil {
				t.Fatal(err)
			}
			if calls != 2 || time.Since(start) < time.Second {
				t.Fatal("忽略 Retry-After")
			}
			adapter.Gate = func() error { return fmt.Errorf("disabled") }
			if _, err := adapter.Search(context.Background(), provider.Query{Title: "x"}); err == nil || calls != 2 {
				t.Fatal("出网禁用失效")
			}
		})
	}
}

func TestWebSourceQuotasAreIndependentAndSharedAcrossInstances(t *testing.T) {
	limiters := provider.NewLimiters()
	arrivals := make(chan string, 4)
	makeSource := func(id string) *WebSource {
		s := NewWebSource(id, "test", limiters, nil)
		s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
			arrivals <- id
			body := `{"code":0,"data":{"song":{"list":[]}}}`
			if id == "163" {
				body = `{"code":200,"result":{"albumCount":0}}`
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		return s
	}
	qq, netease := makeSource("qq"), makeSource("163")
	q := provider.Query{Title: "song"}
	if _, err := qq.Search(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	<-arrivals
	// QQ 已用完一秒内的令牌，网易云仍可立即开始。
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := netease.Search(ctx, q); err != nil {
		t.Fatalf("两个来源错误共用配额: %v", err)
	}
	<-arrivals
	// 新建同来源实例不能刷新桶，第二次 QQ 查询仍须等待或取消。
	anotherQQ := makeSource("qq")
	if _, err := anotherQQ.Search(ctx, q); err == nil {
		t.Fatal("另一个 QQ 实例绕过了共享 1 req/s 配额")
	}
	select {
	case id := <-arrivals:
		t.Fatal("限流期间发出额外请求", id)
	default:
	}
}
