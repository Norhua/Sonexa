package library

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

type recheckContextKey struct{}
type recheckContext struct {
	revision   int64
	facts      []matching.CandidateFact
	incomplete bool
}

var errRecheckNeedsFacts = errors.New("重评需要更多来源资料")

func currentRecheck(ctx context.Context) *recheckContext {
	v, _ := ctx.Value(recheckContextKey{}).(*recheckContext)
	return v
}

// RecheckPending executes a bounded batch of current-evidence evaluations. No
// target/approval from an earlier operation is consulted; only current source
// candidates and local facts participate in the normal selection pipeline.
func (s *Service) RecheckPending(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		limit = 16
	}
	processed := 0
	for processed < limit {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		work, err := s.store.NextProblemRecheck(ctx)
		if err != nil {
			return processed, err
		}
		if work == nil {
			return processed, nil
		}
		err = s.recheckInput(ctx, *work)
		if errors.Is(err, store.ErrProblemBusy) || errors.Is(err, store.ErrPlanStale) {
			if queueErr := s.store.QueueProblemRecheck(work.ProblemID); queueErr != nil {
				return processed, queueErr
			}
			return processed, nil
		}
		if err := s.store.FinishProblemRecheck(*work, err); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}
func (s *Service) recheckInput(ctx context.Context, work store.ProblemRecheck) error {
	state := &recheckContext{revision: work.LibraryRevision}
	if work.Proof != "" {
		var input matching.ProblemInput
		if err := json.Unmarshal([]byte(work.Proof), &input); err != nil {
			return err
		}
		if input.Pool != nil {
			state.incomplete = input.Pool.Truncated
			for _, target := range input.Pool.Targets {
				for _, fact := range target.Facts {
					if fact.Kind == "remote" {
						state.facts = append(state.facts, fact)
					}
				}
				for _, issue := range target.Issues {
					if issue.Code == "decision_facts_unavailable" || issue.Code == "local_target_missing_from_source" {
						state.incomplete = true
					}
				}
			}
		}
	}
	ctx = context.WithValue(ctx, recheckContextKey{}, state)
	ctx = context.WithValue(ctx, automaticScopeKey{}, work.Scope)
	profile := store.ItemProfile(work.Item)
	facts, err := store.DecodeReviewCandidates(value(work.Item.Candidates))
	if err != nil {
		return err
	}
	var selection *Selection
	if len(facts.Candidates) == 0 && len(state.facts) == 0 && !state.incomplete {
		selection, err = s.PrepareLocalExisting(ctx, profile, work.Item.ID, work.Item.Revision)
	} else {
		selected, selectErr := s.SelectCandidate(ctx, facts.Candidates, work.Item.ID, &profile)
		selection, err = &selected, selectErr
	}
	if errors.Is(err, errRecheckNeedsFacts) {
		return nil
	}
	if err != nil {
		return err
	}
	if selection == nil || selection.Plan == nil || selection.Reason != "" {
		return nil
	}
	if selection.Plan.Seed.Operation != domain.AdmissionSkipExisting {
		return nil
	}
	if err := s.store.CheckInputScope(work.Scope); err != nil {
		return err
	}
	if err := s.store.CheckRecheckRevision(work.LibraryRevision); err != nil {
		return err
	}
	result, err := s.CommitAdmission(ctx, selection.Plan.ID, selection.Plan.Hash, domain.AdmissionApproval{})
	if err != nil {
		return err
	}
	if !result.Existing {
		return errors.New("重评不得新增收藏")
	}
	return nil
}

// Ensure future recheck callers don't turn an unowned target into auto-create.
func recheckOwned(ctx context.Context, ownership matching.LocalMatch) bool {
	return currentRecheck(ctx) == nil || ownership.State == matching.OwnershipOwned
}
