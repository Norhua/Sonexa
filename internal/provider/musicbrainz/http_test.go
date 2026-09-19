package musicbrainz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// nopCache 不缓存任何内容，使测试能观察到每次实际请求。
type nopCache struct{}

func (nopCache) GetCache(string, string, string) (*provider.CacheEntry, error) {
	return nil, provider.ErrCacheMiss
}
func (nopCache) PutCache(string, string, string, []byte, time.Duration) error { return nil }

// TestUserAgentHeaderFormat 覆盖 6.1：
// stub server 断言 User-Agent 存在且格式为 Sonexa/<版本> ( <联系方式> )。
func TestUserAgentHeaderFormat(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("User-Agent"))
		mu.Unlock()
		w.Write([]byte(`{"count":0,"recordings":[]}`))
	}))
	defer srv.Close()

	limiters := provider.NewLimiters()
	limiters.Configure(provider.MusicBrainz, 100, time.Second)
	client := provider.NewClient(limiters, nopCache{}, provider.UserAgentFor("0.1.0", "me@example.com"))

	p := New(client, Config{Contact: "me@example.com", ServerURL: srv.URL}, nil)
	if _, err := p.SearchTracks(context.Background(), provider.Query{Title: "God knows"}); err != nil {
		t.Fatalf("搜索失败: %v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("请求数 = %d, 期望 1", len(seen))
	}
	ua := seen[0]
	if ua == "" {
		t.Fatal("请求未携带 User-Agent——MB 的硬性要求")
	}
	// 格式：Sonexa/<版本> ( <联系方式> )
	pattern := regexp.MustCompile(`^Sonexa/\d+\.\d+\.\d+ \( .+ \)$`)
	if !pattern.MatchString(ua) {
		t.Errorf("User-Agent = %q, 不符合 Sonexa/<版本> ( <联系方式> ) 格式", ua)
	}
	if !strings.Contains(ua, "me@example.com") {
		t.Errorf("User-Agent = %q, 应包含用户填写的联系方式", ua)
	}
}

// TestNoRequestWhenContactMissing 覆盖 6.1：
// 联系方式为空时不发出任何请求。
func TestNoRequestWhenContactMissing(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	limiters := provider.NewLimiters()
	limiters.Configure(provider.MusicBrainz, 100, time.Second)
	client := provider.NewClient(limiters, nopCache{}, "")

	p := New(client, Config{Contact: "", ServerURL: srv.URL}, nil)

	if _, err := p.SearchTracks(context.Background(), provider.Query{Title: "x"}); err == nil {
		t.Error("联系方式为空时应返回错误")
	}
	if _, err := p.FetchDetail(context.Background(), provider.Ref{ID: "rel-1"}); err == nil {
		t.Error("联系方式为空时详情也应返回错误")
	}
	if requests != 0 {
		t.Errorf("实际发出 %d 次请求, 期望 0 次", requests)
	}
}

// TestSearchSendsExpectedParams 确认搜索端点的参数构造。
func TestSearchSendsExpectedParams(t *testing.T) {
	var gotQuery string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("query")
		if r.URL.Query().Get("fmt") != "json" {
			t.Errorf("fmt = %q, 期望 json", r.URL.Query().Get("fmt"))
		}
		if inc := r.URL.Query().Get("inc"); inc != searchInc {
			t.Errorf("inc = %q, 期望 %q", inc, searchInc)
		}
		w.Write([]byte(`{"count":0,"recordings":[]}`))
	}))
	defer srv.Close()

	limiters := provider.NewLimiters()
	limiters.Configure(provider.MusicBrainz, 100, time.Second)
	client := provider.NewClient(limiters, nopCache{}, "ua")
	p := New(client, Config{Contact: "c", ServerURL: srv.URL}, nil)

	p.SearchTracks(context.Background(), provider.Query{Title: "God knows", Artist: "平野綾"})

	if gotPath != "/ws/2/recording" {
		t.Errorf("端点 = %q, 期望 /ws/2/recording", gotPath)
	}
	if !strings.Contains(gotQuery, "recording:") || !strings.Contains(gotQuery, "artist:") {
		t.Errorf("查询串 = %q", gotQuery)
	}
}

// TestDetailRequestIsSingleAndFullyLoaded 覆盖 6.3：
// 详情只发出 1 次请求，且 inc 一次拉满。
func TestDetailRequestIsSingleAndFullyLoaded(t *testing.T) {
	var requests int
	var gotInc string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		gotInc = r.URL.Query().Get("inc")
		w.Write([]byte(`{"id":"rel-1","title":"专辑","media":[]}`))
	}))
	defer srv.Close()

	limiters := provider.NewLimiters()
	limiters.Configure(provider.MusicBrainz, 100, time.Second)
	client := provider.NewClient(limiters, nopCache{}, "ua")
	p := New(client, Config{Contact: "c", ServerURL: srv.URL}, nil)

	if _, err := p.FetchDetail(context.Background(), provider.Ref{ID: "rel-1"}); err != nil {
		t.Fatalf("详情请求失败: %v", err)
	}

	if requests != 1 {
		t.Errorf("详情发出 %d 次请求, 期望 1 次（1 req/s 下必须一次拉满）", requests)
	}
	// 逐项确认 inc 覆盖全部所需
	for _, needed := range []string{
		"artist-credits", "labels", "recordings", "release-groups", "media",
		"artist-rels", "recording-level-rels", "work-rels", "work-level-rels",
		"genres", "isrcs", "annotation",
	} {
		if !strings.Contains(gotInc, needed) {
			t.Errorf("inc 缺少 %q（实际 %q）", needed, gotInc)
		}
	}
}

// TestCharacterLookupOnlyForCharacters 覆盖 6.7：
// 非角色艺术家不触发补充请求。
func TestCharacterLookupOnlyForCharacters(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(`{"id":"art-haruhi","name":"涼宮ハルヒ","type":"Character","relations":[]}`))
	}))
	defer srv.Close()

	limiters := provider.NewLimiters()
	limiters.Configure(provider.MusicBrainz, 100, time.Second)
	client := provider.NewClient(limiters, nopCache{}, "ua")
	p := New(client, Config{Contact: "c", ServerURL: srv.URL}, nil)

	// 调用方按 IsCharacter 门控：非角色不调用
	person := "person"
	if IsCharacter(&person) {
		t.Fatal("人物类型不应被判为角色")
	}
	if requests != 0 {
		t.Errorf("非角色触发了 %d 次请求", requests)
	}

	// 角色才调用
	character := "character"
	if IsCharacter(&character) {
		p.VoiceActorOf(context.Background(), "art-haruhi")
	}
	if requests != 1 {
		t.Errorf("角色应触发 1 次补充请求, 实际 %d 次", requests)
	}
}
