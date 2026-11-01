package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func TestReviewProtocolRequiresVersion(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"version":0}`, `{"version":2}`, `[]`} {
		if _, err := DecodeReviewCandidates(raw); err == nil {
			t.Fatal("unversioned facts accepted", raw)
		}
	}
}

func TestProblemQueryCASRejectsLateResultsAndScopeChanges(t *testing.T) {
	for _, change := range []string{"new_query", "new_member", "split", "terminal", "none_matched"} {
		t.Run(change, func(t *testing.T) {
			s := openTestStore(t)
			a := enqueueProblemInput(t, s, "Song", "Artist", "Album")
			if err := s.MarkReview(a.ID, ReviewNoMatch, "[]", "[]"); err != nil {
				t.Fatal(err)
			}
			scope, err := s.ItemProblemScope(a.ID)
			if err != nil {
				t.Fatal(err)
			}
			query := ReviewQuery{Version: ReviewProtocolVersion, Title: "Manual", Provider: "fixture"}
			token, err := s.BeginProblemQuery(scope, query)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "new_query":
				if _, err := s.BeginProblemQuery(token, query); err != nil {
					t.Fatal(err)
				}
			case "new_member", "split":
				b := enqueueProblemInput(t, s, "Song", "Artist", "Album")
				if err := s.MarkReview(b.ID, ReviewNoMatch, "[]", "[]"); err != nil {
					t.Fatal(err)
				}
				if change == "split" {
					current, _ := s.ItemProblemScope(a.ID)
					item, _ := s.GetItem(b.ID)
					if _, err := s.SplitProblemItem(current.ProblemID, current.Revision, b.ID, item.Revision); err != nil {
						t.Fatal(err)
					}
				}
			case "terminal":
				if err := s.DiscardProblemMembers(token, []int64{a.ID}); err != nil {
					t.Fatal(err)
				}
			case "none_matched":
				if err := s.NoneMatchedProblem(token); err != nil {
					t.Fatal(err)
				}
			}
			facts := ReviewCandidateFacts{Version: ReviewProtocolVersion, Candidates: []matching.ScoredCandidate{{Candidate: provider.Candidate{Provider: "fixture", MatchRef: "late"}}}}
			if err := s.CompleteProblemQuery(token, facts); !errors.Is(err, ErrPlanStale) {
				t.Fatal("late query committed", err)
			}
			item, err := s.GetItem(a.ID)
			if err != nil || item.RawRow != a.RawRow {
				t.Fatal(item, err)
			}
			if change == "terminal" && item.Status != StatusDiscarded {
				t.Fatal("terminal restored", item)
			}
		})
	}
}

func TestProblemQueryPersistsSourceOnlyEnvelopeAndNoneMatched(t *testing.T) {
	s := openTestStore(t)
	a := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	b := enqueueProblemInput(t, s, "Song（译名：歌曲）", "Artist", "Album")
	for _, item := range []*ImportItem{a, b} {
		if err := s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
			t.Fatal(err)
		}
	}
	scope, _ := s.ItemProblemScope(a.ID)
	token, err := s.BeginProblemQuery(scope, ReviewQuery{Version: ReviewProtocolVersion, Title: "Manual", Provider: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	facts := ReviewCandidateFacts{Version: ReviewProtocolVersion, Candidates: []matching.ScoredCandidate{{Candidate: provider.Candidate{Provider: "fixture", MatchRef: "track"}, Score: matching.Score{Total: 87}}}, TriedVariants: []string{"Manual"}}
	if err := s.CompleteProblemQuery(token, facts); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := s.db.QueryRow("SELECT candidate_facts FROM review_problem WHERE id=?", scope.ProblemID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	var saved ProblemCandidateFacts
	if err := json.Unmarshal([]byte(body), &saved); err != nil || saved.Version != ReviewProtocolVersion || len(saved.Members) != 2 || saved.Members[0].Facts.Candidates[0].Candidate.MatchRef != "track" {
		t.Fatal(saved, err)
	}
	current, _ := s.ReadProblemScope(scope.ProblemID)
	if err := s.NoneMatchedProblem(current); err != nil {
		t.Fatal(err)
	}
	activeProblemCounts(t, s, 1, 2)
	for _, original := range []*ImportItem{a, b} {
		item, _ := s.GetItem(original.ID)
		facts, err := DecodeReviewCandidates(*item.Candidates)
		if err != nil || len(facts.Candidates) != 0 || item.RawRow != original.RawRow || item.Status != StatusReview {
			t.Fatal(item, facts, err)
		}
	}
}
func TestProblemRetryAndDiscardOnlyListedMembers(t *testing.T) {
	s := openTestStore(t)
	a := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	b := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	c := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	if err := s.MarkReview(a.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []*ImportItem{b, c} {
		if _, err := s.RecordFailure(item.ID, "failed", time.Second, 1); err != nil {
			t.Fatal(err)
		}
	}
	scope, _ := s.ItemProblemScope(a.ID)
	if _, err := s.RetryProblemMembers(scope, []int64{a.ID}); !errors.Is(err, ErrPlanStale) {
		t.Fatal("retried nonfailed member", err)
	}
	if _, err := s.RetryProblemMembers(scope, []int64{b.ID, b.ID}); !errors.Is(err, ErrPlanStale) {
		t.Fatal("duplicate scope", err)
	}
	batches, err := s.RetryProblemMembers(scope, []int64{b.ID})
	if err != nil || len(batches) != 1 || batches[0] != b.BatchID {
		t.Fatal(batches, err)
	}
	gotA, _ := s.GetItem(a.ID)
	gotC, _ := s.GetItem(c.ID)
	if gotA.Status != StatusReview || gotC.Status != StatusFailed {
		t.Fatal("retry propagated", gotA, gotC)
	}
	if err := s.DiscardProblemMembers(scope, []int64{c.ID}); !errors.Is(err, ErrPlanStale) {
		t.Fatal("old scope accepted", err)
	}
	current, _ := s.ItemProblemScope(a.ID)
	if err := s.DiscardProblemMembers(current, []int64{a.ID, c.ID}); err != nil {
		t.Fatal(err)
	}
	activeProblemCounts(t, s, 1, 1)
}
