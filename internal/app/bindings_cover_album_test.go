package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/provider/caa"
	"github.com/norhua/sonexa/internal/store"
)

type coverReplayTransport func(*http.Request) (*http.Response, error)

func (f coverReplayTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTetraCoverHandlerReplaysMediumOnlyIndex(t *testing.T) {
	// Pool 的客户端默认使用 DefaultTransport；测试不并行，并拒绝一切真实出网。
	body, err := os.ReadFile("../../testdata/golden/responses/musicbrainz/caa_lines.json")
	if err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var mu sync.Mutex
	var paths []string
	http.DefaultTransport = coverReplayTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		status, data := 404, ""
		if r.URL.Path == "/release/2659e6f6-ce21-4198-92e5-4878f5e7e3c0" {
			status, data = 200, string(body)
		}
		if strings.HasSuffix(r.URL.Path, "/37269419555-250.jpg") {
			status, data = 200, string(jpegHeader())
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(data)), Header: make(http.Header)}, nil
	})
	app, cleanup := newTestApp(t)
	defer cleanup()
	ref := "2659e6f6-ce21-4198-92e5-4878f5e7e3c0"
	for attempt := 0; attempt < 2; attempt++ {
		rec := httptest.NewRecorder()
		app.coverAssetHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/cover?provider=musicbrainz&ref="+ref+"&rg=e8ec3011-cd49-4558-9d11-01f7fe9b552f&size=250", nil))
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
			t.Fatalf("response %d %s", rec.Code, rec.Body.String())
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 6 {
		t.Fatalf("wanted 4 front attempts + index + actual image, then cache hit: %v", paths)
	}
	if got := app.CoverOutcomes([]CoverSource{{Provider: "musicbrainz", Ref: ref, Size: 250}}); got[0] != string(covers.OutcomeOK) {
		t.Fatal(got)
	}
	// 预取也使用同一条包含索引的链，不再要求先渲染一次。
	refs := app.BuildCoverRefs([]CandidateRow{{Provider: "musicbrainz", ReleaseRef: &ref}})
	if len(refs) != 1 || refs[0].URLs[len(refs[0].URLs)-1] != caa.CoverArtArchiveBaseURL+"/release/"+ref {
		t.Fatal(refs)
	}
}

func TestAlbumDetailDoesNotRedirectSameRecording(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "CD track", MatchSource: strPtrOrNil("musicbrainz"), MatchRef: strPtrOrNil("cd-track"), Metadata: &store.MetadataInput{RecordingRef: strPtrOrNil("recording")}})
	if err != nil {
		t.Fatal(err)
	}
	releaseID, err := app.store.UpsertRelease(&store.Release{Title: "Digital", Provider: strPtrOrNil("musicbrainz"), ProviderRef: strPtrOrNil("digital"), TracklistState: store.TracklistComplete})
	if err != nil {
		t.Fatal(err)
	}
	err = app.store.ReplaceTracklist(releaseID, []store.ReleaseTrack{{DiscNumber: 1, TrackNumber: 1, Title: "Digital track", TrackRef: strPtrOrNil("digital-track"), RecordingRef: strPtrOrNil("recording")}}, store.TracklistComplete)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := app.GetAlbumDetail(releaseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Rows) != 1 || !detail.Rows[0].Ghost || detail.Rows[0].TrackID != 0 || detail.Rows[0].State != "missing" || detail.OwnedCount != 0 {
		t.Fatalf("must not redirect to %d: %+v", id, detail)
	}
}
