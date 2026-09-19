package itunes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

func loadFixture(t *testing.T, name string, v any) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "golden", "responses", "itunes", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取金样例 %s 失败: %v", name, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("解析金样例 %s 失败: %v", name, err)
	}
}

type nopCache struct{}

func (nopCache) GetCache(string, string, string) (*provider.CacheEntry, error) {
	return nil, provider.ErrCacheMiss
}
func (nopCache) PutCache(string, string, string, []byte, time.Duration) error { return nil }

func testClient(baseURL string) *provider.Client {
	l := provider.NewLimiters()
	l.Configure(provider.ITunes, 1000, time.Second)
	return provider.NewClient(l, nopCache{}, "test")
}

// TestSearchSendsCountryNotLang 覆盖 7.1：
// 只传 country，不传 lang——真正决定返回原生标题的是 country。
func TestSearchSendsCountryNotLang(t *testing.T) {
	var gotParams url.Values
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotParams = r.URL.Query()
		w.Write([]byte(`{"resultCount":0,"results":[]}`))
	}))
	defer srv.Close()

	p := New(testClient(srv.URL), Config{DefaultCountry: "jp"}, nil)
	// BaseURL 是常量，用 transport 把请求重定向到 stub 服务器
	p.client.HTTP = &http.Client{Transport: redirectTo(srv.URL)}
	if _, err := p.SearchTracks(context.Background(), provider.Query{Title: "God knows"}); err != nil {
		t.Fatalf("搜索失败: %v", err)
	}

	if gotPath != "/search" {
		t.Errorf("端点 = %q, 期望 /search", gotPath)
	}
	if got := gotParams.Get("country"); got != "jp" {
		t.Errorf("country = %q, 期望 jp", got)
	}
	if _, has := gotParams["lang"]; has {
		t.Error("不应传 lang——它仅支持 en_us / ja_jp，决定原生标题的是 country")
	}
	if got := gotParams.Get("media"); got != "music" {
		t.Errorf("media = %q, 期望 music", got)
	}
	if got := gotParams.Get("entity"); got != "song" {
		t.Errorf("entity = %q, 期望 song", got)
	}
	if gotParams.Get("term") == "" {
		t.Error("term 不应为空")
	}
}

// TestLookupLimitExceedsTrackCount 覆盖 7.2：
// limit 必须大于专辑声明的曲目总数，否则清单被截断。
func TestLookupLimitExceedsTrackCount(t *testing.T) {
	var gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		data, _ := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "golden",
			"responses", "itunes", "lookup_120_tracks.json"))
		w.Write(data)
	}))
	defer srv.Close()

	p := New(testClient(srv.URL), Config{}, nil)
	p.client.HTTP = &http.Client{Transport: redirectTo(srv.URL)}

	d, err := p.FetchDetail(context.Background(), provider.Ref{ID: "jp:999001", Country: "jp"})
	if err != nil {
		t.Fatalf("清单查询失败: %v", err)
	}

	if gotLimit != "200" {
		t.Errorf("limit = %q, 期望 200（需大于 trackCount）", gotLimit)
	}
	if len(d.Tracklist) != 120 {
		t.Errorf("清单条数 = %d, 期望 120（不应被截断）", len(d.Tracklist))
	}
}

// TestLookupFirstResultIsCollection 覆盖 7.2：
// results[0] 是 collection 自身，被解析为发行信息而非曲目。
func TestLookupFirstResultIsCollection(t *testing.T) {
	var resp lookupResponse
	loadFixture(t, "lookup_120_tracks.json", &resp)

	d := ParseLookup(&resp, "jp", "")

	// 发行信息来自首条
	if d.AlbumTitle == nil || *d.AlbumTitle != "超大型合集" {
		t.Errorf("专辑名 = %v", d.AlbumTitle)
	}
	if d.TrackTotal == nil || *d.TrackTotal != 120 {
		t.Errorf("总曲数 = %v, 期望 120", d.TrackTotal)
	}
	if d.DiscTotal == nil || *d.DiscTotal != 2 {
		t.Errorf("总碟数 = %v, 期望 2", d.DiscTotal)
	}

	// 首条不应出现在清单里
	if len(d.Tracklist) != 120 {
		t.Fatalf("清单条数 = %d, 期望 120（首条是 collection，不算曲目）", len(d.Tracklist))
	}
	for _, entry := range d.Tracklist {
		if entry.Title == "超大型合集" {
			t.Error("collection 自身被误解析为清单中的一首曲目")
		}
	}

	// 双碟的碟号被正确解析
	if d.Tracklist[0].DiscNumber != 1 {
		t.Errorf("首条碟号 = %d", d.Tracklist[0].DiscNumber)
	}
	if last := d.Tracklist[119]; last.DiscNumber != 2 {
		t.Errorf("末条碟号 = %d, 期望 2", last.DiscNumber)
	}
}

// TestSearchResultsParsed 覆盖搜索结果解析与 match_ref 构成。
func TestSearchResultsParsed(t *testing.T) {
	var resp searchResponse
	loadFixture(t, "search_song.json", &resp)

	got := ParseSearchResults(&resp, "jp")
	if len(got) != 2 {
		t.Fatalf("候选数 = %d, 期望 2", len(got))
	}

	c := got[0]
	// match_ref 必须带地区
	if c.MatchRef != "jp:534038851" {
		t.Errorf("match_ref = %q, 期望 jp:534038851", c.MatchRef)
	}
	if c.Title == nil || *c.Title != "God knows..." {
		t.Errorf("标题 = %v", c.Title)
	}
	if c.DurationMS == nil || *c.DurationMS != 332000 {
		t.Errorf("时长 = %v", c.DurationMS)
	}
	if c.Year == nil || *c.Year != 2006 {
		t.Errorf("年份 = %v", c.Year)
	}

	// iTunes 不提供的字段必须为 nil，不用空串或零值伪装
	if c.PrimaryType != nil {
		t.Errorf("主要类型应为缺失, 得到 %v", c.PrimaryType)
	}
	if c.MediumFormat != nil {
		t.Errorf("载体格式应为缺失, 得到 %v", c.MediumFormat)
	}
	if c.Country != nil {
		t.Errorf("国家应为缺失（iTunes 只回显查询用的 country）, 得到 %v", c.Country)
	}
	if c.UpstreamScore != nil {
		t.Errorf("上游相关度应为缺失, 得到 %v", c.UpstreamScore)
	}
	if c.ISRC != nil {
		t.Errorf("ISRC 应为缺失, 得到 %v", c.ISRC)
	}
}

// TestRefIncludesCountry 覆盖 8.1：
// 同一首歌在不同地区 trackId 不同，标识必须带 country 才唯一。
func TestRefIncludesCountry(t *testing.T) {
	jp := MakeRef("jp", 534038851)
	us := MakeRef("us", 534038851)
	if jp == us {
		t.Error("不同地区的同一 trackId 应产生不同标识")
	}
	if jp != "jp:534038851" {
		t.Errorf("标识 = %q, 期望 jp:534038851", jp)
	}

	id, country := SplitRef("jp:534038851")
	if id != "534038851" || country != "jp" {
		t.Errorf("拆分 = (%q, %q)", id, country)
	}
}

// TestScriptDetectionGolden 覆盖 7.4 的各文字系统金样例。
func TestScriptDetectionGolden(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		want    string
		detected bool
	}{
		{"假名建议日本", "ハレ晴レユカイ", "jp", true},
		{"平假名建议日本", "そんな私に", "jp", true},
		{"谚文建议韩国", "안녕하세요", "kr", true},
		{"简体特有字建议中国大陆", "骤雨东来", "cn", true},
		{"繁体特有字建议台湾地区", "驟雨東來", "tw", true},
		{"西里尔建议俄罗斯", "Привет", "ru", true},
		{"拉丁文无建议", "That Girl", "", false},
		{"空串无建议", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DetectCountry(c.text)
			if got.Detected != c.detected {
				t.Errorf("Detected = %v, 期望 %v", got.Detected, c.detected)
			}
			if got.Country != c.want {
				t.Errorf("建议地区 = %q, 期望 %q", got.Country, c.want)
			}
			// 探测命中时必须给出可展示的提示
			if c.detected && got.Message() == "" {
				t.Error("探测命中时应给出界面提示")
			}
		})
	}
}

// TestSuggestionMessage 覆盖「探测命中时提示用户」的文案。
func TestSuggestionMessage(t *testing.T) {
	got := DetectCountry("ハレ晴レユカイ").Message()
	if want := "检测到日文，已建议使用 日本 地区"; got != want {
		t.Errorf("提示文案 = %q, 期望 %q", got, want)
	}
}

// TestCountryPriority 覆盖 7.5：
// 手动选择 > 探测建议 > 设置默认，且手动选择不被建议覆盖。
func TestCountryPriority(t *testing.T) {
	p := New(nil, Config{DefaultCountry: "us"}, nil)

	// 手动选择优先于探测建议
	if got := p.ResolveCountry("us", "ハレ晴レユカイ"); got != "us" {
		t.Errorf("手动选美国而文本为日文时 = %q, 期望 us（手动优先）", got)
	}
	// 无手动选择时用探测建议
	if got := p.ResolveCountry("", "ハレ晴レユカイ"); got != "jp" {
		t.Errorf("探测建议 = %q, 期望 jp", got)
	}
	// 无探测结果时退回设置默认
	if got := p.ResolveCountry("", "That Girl"); got != "us" {
		t.Errorf("退回默认 = %q, 期望 us", got)
	}
}

// TestDefaultCountryFollowsSystemLocale 覆盖「默认跟随系统 locale」。
func TestDefaultCountryFollowsSystemLocale(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "ja_JP.UTF-8")

	p := New(nil, Config{}, nil) // 未设默认地区
	if got := p.ResolveCountry("", "That Girl"); got != "jp" {
		t.Errorf("跟随系统 locale = %q, 期望 jp", got)
	}

	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := SystemCountry(); got != "cn" {
		t.Errorf("系统地区 = %q, 期望 cn", got)
	}
}

// TestCapabilitiesReflectGaps 覆盖 7.6：
// 能力声明如实反映 iTunes 不提供的字段。
func TestCapabilitiesReflectGaps(t *testing.T) {
	caps := New(nil, Config{}, nil).Capabilities()

	notSupported := []struct {
		cap  provider.Capabilities
		name string
	}{
		{provider.CapSecondaryTypes, "次要类型"},
		{provider.CapComposer, "作曲"},
		{provider.CapLyricist, "作词"},
		{provider.CapPerformerRoles, "演出者角色"},
		{provider.CapLabel, "厂牌"},
		{provider.CapCatalogNumber, "目录号"},
		{provider.CapMediumFormat, "载体格式"},
		{provider.CapCountry, "国家或地区"},
		{provider.CapISRC, "ISRC"},
		{provider.CapUpstreamScore, "上游相关度分值"},
		{provider.CapPrimaryType, "主要类型"},
	}
	for _, n := range notSupported {
		if caps.Has(n.cap) {
			t.Errorf("iTunes 不应声明支持「%s」", n.name)
		}
	}

	// 但封面与多规格封面是支持的
	if !caps.Has(provider.CapCoverArt) {
		t.Error("iTunes 应声明支持封面")
	}
	if !caps.Has(provider.CapMultiSizeCover) {
		t.Error("iTunes 应声明支持多规格封面（靠构造地址）")
	}
}

// TestRateLimitDefault 覆盖 7.3：默认 20 req/min。
func TestRateLimitDefault(t *testing.T) {
	p := New(nil, Config{}, nil)
	n, per := p.RateLimit()
	if n != 1 {
		t.Errorf("每次放行 %d 个令牌, 期望 1", n)
	}
	// 20 req/min → 每 3s 一次
	if per != 3*time.Second {
		t.Errorf("间隔 = %v, 期望 3s（20 req/min）", per)
	}
}

// TestSearchAndLookupShareOneBucket 覆盖 7.3：
// 两个端点共用同一个令牌桶，合计速率不超上限。
func TestSearchAndLookupShareOneBucket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"resultCount":0,"results":[]}`))
	}))
	defer srv.Close()

	limiters := provider.NewLimiters()
	limiters.Configure(provider.ITunes, 10, time.Second) // 10/s，便于测量
	client := provider.NewClient(limiters, nopCache{}, "test")
	client.HTTP = &http.Client{Transport: redirectTo(srv.URL)}
	p := New(client, Config{DefaultCountry: "jp"}, nil)

	start := time.Now()
	p.SearchTracks(context.Background(), provider.Query{Title: "a"})
	p.FetchDetail(context.Background(), provider.Ref{ID: "jp:1"})
	p.SearchTracks(context.Background(), provider.Query{Title: "b"})
	elapsed := time.Since(start)

	// 若各自一个桶，三次调用会几乎瞬间完成
	if elapsed < 180*time.Millisecond {
		t.Errorf("search 与 lookup 共耗时 %v, 期望至少 200ms——两端点应共用一个桶", elapsed)
	}
}

// TestTTLIsSet 覆盖 7.7：iTunes 数据必须设缓存有效期，默认 30 天。
func TestTTLIsSet(t *testing.T) {
	p := New(nil, Config{}, nil)
	if got := p.TTL(); got != 30*24*time.Hour {
		t.Errorf("TTL = %v, 期望 30 天", got)
	}

	custom := New(nil, Config{TTLDays: 7}, nil)
	if got := custom.TTL(); got != 7*24*time.Hour {
		t.Errorf("自定义 TTL = %v, 期望 7 天", got)
	}
}

// TestArtworkURLConstruction 覆盖 9.6：四档尺寸的构造。
func TestArtworkURLConstruction(t *testing.T) {
	base := "https://is1-ssl.mzstatic.com/image/thumb/Music/abc/100x100bb.jpg"

	cases := []struct{ size int; want string }{
		{3000, "https://is1-ssl.mzstatic.com/image/thumb/Music/abc/3000x3000bb.jpg"},
		{1200, "https://is1-ssl.mzstatic.com/image/thumb/Music/abc/1200x1200bb.jpg"},
		{600, "https://is1-ssl.mzstatic.com/image/thumb/Music/abc/600x600bb.jpg"},
	}
	for _, c := range cases {
		if got := ArtworkURL(base, c.size); got != c.want {
			t.Errorf("ArtworkURL(%d) = %q, 期望 %q", c.size, got, c.want)
		}
	}

	ladder := ArtworkLadder(base)
	if len(ladder) != 4 {
		t.Errorf("降级链档数 = %d, 期望 4（只提供有限档位）", len(ladder))
	}
}

// redirectTo 把所有请求改写到 stub 服务器。
func redirectTo(base string) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		u := *r.URL
		stub, _ := http.NewRequest(r.Method, base+u.Path+"?"+u.RawQuery, r.Body)
		stub.Header = r.Header
		return http.DefaultTransport.RoundTrip(stub)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
