package importer

import (
	"encoding/json"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func inputPriorityCandidates() (provider.Candidate, provider.Candidate) {
	bonus := mbCandidate("f0a33d1a-628a-4a53-bc4e-6af077505956", "(Vocal) you", "霞ヶ丘詩羽(CV.茅野愛衣)", "冴えない彼女の育てかた 第4巻 特典CD")
	collection := mbCandidate("f4458acc-aa40-40b0-a0b7-aaa27df24b04", "you(Vocal)", "霞ヶ丘詩羽(CV.茅野愛衣)", "冴えない彼女の育てかた ギャルゲーカバーソングコレクション")
	for _, c := range []*provider.Candidate{&bonus, &collection} {
		disc, position, duration := 1, 3, 300000
		c.DiscNumber, c.TrackNumber, c.DurationMS = &disc, &position, &duration
		c.RecordingRef = sptr("2283ceda-78b1-4d20-8f52-bdf73bd53686")
		c.ISRC = sptr("JPE301500361")
	}
	return bonus, collection
}

func TestRunnerInputAlbumConstrainsBeforeCrossGroupGuard(t *testing.T) {
	for _, scenario := range []string{"bonus_truncated", "collection", "no_album", "unknown_competitor"} {
		t.Run(scenario, func(t *testing.T) {
			st := openStore(t)
			bonus, collection := inputPriorityCandidates()
			album, want := "冴えない彼女の育てかた 第4巻 特典C...", bonus.MatchRef
			rows := []provider.Candidate{collection, bonus}
			switch scenario {
			case "collection":
				album, want = *collection.AlbumTitle, collection.MatchRef
			case "no_album":
				album, want = "", ""
			case "unknown_competitor":
				unknown := bonus
				unknown.MatchRef, unknown.ReleaseRef, unknown.ReleaseGroupRef, unknown.AlbumTitle = "unknown-track", sptr("unknown-release"), sptr("unknown-group"), nil
				rows = append(rows, unknown)
				want = ""
			}
			searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"you": rows}}
			batch := enqueueOne(t, st, "you", *bonus.ArtistName, album)
			runToCompletion(t, newRunner(st, searcher), batch)
			stats, err := st.Stats(batch)
			if err != nil {
				t.Fatal(err)
			}
			if searcher.queryCount() != 1 {
				t.Fatal("unexpected search count", searcher.queryCount())
			}
			if want == "" {
				if stats.Review != 1 || stats.Imported != 0 || stats.Skipped != 0 {
					t.Fatal(stats)
				}
				items, err := st.AllReviewItems()
				if err != nil || len(items) != 1 || items[0].Candidates == nil {
					t.Fatal(items, err)
				}
				var candidates []matching.ScoredCandidate
				if err := json.Unmarshal([]byte(*items[0].Candidates), &candidates); err != nil || len(candidates) != len(rows) {
					t.Fatal("manual candidates lost", candidates, err)
				}
				return
			}
			if stats.Imported != 1 || stats.Skipped != 0 || stats.Review != 0 {
				t.Fatal(stats)
			}
			var ref, release string
			if err := st.DB().QueryRow("SELECT t.match_ref,r.provider_ref FROM import_item i JOIN track t ON t.id=i.track_id JOIN release r ON r.id=t.release_id WHERE i.batch_id=?", batch).Scan(&ref, &release); err != nil || ref != want {
				t.Fatal(ref, release, err)
			}
			other := bonus.MatchRef
			if want == bonus.MatchRef {
				other = collection.MatchRef
			}
			if id, err := st.FindByMatchRef("musicbrainz", other); err != nil || id != 0 {
				t.Fatal("wrong album also admitted", id, err)
			}
		})
	}
}
