package app

import (
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func (a *App) PreviewEditCandidate(row CandidateRow) (*domain.EditPreview, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return service.PreviewEditCandidate(row.toCandidate())
}
func (a *App) SearchEditCandidates(trackID int64, source, title, artist, album, region string) ([]CandidateRow, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if _, err := a.store.TrackRevision(trackID); err != nil {
		return nil, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil, fmt.Errorf("%s", gate.Reason)
	}
	p, err := a.registry.Get(provider.ProviderID(source))
	if err != nil {
		return nil, err
	}
	if err := p.IsConfigured(); err != nil {
		return nil, err
	}
	done := a.beginInteractive()
	defer done()
	ctx := provider.WithPriority(context.Background(), provider.PriorityInteractive)
	scored, _, err := a.matchUpstream(ctx, p, matching.Input{Title: title, Artist: artist, Album: album, Country: region})
	if err != nil {
		return nil, err
	}
	out := make([]CandidateRow, 0, len(scored))
	for _, candidate := range scored {
		out = append(out, rowFrom(candidate))
	}
	return out, nil
}
func (a *App) LoadEditUpstream(source, releaseInput, trackInput, region string, force bool) (*domain.EditPreview, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil, fmt.Errorf("%s", gate.Reason)
	}
	done := a.beginInteractive()
	defer done()
	return service.LoadEditUpstream(provider.WithPriority(context.Background(), provider.PriorityInteractive), source, releaseInput, trackInput, region, force)
}

func (a *App) PreviewEditSave(draft domain.EditDraft) (*domain.EditImpact, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return service.PreviewEditSave(a.ctx, draft)
}

func (a *App) PlanAlbumChange(draft domain.EditDraft) (*domain.AlbumChangePlan, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return service.PlanAlbumChange(a.ctx, draft)
}
