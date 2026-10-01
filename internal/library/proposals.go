package library

import (
	"context"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func (s *Service) ListGroupProposals() ([]store.GroupProposal, error) {
	return s.store.ListGroupProposals()
}
func (s *Service) DismissGroupProposal(id, revision int64) error {
	return s.store.DismissGroupProposal(id, revision)
}
func (s *Service) PrepareGroupProposal(ctx context.Context, id, targetGroup int64) (*domain.AdmissionPlan, error) {
	proposal, err := s.store.GetGroupProposal(id)
	if err != nil {
		return nil, err
	}
	if proposal.Status != "pending" {
		return nil, store.ErrPlanStale
	}
	// 使用当前绑定事实重新判组。旧提议仅用于解释，不充当永久别名。
	release, err := s.store.ReleaseSnapshot(proposal.ReleaseID)
	if err != nil {
		return nil, err
	}
	seed := domain.AdmissionSeed{Snapshot: domain.Snapshot{Release: release, Track: &domain.TrackSnapshot{}}, MoveReleaseID: proposal.ReleaseID, ProposalID: id, MergeTargetID: targetGroup}
	if proposal.Snapshot.Group != nil && release.Group != nil && proposal.Snapshot.Group.Identity == *release.Group {
		seed.Snapshot.Group = proposal.Snapshot.Group
	}
	return s.PrepareAdmission(ctx, seed)
}
