package musicbrainz

import (
	"context"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

func (p *Provider) Refs() provider.RefCodec { return refCodec{} }
func (p *Provider) SnapshotFromCandidate(c provider.Candidate) (*domain.Snapshot, error) {
	return provider.CandidateSnapshot(provider.MusicBrainz, c)
}
func (p *Provider) Complete(ctx context.Context, t domain.DetailTarget) (*domain.Snapshot, error) {
	return provider.CompleteUsing(ctx, p, t)
}
func (p *Provider) LoadReleaseSummary(ctx context.Context, r domain.ReleaseIdentity) (*domain.ReleaseSummary, error) {
	return provider.SummaryUsing(ctx, p, r)
}
