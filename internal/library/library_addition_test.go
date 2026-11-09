package library

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func TestExplicitLibraryAdditionPlansAndExactReuse(t *testing.T) {
	s, st, clock := newTestService(t)
	id, err := st.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "hidden"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PrepareLibraryAddition(context.Background(), []int64{id, id})
	if err != nil || len(p.WillAdd) != 1 {
		t.Fatal(p, err)
	}
	track, err := st.GetTrack(id)
	if err != nil || track.InLibrary {
		t.Fatal(track, err)
	}
	s.CancelLibraryAddition(p.ID)
	if _, err := s.CommitLibraryAddition(context.Background(), p.ID, p.Hash); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
	p, err = s.PrepareLibraryAddition(context.Background(), []int64{id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitLibraryAddition(context.Background(), p.ID, "forged"); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
	if _, err := st.CommitLibraryAddition(context.Background(), "concurrent", "hash", []int64{id}); err != nil {
		t.Fatal(err)
	}
	result, err := s.CommitLibraryAddition(context.Background(), p.ID, p.Hash)
	if err != nil || len(result.WillAdd) != 0 || len(result.AlreadyInLibrary) != 1 {
		t.Fatal(result, err)
	}
	clock.now = clock.now.Add(PlanTTL)
	if _, err := s.CommitLibraryAddition(context.Background(), p.ID, p.Hash); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
	// An explicit exact identity request is a membership operation, not a copy.
	hidden, err := st.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "upstream", MatchSource: ptrField(domain.Known("test")), MatchRef: ptrField(domain.Known("ref"))})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.PrepareAdmission(context.Background(), domain.AdmissionSeed{Snapshot: domain.Snapshot{Track: &domain.TrackSnapshot{Identity: domain.Identity{Provider: "test", Ref: "ref"}}}})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{})
	if err != nil || !admitted.Existing || admitted.TrackID != hidden {
		t.Fatal(admitted, err)
	}
	track, err = st.GetTrack(hidden)
	if err != nil || !track.InLibrary || track.Title != "upstream" {
		t.Fatal(track, err)
	}
}
