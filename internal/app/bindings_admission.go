package app

import (
	"errors"
	"fmt"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/library"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func (a *App) libraryService() (*library.Service, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.libraryServiceLocked()
}

// libraryServiceLocked 供持有 App 锁的设置更新与串行初始化复用同一服务。
func (a *App) libraryServiceLocked() (*library.Service, error) {
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
		if a.coverSvc != nil {
			a.library.SetFiles(a.coverSvc.Library())
		}
	}
	a.library.SetCandidateSources(a.identitySources)
	return a.library, nil
}
func (a *App) PrepareAdmission(row CandidateRow, itemID int64) (*AdmissionPlanView, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	ctx := provider.WithRequestGate(a.ctx, func() error {
		if gate := a.CheckOutbound(); !gate.Allowed {
			return fmt.Errorf("%s", gate.Reason)
		}
		return nil
	})
	plan, err := service.PrepareCandidate(ctx, row.toCandidate(), itemID)
	var unresolved *library.TargetResolutionError
	if errors.As(err, &unresolved) && len(unresolved.Resolution.Candidates) > 0 {
		scored := make([]matching.ScoredCandidate, 0, len(unresolved.Resolution.Candidates))
		for _, c := range unresolved.Resolution.Candidates {
			scored = append(scored, matching.ScoredCandidate{Candidate: c, Score: matching.Score{Total: row.Confidence}})
		}
		action := "接受"
		if itemID == 0 {
			action = "加入曲库"
		}
		rows := a.rowsFrom(scored, action)
		for i := range rows {
			position := ""
			if rows[i].DiscNumber != nil {
				position = fmt.Sprintf("第 %d 碟", *rows[i].DiscNumber)
			}
			if rows[i].TrackNumber != nil {
				if position != "" {
					position += " · "
				}
				position += fmt.Sprintf("第 %d 轨", *rows[i].TrackNumber)
			}
			if rows[i].Note != "" && position != "" {
				rows[i].Note += " · "
			}
			rows[i].Note += position
		}
		return &AdmissionPlanView{Resolution: &CandidateResolutionView{State: string(unresolved.Resolution.State), Reason: unresolved.Resolution.Reason, Candidates: rows}}, nil
	}
	return admissionView(plan, err)
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
		a.autoStartEnrich()
	}
	return result, err
}

func (a *App) ListGroupProposals() ([]GroupProposalView, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	proposals, err := service.ListGroupProposals()
	if err != nil {
		return nil, err
	}
	out := []GroupProposalView{}
	for _, p := range proposals {
		view, _ := admissionView(&domain.AdmissionPlan{Seed: domain.AdmissionSeed{Snapshot: p.Snapshot}}, nil)
		out = append(out, GroupProposalView{ID: p.ID, ReleaseID: p.ReleaseID, BaselineRevision: p.BaselineRevision, Status: p.Status, Resolution: p.Resolution, Snapshot: view.Seed.Snapshot})
	}
	return out, nil
}
func (a *App) PrepareGroupProposal(id, targetGroup int64) (*AdmissionPlanView, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return admissionView(service.PrepareGroupProposal(a.ctx, id, targetGroup))
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

// LocalAdmissionInput 的歌曲/专辑署名分别输入，不互相推断。
type LocalAdmissionInput struct {
	Title       *string `json:"title"`
	Artist      *string `json:"artist"`
	Album       *string `json:"album"`
	AlbumArtist *string `json:"albumArtist"`
	Year        *int    `json:"year"`
	DiscNumber  *int    `json:"discNumber"`
	TrackNumber *int    `json:"trackNumber"`
}

func (a *App) PrepareLocalAdmission(input LocalAdmissionInput) (*AdmissionPlanView, error) {
	return a.prepareLocalAdmission(input, 0)
}
func (a *App) prepareLocalAdmission(input LocalAdmissionInput, itemID int64) (*AdmissionPlanView, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	track := &domain.TrackSnapshot{Title: provider.FieldFromPointer(input.Title), Year: provider.FieldFromPointer(input.Year), DiscNumber: provider.FieldFromPointer(input.DiscNumber), TrackNumber: provider.FieldFromPointer(input.TrackNumber)}
	if input.Artist != nil {
		track.Credit = domain.Known(domain.Credit{Text: *input.Artist})
	}
	snapshot := domain.Snapshot{Track: track}
	if input.Album != nil || input.AlbumArtist != nil {
		snapshot.Release = &domain.ReleaseSnapshot{Title: provider.FieldFromPointer(input.Album)}
		if input.AlbumArtist != nil {
			snapshot.Release.Credit = domain.Known(domain.Credit{Text: *input.AlbumArtist})
		}
	}
	return admissionView(service.PrepareAdmission(a.ctx, domain.AdmissionSeed{Snapshot: snapshot, ImportItemID: itemID, ItemStatus: "imported"}))
}
func (a *App) PrepareNoMatchNeeded(itemID int64) (*AdmissionPlanView, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("条目不存在")
	}
	return a.prepareLocalAdmission(LocalAdmissionInput{Title: item.InTitle, Artist: item.InArtist, Album: item.InAlbum, Year: item.InYear, DiscNumber: item.InDiscNo, TrackNumber: item.InTrackNo}, itemID)
}
func (a *App) commitUnconfirmedAdmission(plan *AdmissionPlanView) (int64, error) {
	if plan == nil || plan.Resolution != nil {
		return 0, fmt.Errorf("目标存在多个位置或资料不足，请选择具体 track")
	}
	result, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{})
	if err != nil {
		return 0, err
	}
	return result.TrackID, nil
}

func (a *App) ReplanAdmissionGroup(id, hash string, target int64) (*AdmissionPlanView, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return admissionView(service.ReplanAdmissionGroup(a.ctx, id, hash, target))
}
