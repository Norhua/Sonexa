package store

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func TestInputScopeRechecksCurrentLocalProofEvenWithoutQueueRevision(t *testing.T) {
	s := openTestStore(t)
	title, artist, album, release := "Song", "Artist", "Album", "release"
	disc, track := 1, 1
	// This test uses a real stored binding plus a source view. The source view
	// can establish the exact target for a missing-album member; the current
	// view records the local revisions whose later change must invalidate it.
	r, err := s.UpsertRelease(&Release{Provider: ptr("fixture"), ProviderRef: &release, Title: album, Credit: &artist})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateTrack(&Track{Title: title, ReleaseID: &r})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProvenance(id, "fixture", "track"); err != nil {
		t.Fatal(err)
	}
	binding, err := s.FindExactBinding(domain.Identity{Provider: "fixture", Ref: "track"})
	if err != nil || binding == nil {
		t.Fatal(binding, err)
	}
	identity := matching.CandidateIdentity{State: matching.IdentityExact, Track: binding.Track, Release: binding.Release}
	c := provider.Candidate{Provider: "fixture", MatchRef: "track", ReleaseRef: &release, Title: &title, ArtistName: &artist, AlbumTitle: &album, DiscNumber: &disc, TrackNumber: &track}
	for _, albumInput := range []string{album, ""} {
		item := enqueueProblemInput(t, s, title, artist, albumInput)
		facts := matching.MetadataFacts{Source: "fixture", AlbumComplete: true, PositionVerified: true, Candidate: c}
		input := matching.ProblemInput{Profile: *item.Profile, Pool: &matching.CandidatePool{Targets: []matching.CandidateTarget{{Identity: identity, Facts: []matching.CandidateFact{
			{Kind: "remote", Metadata: facts},
			{Kind: "current", LocalTrackID: id, TrackRevision: binding.Revision, ReleaseRevision: binding.ReleaseRevision, Metadata: facts},
		}}}}}
		if err := s.MarkReviewWithProblem(item.ID, item.Revision, ReviewAmbiguous, "[]", "[]", "", input); err != nil {
			t.Fatal(err)
		}
	}
	activeProblemCounts(t, s, 1, 2)
	page, err := s.FindProblemCandidates(context.Background(), matching.ProblemInput{Profile: matching.NewInputProfile(title, artist, album)}, 0, 10)
	if err != nil || len(page.Problems) != 1 {
		t.Fatal(page, err)
	}
	token, err := s.ReadProblemScope(page.Problems[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := s.ReadProblemInputScope(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProvenance(id, "fixture", "rebound"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckInputScope(scope); !errors.Is(err, ErrPlanStale) {
		t.Fatal("stale exact proof accepted", err)
	}
	if _, err := s.ReadProblemInputScope(token); !errors.Is(err, ErrPlanStale) {
		t.Fatal("rebound facts manufactured new equivalent scope", err)
	}
}
