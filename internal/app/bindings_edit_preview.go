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
	registry := a.metadataRegistry()
	p, err := registry.Get(provider.ProviderID(source))
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

func (a *App) PlanEditSave(draft domain.EditDraft) (*AdmissionPlanView, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return admissionView(service.PlanEditSave(a.ctx, draft))
}
func (a *App) SaveEdit(id, hash string, approval domain.AdmissionApproval) (*domain.AdmissionResult, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	result, err := service.SaveEdit(a.ctx, id, hash, approval)
	if err == nil {
		a.notifyLibraryChanged()
	}
	return result, err
}

func (a *App) PreviewCurrentEditUpstream(trackID int64) (*domain.EditPreview, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return service.PreviewCurrentEditUpstream(trackID)
}

type EditVersion struct {
	Ref        string `json:"ref"`
	Title      string `json:"title"`
	Credit     string `json:"credit"`
	Country    string `json:"country"`
	TrackTotal int    `json:"trackTotal"`
}
type EditVersions struct {
	Items []EditVersion `json:"items"`
	Total *int          `json:"total"`
}

func (a *App) ListEditVersions(source, groupRef string, offset int) (*EditVersions, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil, fmt.Errorf("%s", gate.Reason)
	}
	registry := a.metadataRegistry()
	p, err := registry.Get(provider.ProviderID(source))
	if err != nil {
		return nil, err
	}
	if err := p.IsConfigured(); err != nil {
		return nil, err
	}
	if offset < 0 {
		return nil, fmt.Errorf("页码无效")
	}
	done := a.beginInteractive()
	defer done()
	page, err := registry.ListVersions(provider.WithPriority(a.ctx, provider.PriorityInteractive), domain.Identity{Provider: domain.ProviderID(source), Ref: groupRef}, provider.Page{Offset: offset, Limit: 25})
	if err != nil {
		return nil, err
	}
	out := &EditVersions{Items: []EditVersion{}, Total: page.Total}
	for _, r := range page.Releases {
		out.Items = append(out.Items, EditVersion{Ref: r.Identity.Ref, Title: r.Title.Value, Credit: r.Credit.Value.Text, Country: r.Country.Value, TrackTotal: r.ReleaseTrackTotal.Value})
	}
	return out, nil
}
