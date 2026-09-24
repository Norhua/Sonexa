package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type coverSearchProvider struct {
	id     provider.ProviderID
	search func(context.Context, provider.Query) ([]provider.Candidate, error)
}

func (p *coverSearchProvider) ID() provider.ProviderID             { return p.id }
func (p *coverSearchProvider) DisplayName() string                 { return string(p.id) }
func (p *coverSearchProvider) Capabilities() provider.Capabilities { return provider.CapCoverArt }
func (p *coverSearchProvider) RateLimit() (int, time.Duration)     { return 1, time.Second }
func (p *coverSearchProvider) IsConfigured() error                 { return nil }
func (p *coverSearchProvider) SearchTracks(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
	return p.search(ctx, q)
}
func (p *coverSearchProvider) FetchDetail(context.Context, provider.Ref) (*provider.Detail, error) {
	return nil, nil
}
func (p *coverSearchProvider) SearchCovers(context.Context, provider.Query) ([]provider.CoverGroup, error) {
	return nil, nil
}

type appCoverTransport func(*http.Request) (*http.Response, error)

func (f appCoverTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCoverSourceRegistryOnlyQQIsMetadataProvider(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	defer a.coverSvc.Close()
	providers, err := a.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	// QQ 音乐已是标准元数据上游；163 仍只作为封面来源。
	ids := []string{}
	for _, p := range providers {
		ids = append(ids, p.ID)
		if p.ID == "163" {
			t.Fatal("封面专用来源泄漏到元数据注册表")
		}
	}
	if strings.Join(ids, ",") != "musicbrainz,itunes,qq" {
		t.Fatalf("元数据来源改变: %v", ids)
	}
	sources := a.ListCoverSources()
	if len(sources) != 4 {
		t.Fatal(sources)
	}
	for i, id := range []string{"caa", "itunes", "qq", "163"} {
		if sources[i].ID != id || sources[i].NeedsCountry != (id == "itunes") {
			t.Fatal(sources[i])
		}
	}
}

func TestCoverSearchProgressAndAuthorization(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	id, _ := a.store.FindOrCreateLocalRelease("album", nil)
	slow := make(chan struct{})
	events := make(chan CoverSearchEvent, 8)
	a.emit = func(name string, data ...any) {
		if name == EventCoverResults {
			events <- data[0].(CoverSearchEvent)
		}
	}
	a.registry = provider.NewRegistry()
	a.registry.Register(&coverSearchProvider{id: provider.MusicBrainz, search: func(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
		select {
		case <-slow:
			return nil, fmt.Errorf("CAA search failed")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})
	a.registry.Register(&coverSearchProvider{id: provider.ITunes, search: func(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
		if !q.SkipCache {
			t.Error("显式搜索未绕过缓存")
		}
		ref, url, album, title := "jp:12", "https://is1.mzstatic.com/image/100x100bb.jpg", "album", "song"
		return []provider.Candidate{{ReleaseRef: &ref, ThumbURL: &url, AlbumTitle: &album, Title: &title}}, nil
	}})
	request := CoverSearchRequest{SearchID: uuid.NewString(), Generation: 7, ReleaseID: id, Title: "song", Sources: []string{"caa", "itunes"}}
	if err := a.StartCoverSearch(request); err != nil {
		t.Fatal(err)
	}
	var first CoverSearchEvent
	select {
	case first = <-events:
	case <-time.After(time.Second):
		t.Fatal("快来源被慢来源阻塞")
	}
	if first.Generation != 7 || first.ReleaseID != id || first.SearchID != request.SearchID {
		t.Fatalf("会话身份丢失: %+v", first)
	}
	if first.Source != "itunes" || len(first.Cards) != 1 || len(first.Cards[0].Image.Variants) != 5 {
		t.Fatalf("%+v", first)
	}
	close(slow)
	second := <-events
	if second.State != "error" || second.Source != "caa" {
		t.Fatal(second)
	}
	if err := a.StartCoverSearch(request); err == nil {
		t.Fatal("重复会话标识覆盖了现有结果")
	}
	if _, err := a.AdoptCoverCandidate(request.SearchID, 99999, 0); err == nil {
		t.Fatal("任意规格可被采用")
	}
	a.CancelCoverSearch(request.SearchID)
	if _, err := a.AdoptCoverCandidate(request.SearchID, first.Cards[0].Image.Variants[0].ID, 0); err == nil {
		t.Fatal("已结束会话仍可采用")
	}
	a.coverWorkers.Wait()
}

func TestCoverAdoptionFallsBackWithinImageAndUpdatesOnlyTarget(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	id, _ := a.store.FindOrCreateLocalRelease("target", nil)
	other, _ := a.store.FindOrCreateLocalRelease("other", nil)
	key := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "ref"}
	_, err := a.store.SaveCoverCatalog(key, []store.CoverImage{
		{SourceImageID: "front", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "1200", URL: "https://archive.org/missing"}, {Spec: "original", URL: "https://archive.org/front"}}},
		{SourceImageID: "back", PrimaryType: "Back", Variants: []store.CoverVariant{{Spec: "original", URL: "https://archive.org/back"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := a.store.GetCoverCatalog(key)
	variant := catalog.Images[0].Variants[0]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.coverSessions = map[string]*coverSession{"test": {ctx: ctx, cancel: cancel, releaseID: id, variants: map[int64]bool{variant.ID: true}}}
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var mu sync.Mutex
	paths := []string{}
	http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		status, body := 200, coverPNG(t, 900, 900)
		if r.URL.Path == "/missing" {
			status = 404
			body = nil
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	result, err := a.AdoptCoverCandidate("test", variant.ID, 0)
	if err != nil || result.Width != 900 || result.VariantID != catalog.Images[0].Variants[1].ID {
		t.Fatalf("%+v %v", result, err)
	}
	mu.Lock()
	for _, path := range paths {
		if path == "/back" {
			t.Error("跨图片降级")
		}
	}
	mu.Unlock()
	if selection, _ := a.store.GetCoverSelection(other); selection != nil {
		t.Fatal("更新了另一歌集")
	}
	if _, err := a.AdoptCoverCandidate("test", variant.ID, 0); err == nil {
		t.Fatal("旧 revision 可以覆盖新选择")
	}
}

func TestCoverSearchEmptyAndErrorsStayDistinct(t *testing.T) {
	for _, state := range []string{"empty", "error"} {
		t.Run(state, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			if err := a.coverSvc.AttachLibrary(a.store); err != nil {
				t.Fatal(err)
			}
			defer a.coverSvc.Close()
			id, err := a.store.FindOrCreateLocalRelease("album", nil)
			if err != nil {
				t.Fatal(err)
			}
			a.registry = provider.NewRegistry()
			for _, id := range []provider.ProviderID{provider.MusicBrainz, provider.ITunes} {
				a.registry.Register(&coverSearchProvider{id: id, search: func(context.Context, provider.Query) ([]provider.Candidate, error) {
					if state == "error" {
						return nil, fmt.Errorf("source failed")
					}
					return nil, nil
				}})
			}
			old := http.DefaultTransport
			defer func() { http.DefaultTransport = old }()
			http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
				status, body := 200, `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[]}}}}}`
				if strings.Contains(r.URL.Host, "163") {
					body = `{"code":200,"result":{"songCount":0}}`
				}
				if state == "error" {
					status, body = 403, "source unavailable"
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			request := CoverSearchRequest{SearchID: uuid.NewString(), Generation: 3, ReleaseID: id, Title: "song", Sources: []string{"caa", "itunes", "qq", "163"}}
			if err := a.StartCoverSearch(request); err != nil {
				t.Fatal(err)
			}
			a.coverWorkers.Wait()
			results := a.CoverSearchResults(request.SearchID)
			if len(results) != 4 {
				t.Fatal(results)
			}
			for _, result := range results {
				if result.State != state || (result.Error != "") != (state == "error") || len(result.Cards) != 0 {
					t.Fatal(result)
				}
			}
			a.CancelCoverSearch(request.SearchID)
		})
	}
}

func TestCoverSearchCapsCardsAndRetryKeepsOtherAuthorization(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	id, err := a.store.FindOrCreateLocalRelease("album", nil)
	if err != nil {
		t.Fatal(err)
	}
	a.registry = provider.NewRegistry()
	a.registry.Register(&coverSearchProvider{id: provider.ITunes, search: func(context.Context, provider.Query) ([]provider.Candidate, error) {
		rows := []provider.Candidate{}
		for i := range 30 {
			ref, url, title := fmt.Sprintf("jp:%d", i), fmt.Sprintf("https://is1.mzstatic.com/%d/100x100bb.jpg", i), "song"
			rows = append(rows, provider.Candidate{ReleaseRef: &ref, ThumbURL: &url, Title: &title})
		}
		return rows, nil
	}})
	a.registry.Register(&coverSearchProvider{id: provider.MusicBrainz, search: func(context.Context, provider.Query) ([]provider.Candidate, error) { return nil, nil }})
	first := CoverSearchRequest{SearchID: uuid.NewString(), Generation: 1, ReleaseID: id, Title: "song", Sources: []string{"itunes"}}
	if err := a.StartCoverSearch(first); err != nil {
		t.Fatal(err)
	}
	a.coverWorkers.Wait()
	result := a.CoverSearchResults(first.SearchID)
	if len(result) != 1 || len(result[0].Cards) != 20 {
		t.Fatal("结果未限制为 20", result)
	}
	for range 2 {
		retry := CoverSearchRequest{SearchID: uuid.NewString(), Generation: 1, ReleaseID: id, Title: "song", Sources: []string{"caa"}}
		if err := a.StartCoverSearch(retry); err != nil {
			t.Fatal(err)
		}
		a.coverWorkers.Wait()
		a.CancelCoverSearch(retry.SearchID)
	}
	variant := result[0].Cards[0].Image.Variants[0]
	if _, _, err := a.coverSvc.Library().SaveVariant(0, variant.ID, coverPNG(t, 600, 600), false, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptCoverCandidate(first.SearchID, variant.ID, 0); err != nil {
		t.Fatalf("重试其他来源后采用授权丢失: %v", err)
	}
	a.CancelCoverSearch(first.SearchID)
}

func TestCoverSearchCancelledDoesNotEmitOrReopen(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	id, _ := a.store.FindOrCreateLocalRelease("album", nil)
	started := make(chan struct{})
	a.registry = provider.NewRegistry()
	a.registry.Register(&coverSearchProvider{id: provider.ITunes, search: func(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	a.emit = func(name string, _ ...any) {
		if name == EventCoverResults {
			t.Error("取消后仍发送结果")
		}
	}
	request := CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: id, Title: "x", Sources: []string{"itunes"}}
	if err := a.StartCoverSearch(request); err != nil {
		t.Fatal(err)
	}
	<-started
	a.CancelCoverSearch(request.SearchID)
	a.coverWorkers.Wait()
	if len(a.CoverSearchResults(request.SearchID)) != 0 {
		t.Fatal("取消后仍保留搜索快照")
	}
}
