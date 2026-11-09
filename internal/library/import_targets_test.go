package library

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func TestAdmissionTargetsAreServerDerivedAndMergeInvalidatesPlan(t *testing.T) {
	s, st, _ := newTestService(t)
	title := "Song"
	batch, err := st.CreateBatch(&store.ImportBatch{TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	input := store.ImportItem{RawRow: `["Song"]`, InTitle: &title}
	if err := st.EnqueueItems(batch, []store.ImportItem{input}); err != nil {
		t.Fatal(err)
	}
	item, err := st.ClaimNextItem(batch)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	seed := domain.AdmissionSeed{ImportItemID: item.ID, Snapshot: domain.Snapshot{Track: &domain.TrackSnapshot{Title: domain.Known(title)}}, Destinations: []domain.ImportTarget{{InputID: 999, Kind: "synced_playlist"}}}
	plan, err := s.PrepareAdmission(context.Background(), seed)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Seed.Destinations) != 1 || plan.Seed.Destinations[0].InputID != item.ID || plan.Seed.Destinations[0].Kind != "library" {
		t.Fatal(plan.Seed.Destinations)
	}
	other, err := st.CreateBatch(&store.ImportBatch{TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueItems(other, []store.ImportItem{input}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
	fresh, err := s.PrepareAdmission(context.Background(), seed)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Seed.Destinations) != 2 {
		t.Fatal(fresh.Seed.Destinations)
	}
	if err := st.MarkReview(item.ID, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	fresh, err = s.PrepareAdmission(context.Background(), seed)
	if err != nil {
		t.Fatal(err)
	}
	forged := *fresh
	forged.Seed.Destinations = nil
	forged.Hash, _ = store.AdmissionHash(forged)
	if _, err := st.CommitAdmission(forged, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
}
