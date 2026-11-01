package app

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// Until the multi-source UI is wired, legacy per-row research must never
// silently broaden to all members. A multi-row problem requires explicit split.
func (a *App) singleReviewProblemScope(itemID int64) (store.ProblemScope, error) {
	scope, err := a.store.ItemProblemScope(itemID)
	if err != nil {
		return scope, err
	}
	if len(scope.Members) != 1 {
		return scope, fmt.Errorf("该问题涉及多行，请先拆出来源行或使用明确问题范围")
	}
	return scope, nil
}

func (a *App) GetReviewProblemScope(problemID int64) (store.ProblemScope, error) {
	if err := a.ready(); err != nil {
		return store.ProblemScope{}, err
	}
	return a.store.ReadProblemScope(problemID)
}
func (a *App) ResearchReviewProblem(scope store.ProblemScope, query store.ReviewQuery) ([]CandidateRow, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil, fmt.Errorf("%s", gate.Reason)
	}
	p, err := a.metadataRegistry().Get(provider.ProviderID(query.Provider))
	if err != nil {
		return nil, err
	}
	if err := p.IsConfigured(); err != nil {
		return nil, err
	}
	token, err := a.store.BeginProblemQuery(scope, query)
	if err != nil {
		return nil, err
	}
	done := a.beginInteractive()
	defer done()
	ctx := provider.WithPriority(context.Background(), provider.PriorityInteractive)
	scored, variants, err := a.matchUpstream(ctx, p, matching.Input{Title: query.Title, Artist: query.Artist, Album: query.Album, Country: query.Country})
	if err != nil {
		return nil, err
	}
	if err := a.store.CompleteProblemQuery(token, store.ReviewCandidateFacts{Version: store.ReviewProtocolVersion, Candidates: scored, TriedVariants: variants}); err != nil {
		return nil, err
	}
	return a.rowsFrom(scored, matching.CandidateAccept), nil
}
func (a *App) NoneMatchedProblem(scope store.ProblemScope) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.NoneMatchedProblem(scope)
}
func (a *App) DiscardReviewProblemMembers(scope store.ProblemScope, ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.DiscardProblemMembers(scope, ids)
}
func (a *App) RetryReviewProblemMembers(scope store.ProblemScope, ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	batches, err := a.store.RetryProblemMembers(scope, ids)
	if err != nil {
		return err
	}
	for _, batch := range batches {
		if a.runner != nil && !a.runner.IsRunning(batch) {
			if err := a.runner.Start(context.Background(), batch); err != nil {
				return err
			}
		}
	}
	return nil
}
func (a *App) SplitReviewProblemItem(scope store.ProblemScope, itemID int64) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	for _, member := range scope.Members {
		if member.ItemID == itemID {
			return a.store.SplitProblemItem(scope.ProblemID, scope.Revision, itemID, member.InputRevision)
		}
	}
	return 0, store.ErrPlanStale
}
