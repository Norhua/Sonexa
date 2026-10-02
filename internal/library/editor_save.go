package library

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func (s *Service) PlanEditSave(ctx context.Context, draft domain.EditDraft) (*domain.AdmissionPlan, error) {
	impact, err := s.PreviewEditSave(ctx, draft)
	if err != nil {
		return nil, err
	}
	content := domain.EditSaveContent{Draft: draft, Impact: impact, Revisions: map[string]int64{}}
	mergeRevisions := func(revisions map[string]int64) error {
		for k, v := range revisions {
			if old, ok := content.Revisions[k]; ok && old != v {
				return store.ErrPlanStale
			}
			content.Revisions[k] = v
		}
		return nil
	}
	if err := mergeRevisions(impact.Source.Revisions); err != nil {
		return nil, err
	}
	if err := mergeRevisions(impact.TargetRevisions); err != nil {
		return nil, err
	}
	if impact.SharedChange {
		content.Album, err = s.PlanAlbumChange(ctx, draft)
		if err != nil {
			return nil, err
		}
		if !content.Album.CanSubmit {
			return nil, fmt.Errorf("请至少明确选择一首迁入歌曲，并解决全部冲突")
		}
		for _, row := range content.Album.Rows {
			if err := mergeRevisions(row.Revisions); err != nil {
				return nil, err
			}
		}
	}
	content.Target, err = s.editTargetSnapshot(draft, impact)
	if err != nil {
		return nil, err
	}
	for _, id := range impact.TargetTrackIDs {
		session, err := s.store.ReadEditorSession(id)
		if err != nil {
			return nil, err
		}
		if err := mergeRevisions(session.Revisions); err != nil {
			return nil, err
		}
	}
	if impact.CreatesRelease {
		// Preallocate a JS-safe random identity without creating an entity; immutable cover files
		// can then be prepared before the SQL transaction. Collision is rejected at commit.
		var bytes [8]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return nil, err
		}
		content.NewReleaseID = int64(binary.BigEndian.Uint64(bytes[:])&((1<<48)-1)) + 1
	}
	if impact.CreatesRelease && !impact.LinksNewUpstream && (draft.CoverAction == "" || draft.CoverAction == "keep") && impact.Source.ReleaseID != 0 {
		s.editMu.Lock()
		files := s.files
		s.editMu.Unlock()
		if files == nil {
			selection, err := s.store.GetCoverSelection(impact.Source.ReleaseID)
			if err != nil {
				return nil, err
			}
			if selection != nil {
				if !selection.Cleared {
					return nil, fmt.Errorf("封面文件服务不可用，无法保留原采用封面")
				}
				content.Draft.CoverAction = "clear"
			}
		}
		if files != nil {
			image, cleared, err := files.StageReleaseArtwork(impact.Source.ReleaseID)
			if err != nil {
				return nil, err
			}
			if image != nil {
				content.Draft.CoverAction = "replace"
				content.Draft.CoverToken = image.Token
			}
			if cleared {
				content.Draft.CoverAction = "clear"
			}
		}
	}
	seed := editAdmissionSeed(draft, impact, content.Target)
	seed.Snapshot.Track = &domain.TrackSnapshot{}
	if !impact.SharedChange {
		seed.Snapshot = domain.Snapshot{Track: &domain.TrackSnapshot{}}
		seed.CurrentRelease = nil
	}
	body, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	seed.EditJSON = string(body)
	return s.PrepareAdmission(ctx, seed)
}

func (s *Service) SaveEdit(ctx context.Context, id, hash string, approval domain.AdmissionApproval) (*domain.AdmissionResult, error) {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	prior, err := s.store.AdmissionResult(id, hash)
	if err != nil || prior != nil {
		return prior, err
	}
	plan, err := s.plan(id, hash)
	if err != nil {
		return nil, err
	}
	if plan.Seed.EditJSON == "" {
		return nil, store.ErrPlanStale
	}
	var content domain.EditSaveContent
	if err := json.Unmarshal([]byte(plan.Seed.EditJSON), &content); err != nil {
		return nil, err
	}
	var result *domain.AdmissionResult
	commit := func(file *store.CoverFile) error {
		var err error
		result, err = s.store.CommitEdit(ctx, plan, approval, file)
		return err
	}
	if content.Draft.CoverAction == "replace" && !content.Impact.PreservesTargetValues {
		if s.files == nil {
			return nil, fmt.Errorf("封面文件服务不可用")
		}
		releaseID := content.Impact.TargetReleaseID
		if content.Impact.CreatesRelease {
			releaseID = content.NewReleaseID
		}
		err = s.files.WithPreparedArtwork(content.Draft.CoverToken, releaseID, func(file store.CoverFile) error { return commit(&file) })
	} else {
		err = commit(nil)
	}
	return result, err
}
