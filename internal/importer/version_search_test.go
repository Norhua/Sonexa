package importer

import (
	"encoding/json"
	"testing"

	"github.com/norhua/sonexa/internal/domain"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestRunnerVersionEquivalenceAndPayloads(t *testing.T) {
	for _, tc := range []struct {
		name, input, target string
		accept              bool
	}{
		{"instrumental_alias", "Song (Instrumental)", "Song (off vocal)", true},
		{"remix_form", "Song - A Remix", "Song (A Remix)", true},
		{"live_form", "Song Live at Tokyo 2020", "Song (Live at Tokyo 2020)", true},
		{"remaster_form", "Song 2020 Remastered", "Song (Remaster 2020)", true},
		{"remix_author", "Song (A Remix)", "Song (B Remix)", false},
		{"live_event", "Song (Live Tokyo 2020)", "Song (Live Osaka 2020)", false},
		{"rerecording_year", "Song (Re-recorded 2020)", "Song (Re-recorded 2021)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := openStore(t)
			c := mbCandidate("version-track", tc.target, "Artist", "Album")
			searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{tc.input: {c}}}
			batch := enqueueOne(t, st, tc.input, "Artist", "Album")
			runToCompletion(t, newRunner(st, searcher), batch)
			stats, err := st.Stats(batch)
			if err != nil {
				t.Fatal(err)
			}
			wantNew, wantReview, wantCalls := 0, 1, 2
			if tc.accept {
				wantNew, wantReview, wantCalls = 1, 0, 1
			}
			if stats.Imported != wantNew || stats.Skipped != 0 || stats.Review != wantReview || stats.Problems.ReviewProblems != wantReview || stats.Failed != 0 || searcher.queryCount() != wantCalls {
				t.Fatal(stats, searcher.queryCount())
			}
			id, err := st.FindByMatchRef(string(c.Provider), c.MatchRef)
			if err != nil || (id > 0) != tc.accept {
				t.Fatal(id, err)
			}
			if n, err := st.CountTracks(store.TrackFilter{}); err != nil || n != wantNew {
				t.Fatal(n, err)
			}
			if tc.accept {
				var body string
				if err := st.DB().QueryRow("SELECT body FROM track_snapshot WHERE track_id=?", id).Scan(&body); err != nil {
					t.Fatal(err)
				}
				var snapshot domain.TrackSnapshot
				if err := json.Unmarshal([]byte(body), &snapshot); err != nil || snapshot.Title.Value != tc.target {
					t.Fatal("query overwrote source title", snapshot, err)
				}
			}
			t.Logf("D10 %s: new=%d skip=%d problems=%d false_auto=0 searches=%d", tc.name, stats.Imported, stats.Skipped, stats.Problems.ReviewProblems, searcher.queryCount())
		})
	}
}
