package app

import (
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestMetadataVersionSemanticsAcrossManualSearchEntries(t *testing.T) {
	for _, tc := range []struct {
		name, input, target string
		show                bool
	}{
		{"equivalent", "Song (Instrumental)", "Song (off vocal)", true},
		{"remix_conflict", "Song (A Remix)", "Song (B Remix)", false},
		{"live_conflict", "Song (Live Tokyo 2020)", "Song (Live Osaka 2020)", false},
		{"rerecord_conflict", "Song (Re-recorded 2020)", "Song (Re-recorded 2021)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			a.enricher.Pause()
			c := fullCandidate("versions")
			c.Title, c.ArtistName, c.AlbumTitle = strPtr(tc.target), strPtr("Artist"), strPtr("Album")
			base, err := a.registry.Get(provider.MusicBrainz)
			if err != nil {
				t.Fatal(err)
			}
			searcher := &recordingSearcher{MetadataProvider: base, hit: []provider.Candidate{c}}
			a.registry.Register(searcher)
			check := func(rows []CandidateRow, err error) {
				t.Helper()
				want := 0
				if tc.show {
					want = 1
				}
				if err != nil || len(rows) != want {
					t.Fatal(rows, err)
				}
				if tc.show && (rows[0].MatchRef != c.MatchRef || *rows[0].Title != tc.target) {
					t.Fatal("source fact changed", rows)
				}
			}
			search, err := a.SearchUpstream(tc.input, "Artist", "Album", []string{"musicbrainz"}, "")
			if err != nil {
				t.Fatal(err)
			}
			check(search.Aggregate, nil)
			item := enqueueOneItem(t, a, tc.input, "Artist", "Album")
			if err := a.store.MarkReview(item, store.ReviewNoMatch, "[]", "[]"); err != nil {
				t.Fatal(err)
			}
			scope, err := a.store.ItemProblemScope(item)
			if err != nil {
				t.Fatal(err)
			}
			check(a.ResearchReviewProblem(scope, store.ReviewQuery{Version: 1, Title: tc.input, Artist: "Artist", Album: "Album", Provider: "musicbrainz"}))
			check(a.ResearchReviewItem(item, tc.input, "Artist", "Album", "musicbrainz", ""))
			local, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "Editor track"})
			if err != nil {
				t.Fatal(err)
			}
			check(a.SearchEditCandidates(local, "musicbrainz", tc.input, "Artist", "Album", ""))
		})
	}
}
