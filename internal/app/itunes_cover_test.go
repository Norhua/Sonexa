package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// 真实 Search/Lookup 摘录 + 生产 App/AssetServer：不能仅测 URL 零件。
func TestITunesSearchAdmissionAndCoverRecovery(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	search, err := os.ReadFile("../../testdata/golden/responses/itunes/search_side_c.json")
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := os.ReadFile("../../testdata/golden/responses/itunes/lookup_side_c.json")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls := map[string]int{}
	replay := appCoverTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		var data []byte
		switch r.URL.Hostname() {
		case "itunes.apple.com":
			if r.URL.Query().Get("country") != "jp" {
				t.Errorf("wrong storefront: %s", r.URL)
			}
			switch r.URL.Path {
			case "/search":
				data = search
			case "/lookup":
				if r.URL.Query().Get("id") != "1475065332" {
					t.Errorf("wrong album: %s", r.URL)
				}
				data = lookup
			default:
				return nil, fmt.Errorf("unexpected metadata: %s", r.URL)
			}
		case "is1-ssl.mzstatic.com":
			size := 250
			if strings.HasSuffix(r.URL.Path, "/1200x1200bb.jpg") {
				size = 1200
			} else if !strings.HasSuffix(r.URL.Path, "/250x250bb.jpg") {
				return nil, fmt.Errorf("wrong artwork size: %s", r.URL)
			}
			data = coverPNG(t, size, size)
		default:
			return nil, fmt.Errorf("unexpected host (must not query CAA): %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	})
	old := http.DefaultTransport
	http.DefaultTransport = replay
	defer func() { http.DefaultTransport = old }()
	a.client.HTTP.Transport = replay
	startService := func() {
		svc, err := newCoverService(a.store, a.paths, a.settings.Get(), "test")
		if err != nil {
			t.Fatal(err)
		}
		a.coverSvc = a.attachLadderBuilder(svc)
		svc.EnableNetworkPolicy(a.checkCoverOutbound)
	}
	startService()
	defer func() { a.coverSvc.Close() }()
	request := func(target string, usage string, size int) {
		t.Helper()
		r := httptest.NewRecorder()
		a.coverAssetHandler().ServeHTTP(r, httptest.NewRequest("GET", "/cover?"+target+"&usage="+usage, nil))
		if r.Code != 200 {
			t.Fatalf("%s %s: %d %s", target, usage, r.Code, r.Body.String())
		}
		info, err := covers.InspectImage(r.Body.Bytes())
		if err != nil || info.Width != size {
			t.Fatalf("wrong size: %+v %v", info, err)
		}
	}
	response, err := a.SearchUpstream("ツバサ", "4円", "side-C", []string{"itunes"}, "jp")
	if err != nil || len(response.Aggregate) != 1 {
		t.Fatalf("search: %+v %v", response, err)
	}
	row := response.Aggregate[0]
	if row.ReleaseID != 0 || deref(row.ReleaseRef) != "jp:1475065332" {
		t.Fatal(row)
	}
	refs := a.BuildCoverRefs([]CandidateRow{row})
	if len(refs) != 1 || refs[0].Target.Ref != "jp:1475065332" {
		t.Fatal(refs)
	}
	candidateTarget := "provider=itunes&ref=jp:1475065332"
	request(candidateTarget, "thumbnail", 250) // 不需要先预取；也不额外 Lookup
	if calls["/lookup"] != 0 {
		t.Fatal("known artwork caused redundant lookup", calls)
	}
	if n, err := a.store.CountTracks(store.TrackFilter{}); err != nil || n != 0 {
		t.Fatal("preview created a track", n, err)
	}
	albums, err := a.ListAlbums()
	if err != nil || len(albums) != 0 {
		t.Fatal("preview created album", albums, err)
	}

	id, err := a.AddToLibrary(row)
	if err != nil {
		t.Fatal(err)
	}
	track, err := a.store.GetTrack(id)
	if err != nil || track.ReleaseID == nil {
		t.Fatal(track, err)
	}
	releaseID := *track.ReleaseID
	libraryTarget := fmt.Sprintf("releaseId=%d", releaseID)
	request(libraryTarget, "thumbnail", 250) // 复制候选自动预览，不再下载
	request(libraryTarget, "detail", 1200)
	request(candidateTarget, "detail", 1200) // 已入库候选也定位到歌集
	for path, count := range calls {
		if strings.Contains(path, "x") && count != 1 {
			t.Fatalf("duplicate artwork: %s=%d", path, count)
		}
	}
	files, err := a.store.ListReleaseCoverFiles(releaseID)
	if err != nil || len(files) != 2 {
		t.Fatalf("files: %+v %v", files, err)
	}
	for _, file := range files {
		if file.Protected || !strings.Contains(file.RelativePath, "/Front-itunes-") {
			t.Fatal(file)
		}
	}
	if selection, err := a.store.GetCoverSelection(releaseID); err != nil || selection != nil {
		t.Fatal("automatic cover became explicit adoption", selection, err)
	}

	if err := a.RefreshTrackDetail(id, "itunes", "jp:1475065332"); err != nil {
		t.Fatal(err)
	}
	d, err := a.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.CreditText != "4円" || len(d.CreditSpans) != 1 || len(d.Participants) != 0 {
		t.Fatalf("lost credit / invented roles: %+v", d)
	}
	if d.Metadata.Year == nil || *d.Metadata.Year != 2019 || deref(d.Metadata.Genre) != "J-Pop" || *d.Metadata.TrackNumber != 2 || *d.Metadata.DurationMS != 267720 {
		t.Fatalf("lost details: %+v", d.Metadata)
	}
	if d.Metadata.Country != nil || d.Metadata.MediumFormat != nil || d.Metadata.PrimaryType != nil {
		t.Fatal("invented unavailable fields")
	}
	release, err := a.store.GetRelease(releaseID)
	if err != nil || release.TracklistState != store.TracklistComplete {
		t.Fatal(release, err)
	}

	// 模拟此前只有已入库身份和原始 Lookup 缓存、没有封面目录的数据库。
	if err := a.coverSvc.ClearCache(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.DB().Exec(`
		DELETE FROM cover_file WHERE variant_id IN (SELECT v.id FROM cover_variant v JOIN cover_image i ON i.id=v.image_id JOIN cover_catalog c ON c.id=i.catalog_id WHERE c.source='itunes');
		DELETE FROM cover_variant WHERE image_id IN (SELECT id FROM cover_image WHERE catalog_id IN (SELECT id FROM cover_catalog WHERE source='itunes'));
		DELETE FROM cover_image WHERE catalog_id IN (SELECT id FROM cover_catalog WHERE source='itunes');
		DELETE FROM cover_catalog WHERE source='itunes'`); err != nil {
		t.Fatal(err)
	}
	request(libraryTarget, "detail", 1200)
	if calls["/lookup"] != 1 {
		t.Fatal("failed to reuse metadata lookup cache", calls)
	}

	// 数据库与封面服务一起重开，证明目录/文件不依赖进程内记忆。
	if err := a.coverSvc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(a.paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	a.store = reopened
	defer reopened.Close()
	a.rebuildProviders(a.settings.Get())
	a.client.HTTP.Transport = replay
	startService()
	request(libraryTarget, "detail", 1200)
	if err := a.ClearCache(); err != nil {
		t.Fatal(err)
	}
	request(libraryTarget, "detail", 1200)
	if calls["/lookup"] != 1 {
		t.Fatal("forgot persisted catalog", calls)
	}

	// 清除与采用必须压过自动 iTunes。
	if _, err := a.ClearCover(releaseID, 0); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	a.coverAssetHandler().ServeHTTP(r, httptest.NewRequest("GET", "/cover?"+libraryTarget+"&usage=detail", nil))
	if r.Code != 404 {
		t.Fatalf("clear refetched artwork: %d", r.Code)
	}
	if _, err := a.coverSvc.Library().ImportLocal(releaseID, coverPNG(t, 800, 800), 1); err != nil {
		t.Fatal(err)
	}
	request(libraryTarget, "detail", 800)
}

func TestITunesAutomaticLookupGateAndIdentity(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	a.coverSvc.EnableNetworkPolicy(a.checkCoverOutbound)
	settings := a.settings.Get()
	settings.MusicBrainz.Contact = ""
	if err := a.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	a.client.HTTP.Transport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("disabled lookup sent HTTP: %s", r.URL)
		return nil, fmt.Errorf("unexpected network")
	})
	r := httptest.NewRecorder()
	a.coverAssetHandler().ServeHTTP(r, httptest.NewRequest("GET", "/cover?provider=itunes&ref=jp:1475065332&usage=detail", nil))
	if r.Code != 403 || !strings.Contains(r.Body.String(), OutboundDisabledReason) {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	key := store.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: "jp:1475065332"}
	if c, err := a.store.GetCoverCatalog(key); err != nil || c != nil {
		t.Fatal("disabled became catalog failure", c, err)
	}
	// 即使缓存了另一地区的专辑，也不能误当目标。
	a.coverSvc.SetDetailLoader(func(context.Context, provider.Ref) (*provider.Detail, error) {
		return &provider.Detail{Provider: provider.ITunes, ReleaseRef: strPtrOrNil("us:1475065332"), ArtworkURL: strPtrOrNil("https://is1.mzstatic.com/100x100bb.jpg")}, nil
	})
	if _, err := a.coverSvc.Resolve(context.Background(), covers.Target{Provider: "itunes", Ref: key.SourceRef}, covers.Detail); err == nil {
		t.Fatal("accepted wrong storefront")
	}
}
