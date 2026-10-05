package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"reflect"
	"strings"
	"time"
)

type editWrite struct {
	id       int64
	fields   domain.EditFields
	snapshot *domain.TrackSnapshot
	rebind   bool
}
type editLyrics struct {
	current, upstream           *preparedLyrics
	currentState, upstreamState domain.FieldState
}

func (s *Store) CommitEdit(ctx context.Context, plan domain.AdmissionPlan, approval domain.AdmissionApproval, cover *CoverFile) (*domain.AdmissionResult, error) {
	hash, err := AdmissionHash(plan)
	if err != nil {
		return nil, err
	}
	if hash != plan.Hash || plan.Seed.EditJSON == "" || !s.validApproval(plan, domain.AdmissionApproval{}) {
		return nil, ErrPlanStale
	}
	var content domain.EditSaveContent
	if err := json.Unmarshal([]byte(plan.Seed.EditJSON), &content); err != nil {
		return nil, err
	}
	if content.Impact == nil {
		return nil, ErrPlanStale
	}
	migrates := content.Impact.SharedChange && !content.Impact.UpdatesRelease
	writes := []editWrite{}
	if migrates {
		if content.Album == nil || !content.Album.CanSubmit || len(content.Album.SelectedTrackIDs) == 0 {
			return nil, ErrPlanStale
		}
		for _, row := range content.Album.Rows {
			if row.Stay {
				continue
			}
			if row.Conflict != "" {
				return nil, ErrPlanStale
			}
			write := editWrite{id: row.TrackID, fields: row.After, rebind: true}
			if row.TargetSnapshotJSON != "" {
				var snapshot domain.TrackSnapshot
				if err := json.Unmarshal([]byte(row.TargetSnapshotJSON), &snapshot); err != nil {
					return nil, err
				}
				write.snapshot = &snapshot
			}
			writes = append(writes, write)
		}
	} else {
		write := editWrite{id: content.Draft.TrackID, fields: content.Draft.Fields}
		switch content.Draft.BindingAction {
		case "set":
			write.rebind = true
			write.snapshot = content.Target.Track
			// Retained fields must also be classified against the new effective binding.
			write.fields = domain.EditFields{}
			for k, v := range content.Impact.Source.Current {
				write.fields[k] = v
			}
			for k, v := range content.Draft.Fields {
				write.fields[k] = v
			}
		case "clear_track", "clear_release":
			write.rebind = content.Impact.DetachesTrack
		}
		writes = append(writes, write)
	}
	if len(writes) == 0 {
		return nil, ErrPlanStale
	}
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	lyrics := map[int64]editLyrics{}
	drafts := []LyricsDraft{}
	defer func() {
		for _, draft := range drafts {
			_ = s.DiscardLyrics(draft)
		}
		_ = s.collectLyrics()
	}()
	prepare := func(id int64, text string) (*preparedLyrics, error) {
		draft, err := s.StageLyrics(ctx, text)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
		file, err := s.prepareLyrics(ctx, id, draft)
		return &file, err
	}
	for _, write := range writes {
		item := editLyrics{}
		if field, ok := write.fields["track.lyrics"]; ok {
			item.currentState = field.State
			if field.State == domain.Value {
				var text string
				if err := json.Unmarshal([]byte(field.ValueJSON), &text); err != nil {
					return nil, err
				}
				item.current, err = prepare(write.id, text)
				if err != nil {
					return nil, err
				}
			}
		}
		if write.snapshot != nil {
			item.upstreamState = write.snapshot.Lyrics.State
			if write.snapshot.Lyrics.State == domain.Value {
				item.upstream, err = prepare(write.id, write.snapshot.Lyrics.Value)
				if err != nil {
					return nil, err
				}
			}
		}
		lyrics[write.id] = item
	}
	var result *domain.AdmissionResult
	err = s.withTx(func(tx *sql.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		prior, err := operationResultTx(tx, plan.ID, plan.Hash)
		if err != nil {
			return err
		}
		if prior != nil {
			result = prior
			return nil
		}
		if !s.validApproval(plan, domain.AdmissionApproval{}) {
			return ErrPlanStale
		}
		for key, want := range content.Revisions {
			if strings.HasPrefix(key, "group_snapshot:") {
				parts := strings.SplitN(key, ":", 3)
				var got int64
				err := tx.QueryRow("SELECT revision FROM release_group_snapshot WHERE provider=? AND group_ref=?", parts[1], parts[2]).Scan(&got)
				if err != nil && err != sql.ErrNoRows {
					return err
				}
				if got != want {
					return ErrPlanStale
				}
				continue
			}
			parts := strings.Split(key, ":")
			if len(parts) != 2 {
				return ErrPlanStale
			}
			table := parts[0]
			column := "id"
			switch table {
			case "track", "release", "release_group":
			case "track_snapshot":
				column = "track_id"
			case "release_snapshot":
				column = "release_id"
			case "cover_selection":
				table = "release_cover_selection"
				column = "release_id"
			default:
				return ErrPlanStale
			}
			var got int64
			err := tx.QueryRow("SELECT revision FROM "+table+" WHERE "+column+"=?", parts[1]).Scan(&got)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if got != want {
				return ErrPlanStale
			}
		}
		source, err := readEditorSessionTx(tx, content.Draft.TrackID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(source.ReleaseTrackIDs, content.Impact.Source.ReleaseTrackIDs) {
			return ErrPlanStale
		}
		baseline, err := admissionBaselineTx(tx, plan.Seed)
		if err != nil {
			return err
		}
		if !sameAdmissionBaseline(plan.Baseline, baseline) {
			return ErrPlanStale
		}
		if baseline.Conflict {
			return ErrGroupConflict
		}
		if baseline.RequiresConfirmation && (approval.Credential == "" || !s.validApproval(plan, approval)) {
			return ErrConfirmationRequired
		}
		result = &domain.AdmissionResult{TrackID: content.Draft.TrackID, ReleaseID: source.ReleaseID, GroupID: source.GroupID}
		if migrates {
			if content.Impact.CreatesRelease {
				var exists int
				if err := tx.QueryRow("SELECT count(*) FROM release WHERE id=?", content.NewReleaseID).Scan(&exists); err != nil {
					return err
				}
				if exists != 0 {
					return ErrPlanStale
				}
				result, err = insertAdmissionEntitiesTx(tx, plan.Seed, baseline, false, content.NewReleaseID)
				if err != nil {
					return err
				}
				result.TrackID = content.Draft.TrackID
				fields := domain.EditFields{}
				for _, row := range content.Album.Rows {
					if !row.Stay {
						fields = row.After
						break
					}
				}
				if err := writeEditorFieldsTx(tx, "release", result.ReleaseID, fields); err != nil {
					return err
				}
			} else {
				if baseline.RequiresConfirmation {
					result, err = insertAdmissionEntitiesTx(tx, plan.Seed, baseline, false, 0)
					if err != nil {
						return err
					}
					result.TrackID = content.Draft.TrackID
				} else {
					result.ReleaseID = content.Impact.TargetReleaseID
					if err := tx.QueryRow("SELECT release_group_id FROM release WHERE id=?", result.ReleaseID).Scan(&result.GroupID); err != nil {
						return err
					}
				}
			}
		}
		if content.Impact.UpdatesRelease {
			if result.ReleaseID == 0 || result.ReleaseID != content.Impact.TargetReleaseID || content.Draft.AlbumAction != "update" {
				return ErrPlanStale
			}
			if content.Draft.BindingAction == "set" && content.Target.Release != nil {
				if source.ReleaseIdentity == nil || *source.ReleaseIdentity != content.Target.Release.Identity {
					return ErrSnapshotTarget
				}
				body, err := saveSnapshotTx(tx, "release", result.ReleaseID, content.Target.Release)
				if err != nil {
					return err
				}
				if content.Target.Release.Tracklist.IsKnown() {
					var snapshot domain.ReleaseSnapshot
					if err := json.Unmarshal(body, &snapshot); err != nil {
						return err
					}
					if err := syncSnapshotTracklistTx(tx, result.ReleaseID, snapshot); err != nil {
						return err
					}
				}
			}
			if err := writeEditorFieldsTx(tx, "release", result.ReleaseID, content.Draft.Fields); err != nil {
				return err
			}
			if err := syncFTSForRelease(tx, result.ReleaseID); err != nil {
				return err
			}
		}
		// Recheck complete positions and identities even for callers bypassing the UI.
		seen := map[string]bool{}
		for _, write := range writes {
			if migrates {
				var releaseID *int64
				if err := tx.QueryRow("SELECT release_id FROM track WHERE id=?", write.id).Scan(&releaseID); err != nil {
					return err
				}
				if nullableIDValue(releaseID) != source.ReleaseID {
					return ErrPlanStale
				}
				disc, track := 0, 0
				for key, value := range map[string]*int{"track.discNumber": &disc, "track.trackNumber": &track} {
					field := write.fields[key]
					if field.State == domain.Value {
						if json.Unmarshal([]byte(field.ValueJSON), value) != nil || *value <= 0 {
							return fmt.Errorf("位置必须是正整数或未知")
						}
					}
				}
				if disc > 0 && track > 0 {
					key := fmt.Sprintf("%d:%d", disc, track)
					if seen[key] {
						return fmt.Errorf("迁入位置重复")
					}
					seen[key] = true
					var occupied int
					if err := tx.QueryRow("SELECT count(*) FROM track t JOIN track_current c ON c.track_id=t.id WHERE t.release_id=? AND c.disc_number=? AND c.track_number=?", result.ReleaseID, disc, track).Scan(&occupied); err != nil {
						return err
					}
					if occupied > 0 {
						return fmt.Errorf("目标位置已收藏")
					}
				}
			}
			if write.snapshot != nil {
				var id int64
				err := tx.QueryRow("SELECT id FROM track WHERE match_source=? AND match_ref=?", write.snapshot.Identity.Provider, write.snapshot.Identity.Ref).Scan(&id)
				if err != nil && err != sql.ErrNoRows {
					return err
				}
				if id != 0 && id != write.id {
					return fmt.Errorf("目标歌曲身份已收藏")
				}
			}
		}
		for _, write := range writes {
			clearBinding := write.rebind
			if write.snapshot != nil {
				var provider, ref *string
				if err := tx.QueryRow("SELECT match_source,match_ref FROM track WHERE id=?", write.id).Scan(&provider, &ref); err != nil {
					return err
				}
				clearBinding = provider == nil || ref == nil || *provider != string(write.snapshot.Identity.Provider) || *ref != write.snapshot.Identity.Ref
				if write.snapshot.Release != nil {
					var rp, rr *string
					if err := tx.QueryRow("SELECT provider,provider_ref FROM release WHERE id=?", result.ReleaseID).Scan(&rp, &rr); err != nil {
						return err
					}
					if rp == nil || rr == nil || *rp != string(write.snapshot.Release.Provider) || *rr != write.snapshot.Release.Ref {
						return ErrSnapshotTarget
					}
				}
			}
			if clearBinding {
				if err := clearTrackBindingTx(tx, write.id); err != nil {
					return err
				}
				if _, err := tx.Exec("DELETE FROM track_artist WHERE track_id=? AND role NOT IN ('vocal','composer','lyricist')", write.id); err != nil {
					return err
				}
				origins, err := readOriginsTx(tx, "track", write.id)
				if err != nil {
					return err
				}
				for key := range origins {
					if strings.HasPrefix(key, "roles.") && key != "roles.vocal" && key != "roles.composer" && key != "roles.lyricist" {
						delete(origins, key)
					}
				}
				body, err := json.Marshal(origins)
				if err != nil {
					return err
				}
				if _, err := tx.Exec("UPDATE track_current SET states_json=? WHERE track_id=?", string(body), write.id); err != nil {
					return err
				}
			}
			if migrates {
				if _, err := tx.Exec("UPDATE track SET release_id=?,revision=revision+1,updated_at=? WHERE id=?", result.ReleaseID, time.Now().UTC(), write.id); err != nil {
					return err
				}
			}
			if write.snapshot != nil {
				snapshot := *write.snapshot
				snapshot.Lyrics = domain.Missing[string]()
				if _, err := tx.Exec("UPDATE track SET match_source=?,match_ref=?,matched_at=?,enrich_state='pending',revision=revision+1 WHERE id=?", snapshot.Identity.Provider, snapshot.Identity.Ref, time.Now().UTC(), write.id); err != nil {
					return err
				}
				if _, err := saveSnapshotTx(tx, "track", write.id, &snapshot); err != nil {
					return err
				}
				if err := rebuildIdentifiersTx(tx, write.id, snapshot); err != nil {
					return err
				}
				if err := syncRolesTx(tx, write.id, snapshot.Roles, string(snapshot.Identity.Provider), false); err != nil {
					return err
				}
			}
			item := lyrics[write.id]
			if item.upstreamState == domain.Value || item.upstreamState == domain.Empty {
				var fileID *int64
				if item.upstream != nil {
					id, err := registerLyricsTx(tx, *item.upstream, "upstream")
					if err != nil {
						return err
					}
					fileID = &id
				}
				if _, err := tx.Exec("UPDATE track_snapshot SET lyrics_file_id=?,lyrics_state=? WHERE track_id=?", fileID, item.upstreamState, write.id); err != nil {
					return err
				}
			}
			if err := writeEditorFieldsTx(tx, "track", write.id, write.fields); err != nil {
				return err
			}
			if item.currentState == domain.Value || item.currentState == domain.Empty {
				field := rawField{State: domain.Empty}
				if item.current != nil {
					id, err := registerLyricsTx(tx, *item.current, "user")
					if err != nil {
						return err
					}
					field = rawKnown(id)
				}
				if err := writeCurrentTx(tx, "track", write.id, fieldMap{"lyricsFileId": field}, "", true, nil); err != nil {
					return err
				}
			}
			if err := syncFTS(tx, write.id); err != nil {
				return err
			}
		}
		if content.Impact.SharedChange && !content.Impact.PreservesTargetValues {
			if cover != nil {
				if cover.ReleaseID == nil || *cover.ReleaseID != result.ReleaseID {
					return ErrPlanStale
				}
				expectedRevision := int64(0)
				if content.Impact.UpdatesRelease {
					expectedRevision = source.CoverRevision
				}
				if _, err := adoptCoverFileTx(tx, *cover, expectedRevision); err != nil {
					return err
				}
			}
			if content.Draft.CoverAction == "clear" {
				if _, err := tx.Exec(`INSERT INTO release_cover_selection(release_id,image_id,variant_id,revision,adopted_at) VALUES(?,NULL,NULL,1,?)
					ON CONFLICT(release_id) DO UPDATE SET image_id=NULL,variant_id=NULL,revision=release_cover_selection.revision+1,adopted_at=excluded.adopted_at`, result.ReleaseID, time.Now().UTC()); err != nil {
					return err
				}
				if _, err := tx.Exec("UPDATE release SET revision=revision+1,updated_at=? WHERE id=?", time.Now().UTC(), result.ReleaseID); err != nil {
					return err
				}
			}
		}
		if baseline.RequiresConfirmation {
			actions, _ := json.Marshal(content.Album)
			evidence, _ := json.Marshal(baseline.Resolution.Evidence)
			if _, err := tx.Exec("INSERT INTO release_membership_decision(plan_id,content_hash,actions_json,evidence_json,approved_at) VALUES(?,?,?,?,?)", plan.ID, plan.Hash, string(actions), string(evidence), time.Now().UTC()); err != nil {
				return err
			}
		}
		if migrates && source.ReleaseID != result.ReleaseID {
			if err := deleteOrphanReleaseTx(tx, source.ReleaseID); err != nil {
				return err
			}
		}
		if err := rebuildGroupKeysTx(tx); err != nil {
			return err
		}
		return finishAdmissionTx(tx, plan, *result)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func nullableIDValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
func writeEditorFieldsTx(tx *sql.Tx, kind string, id int64, values domain.EditFields) error {
	fields := fieldMap{}
	roles := map[string]domain.Field[[]domain.Role]{}
	for key, value := range values {
		if !strings.HasPrefix(key, kind+".") {
			continue
		}
		key = strings.TrimPrefix(key, kind+".")
		if strings.HasPrefix(key, "roles.") {
			role := strings.TrimPrefix(key, "roles.")
			if role != "vocal" && role != "composer" && role != "lyricist" {
				continue
			}
			field := domain.Field[[]domain.Role]{State: value.State}
			if value.State == domain.Value {
				if err := json.Unmarshal([]byte(value.ValueJSON), &field.Value); err != nil {
					return err
				}
			}
			roles[role] = field
			continue
		}
		if key == "lyrics" || key == "cover" {
			continue
		}
		field := rawField{State: value.State, Value: json.RawMessage(value.ValueJSON)}
		fields[key] = field
	}
	if err := writeCurrentTx(tx, kind, id, fields, "", true, nil); err != nil {
		return err
	}
	if kind == "track" {
		return syncRolesTx(tx, id, roles, "", true)
	}
	return nil
}
