package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestQueueCASRejectsStaleActiveAndTerminalWrites(t *testing.T) {
	s := openTestStore(t)
	item, err := s.ClaimNextItem(seedBatch(t, s, 1))
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	if err := s.MarkReviewAtRevision(item.ID, item.Revision, ReviewAmbiguous, `[{"ref":"current"}]`, `[]`); err != nil {
		t.Fatal(err)
	}
	before, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, write := range []func() error{
		func() error { return s.MarkReviewAtRevision(item.ID, item.Revision, ReviewNoMatch, `[]`, `[]`) },
		func() error { return s.RetryItem(item.ID, item.Revision) },
		func() error {
			_, err := s.RecordFailure(item.ID, "stale error", time.Second, 1, item.Revision)
			return err
		},
	} {
		if err := write(); !errors.Is(err, ErrPlanStale) {
			t.Fatalf("stale write accepted: %v", err)
		}
		got, err := s.GetItem(item.ID)
		if err != nil || !reflect.DeepEqual(before, got) {
			t.Fatal("failed CAS mutated input", got, err)
		}
	}
	if err := s.MarkDiscarded(item.ID); err != nil {
		t.Fatal(err)
	}
	terminal, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, write := range []func() error{
		func() error { return s.MarkReview(item.ID, ReviewAmbiguous, `[]`, `[]`) },
		func() error { return s.RetryItem(item.ID) },
		func() error { _, err := s.RecordFailure(item.ID, "late error", time.Second, 1); return err },
		func() error { return s.MarkSkipped(item.ID) },
		func() error { return s.MarkImported(item.ID, 123) },
	} {
		if err := write(); !errors.Is(err, ErrPlanStale) {
			t.Fatalf("terminal write accepted: %v", err)
		}
		got, err := s.GetItem(item.ID)
		if err != nil || !reflect.DeepEqual(terminal, got) {
			t.Fatal("terminal input mutated", got, err)
		}
	}
}
