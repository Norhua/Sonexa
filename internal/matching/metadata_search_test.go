package matching

import (
	"context"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

type versionSearcher struct {
	candidate provider.Candidate
	calls     int
}

func (s *versionSearcher) SearchTracks(context.Context, provider.Query) ([]provider.Candidate, error) {
	s.calls++
	return []provider.Candidate{s.candidate}, nil
}

func TestMetadataSearchVersionSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, input, target string
		show                bool
	}{
		{"instrumental_alias", "Song (Instrumental)", "Song (off vocal)", true},
		{"reverse_alias", "Song (off vocal)", "Song (Instrumental)", true},
		{"remix_form", "Song - A Remix", "Song (A Remix)", true},
		{"live_form", "Song Live at Tokyo 2020", "Song (Live at Tokyo 2020)", true},
		{"remaster_form", "Song 2020 Remastered", "Song (Remaster 2020)", true},
		{"ordinary_vocal", "(Vocal) Song", "Song", true},
		{"unknown_annotation", "Song (unexplained)", "Song", true},
		{"remix_author", "Song (A Remix)", "Song (B Remix)", false},
		{"live_event", "Song (Live Tokyo 2020)", "Song (Live Osaka 2020)", false},
		{"rerecording_year", "Song (Re-recorded 2020)", "Song (Re-recorded 2021)", false},
		{"normal_instrumental", "Song", "Song (off vocal)", false},
		{"movie_version", "Tetra.", "Tetra. -movie ver.-", false},
		{"different_song", "Unrelated Song (Instrumental)", "Another Title (off vocal)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := highConfCandidate("track", tc.target, "Artist", "Album", nil)
			original := c
			query := provider.Query{Title: tc.input, Artist: "Artist", Album: "Album"}
			legacy := DefaultWeights().ScoreCandidate(query, c)
			s := &versionSearcher{candidate: c}
			got, err := testEngine().Run(context.Background(), s, nil, Input{Title: tc.input, Artist: "Artist", Album: "Album"})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if tc.show {
				want = 1
			}
			if len(got.Scored) != want {
				t.Fatalf("candidates=%d want=%d, legacy=%+v", len(got.Scored), want, legacy)
			}
			if tc.show && (s.calls != 1 || got.Scored[0].Score.Total != 95 || !got.Scored[0].Score.HighConf || !reflect.DeepEqual(got.Scored[0].Candidate, original)) {
				t.Fatal("score/source changed", got.Scored, s.calls)
			}
			if !reflect.DeepEqual(legacy, DefaultWeights().ScoreCandidate(query, c)) {
				t.Fatal("metadata search mutated legacy scoring")
			}
			if tc.name == "instrumental_alias" && !legacy.Rejected {
				t.Fatal("legacy lyrics comparison changed")
			}
		})
	}
}
