package importer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

// Search is a fixed bounded set; decision facts use the real MB adapter, HTTP
// cache and native fixture. No production endpoint is constructed in this test.
type decisionReplaySearcher struct {
	*musicbrainz.Provider
	rows []provider.Candidate
}

func (s *decisionReplaySearcher) SearchTracks(context.Context, provider.Query) ([]provider.Candidate, error) {
	return s.rows, nil
}

func TestRunnerDecisionFactsReplayAndRevisionBoundary(t *testing.T) {
	body, err := os.ReadFile("../provider/musicbrainz/testdata/identity_repeated_recording.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"second_disc", "repeated_position", "partial_list", "failed_request", "stale_input"} {
		t.Run(scenario, func(t *testing.T) {
			st := openStore(t)
			profile := matching.NewInputProfile("you", "歌手", "特典CD")
			if scenario != "repeated_position" {
				profile.Disc = ParsePositionEvidence("2", "disc")
				profile.Track = ParsePositionEvidence("1", "track")
			}
			batch, err := st.CreateBatch(&store.ImportBatch{Filename: "decision-replay.csv", TotalRows: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: `["you","歌手","特典CD"]`, InTitle: sptr("you"), InArtist: sptr("歌手"), InAlbum: sptr("特典CD"), Profile: &profile}}); err != nil {
				t.Fatal(err)
			}
			item, err := st.ClaimNextItem(batch)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/ws/2/release/11111111-1111-4111-8111-111111111111" {
					t.Error("unexpected release/group request", r.URL.Path)
					http.Error(w, "unexpected path", 400)
					return
				}
				if scenario == "failed_request" {
					http.Error(w, "fixture decision failure", 400)
					return
				}
				if scenario == "stale_input" {
					if err := st.MarkReviewAtRevision(item.ID, item.Revision, store.ReviewAmbiguous, "[]", "[]", "newer review"); err != nil {
						t.Error(err)
					}
				}
				response := body
				if scenario == "partial_list" {
					response = []byte(strings.Replace(string(body), `"medium-count": 2`, `"medium-count": 3`, 1))
				}
				_, _ = w.Write(response)
			}))
			defer srv.Close()
			client := provider.NewClient(nil, st.AsProviderCache(), "test")
			mb := musicbrainz.New(client, musicbrainz.Config{Contact: "fixture", ServerURL: srv.URL}, store.ParamsHash)
			const release = "11111111-1111-4111-8111-111111111111"
			const recording = "2283ceda-78b1-4d20-8f52-bdf73bd53686"
			seed := provider.Candidate{Provider: provider.MusicBrainz, MatchRef: recording + "@" + release, ReleaseRef: sptr(release), RecordingRef: sptr(recording), Title: sptr("you"), ArtistName: sptr("歌手"), AlbumTitle: sptr("特典CD")}
			excluded := seed
			excluded.ReleaseRef = sptr("22222222-2222-4222-8222-222222222222")
			excluded.MatchRef = recording + "@" + *excluded.ReleaseRef
			excluded.AlbumTitle = sptr("別作品集")
			runner := newRunner(st, &decisionReplaySearcher{Provider: mb, rows: []provider.Candidate{seed, excluded, seed}})
			runner.retry.MaxAttempts = 1
			line := runner.processItem(context.Background(), item)
			got, err := st.GetItem(item.ID)
			if err != nil || calls.Load() != 1 {
				t.Fatal(got, err, calls.Load(), line)
			}
			if scenario == "second_disc" {
				if got.Status != store.StatusImported || got.TrackID == nil {
					t.Fatal(got, line)
				}
				id, err := st.FindByMatchRef("musicbrainz", "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
				if err != nil || id != *got.TrackID {
					t.Fatal("did not select actual second-disc track", id, err)
				}
				return
			}
			if scenario == "failed_request" {
				if got.Status != store.StatusFailed || got.LastError == nil {
					t.Fatal(got, line)
				}
			} else if got.Status != store.StatusReview {
				t.Fatal("insufficient/replaced input was automatically completed", got, line)
			}
			if scenario == "stale_input" && (got.ReviewEvidence == nil || *got.ReviewEvidence != "newer review") {
				t.Fatal("late decision overwrote current revision", got)
			}
			if scenario == "repeated_position" {
				var rows []matching.ScoredCandidate
				if got.Candidates == nil || json.Unmarshal([]byte(*got.Candidates), &rows) != nil || len(rows) != 3 {
					t.Fatal("must retain two exact positions and excluded manual candidate", got)
				}
			}
			if count, err := st.CountTracks(store.TrackFilter{}); err != nil || count != 0 || got.TrackID != nil || (got.ResolutionReason != nil && *got.ResolutionReason == store.ResolutionExistingTrack) {
				t.Fatal("review/error created or skipped a collection", got, count, err)
			}
		})
	}
}
