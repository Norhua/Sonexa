package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

// Replay a real MB response through the actual HTTP parser and all four entry
// points. No real application data or network is used by this regression.
func TestMusicBrainzVersionRecallAcrossEntrypoints(t *testing.T) {
	body, err := os.ReadFile("../../testdata/golden/responses/musicbrainz/search_version_recall.json")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/2/recording" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.Error(w, "unexpected", 400)
			return
		}
		q := r.URL.Query().Get("query")
		mu.Lock()
		queries = append(queries, q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// Deliberately empty the anchored levels to exercise the fallback;
		// the bounded live check also verified the real anchored query succeeds.
		if q != `recording:"悲しくて lulululu"` {
			_, _ = w.Write([]byte(`{"recordings":[]}`))
			return
		}
		_, _ = w.Write(body)
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

	const title = "悲しくて lulululu (难忍的悲伤)"
	const artist = "早坂爱"
	const album = "『かぐや様は告らせたい-ウルトラロマンティック-』第1巻 特典CD OST Vol.1"
	const target = "d2895728-1e49-42e6-81fd-75e1be292099"
	wanted := map[string]string{
		"4b0067e8-88f2-450f-af2c-a773b76130a6": "Kanashikute lulululu -Hayasaka ver.-",
		"f978b91f-8ccb-40c1-a4b7-d54a29ef697e": "Kanashikute lulululu -Hayasaka Version-",
		"08c69ced-4a7e-4e3c-8893-957e27620817": "悲しくて lulululu -ハーサカver.-",
		target:                                 "悲しくて lulululu -ハーサカ Ver.-",
	}
	assertRows := func(rows []CandidateRow) CandidateRow {
		t.Helper()
		if len(rows) != 8 {
			t.Fatalf("want 4 vocal + 4 instrumental tracks, excluding unrelated artist: %d", len(rows))
		}
		found := map[string]string{}
		var selected CandidateRow
		for _, row := range rows {
			if want, ok := wanted[row.MatchRef]; ok {
				if row.Title == nil || *row.Title != want {
					t.Fatalf("track title overwritten: %+v", row)
				}
				found[row.MatchRef] = *row.Title
			}
			if !row.ReviewOnly || !strings.Contains(row.Note, "人工确认") {
				t.Fatalf("missing warning: %+v", row)
			}
			if row.MatchRef == target {
				selected = row
			}
		}
		if !reflect.DeepEqual(found, wanted) {
			t.Fatalf("missing release tracks: %v", found)
		}
		if selected.TrackNumber == nil || *selected.TrackNumber != 2 || selected.MediumFormat == nil || *selected.MediumFormat != "CD" {
			t.Fatalf("target position lost: %+v", selected)
		}
		return selected
	}

	// Verify parser retention independently of scoring.
	raw, err := p.SearchTracks(context.Background(), provider.Query{Title: "悲しくて lulululu", TitleOnly: true})
	if err != nil || len(raw) != 12 {
		t.Fatalf("parse: %d %v", len(raw), err)
	}
	for _, c := range raw {
		if c.MatchRef == "4b0067e8-88f2-450f-af2c-a773b76130a6" && (len(c.SearchTitles) != 1 || c.SearchTitles[0] != wanted[target]) {
			t.Fatalf("recording search title lost: %+v", c)
		}
	}

	path := filepath.Join(t.TempDir(), "input.csv")
	if err := os.WriteFile(path, []byte("title,artist,album\n"+title+","+artist+","+album+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	batch, err := a.SubmitImport(path, "", []string{"title", "artist", "album"}, "UTF-8", ",", true)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "import completes", func() bool { return !a.runner.IsRunning(batch) })
	queue, err := a.GetReviewQueue()
	if err != nil || queue.Total != 1 {
		t.Fatal(queue, err)
	}
	item := queue.Items[0]
	assertRows(item.Candidates)
	if item.Status != string(store.StatusReview) {
		t.Fatal(item.Status)
	}
	if n, _ := a.store.CountTracks(store.TrackFilter{}); n != 0 {
		t.Fatal("review-only candidate auto-imported")
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
	if queue.Items[0].InTitle == nil || *queue.Items[0].InTitle != title {
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

	// A human may explicitly choose the exact CD track; no extra search/detail
	// is necessary and the original warning is not an admission prohibition.
	accepted, err := a.AcceptCandidate(item.ID, selected)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := a.store.FindByMatchRef("musicbrainz", target)
	if err != nil || stored != accepted {
		t.Fatal(stored, accepted, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queries) != 3 {
		t.Fatalf("expected three cached query levels, got %v", queries)
	}
	for _, q := range queries {
		if strings.Contains(q, "难忍的悲伤") && strings.Contains(q, "悲しくて") {
			t.Fatal("translation left in phrase query")
		}
	}
}
