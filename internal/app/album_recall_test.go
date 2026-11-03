package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

func TestMusicBrainzAlbumRecallAcrossEntrypoints(t *testing.T) {
	readFixture := func(name string) []byte {
		t.Helper()
		body, err := os.ReadFile("../../testdata/golden/responses/musicbrainz/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	anchored := readFixture("search_portamento_anchored.json")
	titleOnly := readFixture("search_portamento.json")
	const title = "portamento"
	const artist = "川田麻美"
	const album = "TVアニメ「灼眼のシャナ」ベストアルバム灼眼のシャナ-BEST-"
	const target = "a2079f0f-906c-4f64-b03c-9bdd36d5ebdb"
	const other = "72d6d601-73d7-4b5e-afd1-5d0c7655f2d2"
	const anchoredQuery = `recording:"portamento" AND (release:"TVアニメ「灼眼のシャナ」ベストアルバム灼眼のシャナ\-BEST\-" OR artist:"川田麻美")`
	var anchoredCalls, titleCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/2/recording" || r.URL.Query().Get("limit") != "25" {
			t.Errorf("unexpected request: %s", r.URL)
			http.Error(w, "unexpected request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("query") {
		case anchoredQuery:
			anchoredCalls.Add(1)
			_, _ = w.Write(anchored)
		case `recording:"portamento"`:
			titleCalls.Add(1)
			_, _ = w.Write(titleOnly)
		default:
			t.Errorf("unexpected query: %s", r.URL.Query().Get("query"))
			http.Error(w, "unexpected query", 400)
		}
	}))
	defer srv.Close()
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	a.limiters.Configure(provider.MusicBrainz, 1000, time.Second)
	client := provider.NewClient(a.limiters, a.store.AsProviderCache(), "Sonexa/test ( test@example.com )")
	p := musicbrainz.New(client, musicbrainz.Config{Contact: "test@example.com", ServerURL: srv.URL}, store.ParamsHash)
	a.registry.Register(p)
	a.rebuildRunner(a.settings.Get())

	// Actual first page already contains the target; pagination and parsing are
	// not responsible for this reproduction. Strict local corroboration is.
	raw, err := p.SearchTracks(context.Background(), provider.Query{Title: title, TitleOnly: true})
	if err != nil || len(raw) != 36 {
		t.Fatalf("raw first-page tracks=%d: %v", len(raw), err)
	}
	found := false
	for _, c := range raw {
		if c.MatchRef == target {
			found = true
			s := matching.DefaultWeights().ScoreCandidate(provider.Query{Title: title, Artist: artist, Album: album}, c)
			if s.Plausible || s.TitleMatch != matching.MatchExact || s.CreditMatch != matching.MatchNone || s.AlbumMatch != matching.MatchNone {
				t.Fatalf("strict diagnosis changed: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("target missing from raw parser")
	}
	result, err := matching.NewEngine(config.Defaults().Matching).Run(context.Background(), p, nil, matching.Input{Title: title, Artist: artist, Album: album})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scored) != 2 || len(result.TriedVariants) != 2 || len(matching.AutomaticCandidates(result.Scored)) != 0 {
		t.Fatalf("want two manual candidates after two levels, got %+v", result)
	}

	assertRows := func(rows []CandidateRow) CandidateRow {
		t.Helper()
		if len(rows) != 2 {
			t.Fatalf("want two requested-album tracks, got %d: %+v", len(rows), rows)
		}
		seen := map[string]bool{}
		var selected CandidateRow
		for _, row := range rows {
			if (row.MatchRef != target && row.MatchRef != other) || seen[row.MatchRef] {
				t.Fatalf("unrelated, duplicate or synthetic identity: %+v", row)
			}
			seen[row.MatchRef] = true
			if !row.ReviewOnly || row.Note == "" || row.Title == nil || *row.Title != title || row.Artist == nil || *row.Artist != "川田まみ" || row.DiscNumber == nil || *row.DiscNumber != 2 || row.TrackNumber == nil || *row.TrackNumber != 4 {
				t.Fatalf("lost review guard or upstream facts: %+v", row)
			}
			if row.Album == nil || row.ReleaseRef == nil || row.MediumFormat == nil || *row.MediumFormat != "CD" {
				t.Fatalf("lost release fields: %+v", row)
			}
			if row.MatchRef == target {
				if *row.Album != "灼眼のシャナ -BEST-" || *row.ReleaseRef != "fa61ac5d-447b-4698-817e-afd7faba11b2" {
					t.Fatalf("target release changed: %+v", row)
				}
				selected = row
			} else if *row.Album != "灼眼のシャナ-BEST-" || *row.ReleaseRef != "eb7cc685-a384-4eaa-96b6-1b4225c35bf5" {
				t.Fatalf("second release changed: %+v", row)
			}
		}
		return selected
	}
	path := filepath.Join(t.TempDir(), "input.csv")
	if err := os.WriteFile(path, []byte("title,artist,album\n"+title+","+artist+","+album+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	batch, err := a.SubmitImport(path, "", []string{"title", "artist", "album"}, "UTF-8", ",", true)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "album recall import", func() bool { return !a.runner.IsRunning(batch) })
	queue, err := a.GetReviewQueue()
	if err != nil || queue.Total != 1 {
		t.Fatal(queue, err)
	}
	item := queue.Items[0]
	assertRows(item.Candidates)
	if n, err := a.store.CountTracks(store.TrackFilter{}); err != nil || n != 0 {
		t.Fatal("album phrase auto-imported", n, err)
	}
	// Simulate the persisted empty snapshot from an older build. Merely reading
	// the queue must not start networking; explicit re-search rescans cached raw
	// HTTP data using the corrected local policy.
	if err := a.store.MarkReview(item.ID, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	queue, err = a.GetReviewQueue()
	if err != nil || queue.Total != 1 || len(queue.Items[0].Candidates) != 0 {
		t.Fatal(queue, err)
	}
	rows, err := a.ResearchReviewItem(item.ID, title, artist, album, "musicbrainz", "")
	if err != nil {
		t.Fatal(err)
	}
	selected := assertRows(rows)
	queue, err = a.GetReviewQueue()
	if err != nil || queue.Total != 1 {
		t.Fatal(queue, err)
	}
	assertRows(queue.Items[0].Candidates)
	if queue.Items[0].InAlbum == nil || *queue.Items[0].InAlbum != album || queue.Items[0].InArtist == nil || *queue.Items[0].InArtist != artist {
		t.Fatal("original input rewritten")
	}
	refreshed, err := a.RefreshSearchCandidates(rows)
	if err != nil {
		t.Fatal(err)
	}
	assertRows(refreshed)
	search, err := a.SearchUpstream(title, artist, album, []string{"musicbrainz"}, "")
	if err != nil || search.ByProvider[0].Error != "" {
		t.Fatal(search, err)
	}
	assertRows(search.Aggregate)
	id, err := a.CreateTrackManual("Local", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = a.SearchEditCandidates(id, "musicbrainz", title, artist, album, "")
	if err != nil {
		t.Fatal(err)
	}
	assertRows(rows)
	accepted, err := a.AcceptCandidate(item.ID, selected)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := a.store.FindByMatchRef("musicbrainz", target)
	if err != nil || stored != accepted {
		t.Fatal(stored, accepted, err)
	}
	if anchoredCalls.Load() != 1 || titleCalls.Load() != 1 {
		t.Fatalf("unexpected extra requests: anchored=%d title=%d", anchoredCalls.Load(), titleCalls.Load())
	}
	t.Log("真实第一页 36 tracks → 两个原始发行位置仅供人工确认；四入口、快照、接受通过，共两次缓存 HTTP 重放")
}
