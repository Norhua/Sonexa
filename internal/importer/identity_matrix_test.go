package importer

import (
	"encoding/json"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// D10 end-to-end decision matrix: the real Runner/library/store path must not
// turn display similarity or a high score into an automatic identity decision.
func TestRunnerIdentityValidationMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, title, artist, album                      string
		candidateTitle, candidateArtist, candidateAlbum string
		disc, duration                                  int
		accept                                          bool
	}{
		{name: "explicit_aliases", title: "Ｓｏｎｇ（译名：歌曲）", artist: "Artist（译名：艺术家）", album: "Album（译名：专辑）", accept: true},
		{name: "ordinary_vocal", title: "Song", artist: "Artist", album: "Album", candidateTitle: "(Vocal) Song", accept: true},
		{name: "duration_3s", title: "Song", artist: "Artist", album: "Album", duration: 623000, accept: true},
		{name: "duration_10s", title: "Song", artist: "Artist", album: "Album", duration: 630000, accept: true},
		{name: "duration_over_10s", title: "Song", artist: "Artist", album: "Album", duration: 630001},
		{name: "unknown_annotation", title: "Song (unexplained)", artist: "Artist", album: "Album"},
		{name: "pure_translation", title: "歌曲", artist: "Artist", album: "Album"},
		{name: "remix_author", title: "Song (A Remix)", artist: "Artist", album: "Album", candidateTitle: "Song (B Remix)"},
		{name: "live_event", title: "Song (Live at Tokyo)", artist: "Artist", album: "Album", candidateTitle: "Song (Live at Osaka)"},
		{name: "off_vocal", title: "Song", artist: "Artist", album: "Album", candidateTitle: "Song (Off Vocal)"},
		{name: "different_role", title: "Song", artist: "角色A(CV.X)", album: "Album", candidateArtist: "角色B(CV.X)"},
		{name: "different_collaboration", title: "Song", artist: "Artist feat. A", album: "Album", candidateArtist: "Artist feat. B"},
		{name: "explicit_deluxe", title: "Song", artist: "Artist", album: "Album (Deluxe Edition)"},
		{name: "short_album_prefix", title: "Song", artist: "Artist", album: "Album...", candidateAlbum: "Album Long Name"},
		{name: "wrong_disc", title: "Song", artist: "Artist", album: "Album", disc: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := openStore(t)
			c := mbCandidate("matrix-track", "Song", "Artist", "Album")
			if tc.candidateTitle != "" {
				c.Title = sptr(tc.candidateTitle)
			}
			if tc.candidateArtist != "" {
				c.ArtistName, c.AlbumCredit = sptr(tc.candidateArtist), sptr(tc.candidateArtist)
			}
			if tc.candidateAlbum != "" {
				c.AlbumTitle = sptr(tc.candidateAlbum)
			}
			disc, position, duration, relevance := 2, 3, 620000, 100
			c.DiscNumber, c.TrackNumber, c.DurationMS, c.UpstreamScore = &disc, &position, &duration, &relevance
			profile := matching.NewInputProfile(tc.title, tc.artist, tc.album)
			if tc.disc != 0 {
				profile.Disc = matching.NumberInput{State: matching.ValueKnown, Value: int64(tc.disc), Unit: "number", Semantic: "disc", Precision: 1}
			}
			if tc.duration != 0 {
				profile.Duration = matching.NumberInput{State: matching.ValueKnown, Value: int64(tc.duration), Unit: "ms", Semantic: "duration", Precision: 1}
			}
			batch, err := st.CreateBatch(&store.ImportBatch{Filename: tc.name, TotalRows: 1})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal([]string{tc.title, tc.artist, tc.album})
			if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: string(raw), Profile: &profile, InTitle: sptr(tc.title), InArtist: sptr(tc.artist), InAlbum: sptr(tc.album)}}); err != nil {
				t.Fatal(err)
			}
			searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{tc.title: {c}}}
			runToCompletion(t, newRunner(st, searcher), batch)
			stats, err := st.Stats(batch)
			if err != nil {
				t.Fatal(err)
			}
			wantNew, wantReview := 0, 1
			if tc.accept {
				wantNew, wantReview = 1, 0
			}
			if stats.Imported != wantNew || stats.Review != wantReview || stats.Failed != 0 || stats.Skipped != 0 || stats.Problems.ReviewProblems != wantReview || stats.Problems.SourceRows != wantReview {
				t.Fatal(stats)
			}
			id, err := st.FindByMatchRef("musicbrainz", c.MatchRef)
			if err != nil || (id != 0) != tc.accept {
				t.Fatal("wrong target", id, err)
			}
			wantCalls := 1
			if tc.name == "pure_translation" || tc.name == "off_vocal" || tc.name == "remix_author" || tc.name == "live_event" {
				wantCalls = 2
			}
			if searcher.queryCount() != wantCalls {
				t.Fatal("unexpected search count", searcher.queryCount(), wantCalls)
			}
			if count, err := st.CountTracks(store.TrackFilter{}); err != nil || count != wantNew {
				t.Fatal(count, err)
			}
			t.Logf("D10 %s: new=%d skip=%d problems=%d source_rows=%d false_auto=0 searches=%d", tc.name, stats.Imported, stats.Skipped, stats.Problems.ReviewProblems, stats.Problems.SourceRows, searcher.queryCount())
		})
	}
}
