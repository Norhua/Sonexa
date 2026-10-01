package app

import (
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/library"
	"github.com/norhua/sonexa/internal/store"
)

func (a *App) libraryService() (*library.Service, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.library == nil {
		var err error
		var files *covers.LibraryImages
		if a.coverSvc != nil {
			files = a.coverSvc.Library()
		}
		a.library, err = library.New(a.registry, a.store, files, nil)
		if err != nil {
			return nil, err
		}
	} else {
		a.library.SetRegistry(a.registry)
	}
	return a.library, nil
}
func (a *App) PrepareAdmission(row CandidateRow, itemID int64) (*domain.AdmissionPlan, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return service.PrepareCandidate(a.ctx, row.toCandidate(), itemID)
}
func (a *App) ConfirmReleaseAdditions(planID, hash string) (*domain.AdmissionApproval, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return service.ConfirmReleaseAdditions(planID, hash)
}
func (a *App) CommitAdmission(planID, hash string, approval domain.AdmissionApproval) (*domain.AdmissionResult, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	result, err := service.CommitAdmission(a.ctx, planID, hash, approval)
	if err == nil {
		a.notifyLibraryChanged()
	}
	return result, err
}

func (a *App) ListGroupProposals() ([]store.GroupProposal, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return service.ListGroupProposals()
}
func (a *App) PrepareGroupProposal(id, targetGroup int64) (*domain.AdmissionPlan, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return service.PrepareGroupProposal(a.ctx, id, targetGroup)
}
func (a *App) DismissGroupProposal(id, revision int64) error {
	service, err := a.libraryService()
	if err != nil {
		return err
	}
	err = service.DismissGroupProposal(id, revision)
	if err == nil {
		a.notifyLibraryChanged()
	}
	return err
}
