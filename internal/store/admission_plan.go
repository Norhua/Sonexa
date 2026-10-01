package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

var ErrPlanStale = errors.New("plan_stale: 计划已失效，请重新预览")
var ErrConfirmationRequired = errors.New("confirmation_required: 请确认具体发行成员变更")
var ErrGroupConflict = errors.New("group_conflict: 归组证据命中多个组，请选择明确合组方案")

// SetAdmissionVerifier 由用例层组装，不向前端暴露。
func (s *Store) SetAdmissionVerifier(verify func(domain.AdmissionPlan, domain.AdmissionApproval) bool) {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	s.admissionVerifier = verify
}
func (s *Store) validApproval(plan domain.AdmissionPlan, approval domain.AdmissionApproval) bool {
	s.planMu.RLock()
	verify := s.admissionVerifier
	s.planMu.RUnlock()
	return verify != nil && verify(plan, approval)
}

func AdmissionHash(plan domain.AdmissionPlan) (string, error) {
	plan.Hash = ""
	plan.Seal = ""
	raw, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func operationResultTx(tx *sql.Tx, id, hash string) (*domain.AdmissionResult, error) {
	var storedHash, body string
	err := tx.QueryRow("SELECT content_hash,result_json FROM operation_result WHERE plan_id=?", id).Scan(&storedHash, &body)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if storedHash != hash {
		return nil, ErrPlanStale
	}
	var result domain.AdmissionResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, err
	}
	return &result, nil
}
func (s *Store) AdmissionResult(id, hash string) (*domain.AdmissionResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return operationResultTx(tx, id, hash)
}

func (s *Store) CommitAdmission(plan domain.AdmissionPlan, approval domain.AdmissionApproval) (*domain.AdmissionResult, error) {
	hash, err := AdmissionHash(plan)
	if err != nil {
		return nil, err
	}
	if hash != plan.Hash || plan.ID == "" {
		return nil, ErrPlanStale
	}
	var result *domain.AdmissionResult
	err = s.withTx(func(tx *sql.Tx) error {
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
		baseline, err := admissionBaselineTx(tx, plan.Seed)
		if err != nil {
			return err
		}
		// 精确歌曲已经被另一入口写入，仍原子结束当前导入条目，且不再改变任何成员。
		if baseline.ExistingTrackID != 0 {
			var releaseID, groupID *int64
			if err := tx.QueryRow("SELECT t.release_id,r.release_group_id FROM track t LEFT JOIN release r ON r.id=t.release_id WHERE t.id=?", baseline.ExistingTrackID).Scan(&releaseID, &groupID); err != nil {
				return err
			}
			result = &domain.AdmissionResult{TrackID: baseline.ExistingTrackID, Existing: true}
			if releaseID != nil {
				result.ReleaseID = *releaseID
			}
			if groupID != nil {
				result.GroupID = *groupID
			}
			return finishAdmissionTx(tx, plan, *result)
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
		if plan.Seed.ImportItemID != 0 && ItemStatus(baseline.ItemState).IsTerminal() {
			return ErrPlanStale
		}
		result, err = insertAdmissionTx(tx, plan.Seed, baseline)
		if err != nil {
			return err
		}
		if baseline.RequiresConfirmation {
			actions, _ := json.Marshal(struct {
				Baseline domain.AdmissionBaseline `json:"baseline"`
				Result   domain.AdmissionResult   `json:"result"`
			}{baseline, *result})
			evidence, _ := json.Marshal(baseline.Resolution.Evidence)
			if _, err := tx.Exec("INSERT INTO release_membership_decision(plan_id,content_hash,actions_json,evidence_json,approved_at) VALUES(?,?,?,?,?)", plan.ID, plan.Hash, string(actions), string(evidence), time.Now().UTC()); err != nil {
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
func finishAdmissionTx(tx *sql.Tx, plan domain.AdmissionPlan, result domain.AdmissionResult) error {
	if plan.Seed.ProposalID != 0 {
		row, err := tx.Exec("UPDATE group_change_proposal SET status='accepted',updated_at=? WHERE id=? AND release_id=? AND status='pending'", time.Now().UTC(), plan.Seed.ProposalID, plan.Seed.MoveReleaseID)
		if err != nil {
			return err
		}
		if n, _ := row.RowsAffected(); n != 1 {
			return ErrPlanStale
		}
	}

	if plan.Seed.ImportItemID != 0 {
		status := plan.Seed.ItemStatus
		if status == "" {
			status = "imported"
		}
		if status != "imported" && status != "skipped" {
			return fmt.Errorf("无效导入终态")
		}
		row, err := tx.Exec("UPDATE import_item SET status=?,track_id=?,review_reason=NULL,next_retry_at=NULL,last_error=NULL,updated_at=? WHERE id=? AND status IN ('queued','matching','review','failed')", status, result.TrackID, time.Now().UTC(), plan.Seed.ImportItemID)
		if err != nil {
			return err
		}
		n, err := row.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrPlanStale
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO operation_result(plan_id,content_hash,result_json,committed_at) VALUES(?,?,?,?)", plan.ID, plan.Hash, string(raw), time.Now().UTC())
	return err
}

func insertAdmissionTx(tx *sql.Tx, seed domain.AdmissionSeed, baseline domain.AdmissionBaseline) (*domain.AdmissionResult, error) {
	snapshot := seed.Snapshot
	result := &domain.AdmissionResult{ReleaseID: baseline.ExistingReleaseID, GroupID: baseline.TargetGroupID}
	now := time.Now().UTC()
	if snapshot.Release != nil {
		r := snapshot.Release
		if result.GroupID == 0 {
			id, err := createGroupTx(tx, "", nil)
			if err != nil {
				return nil, err
			}
			result.GroupID = id
			// 第一成员初始化组摘要。后续成员不会覆盖它。
			fields, err := layerFields(r)
			if err != nil {
				return nil, err
			}
			if err := writeCurrentTx(tx, "release_group", id, fields, string(r.Identity.Provider), false, nil); err != nil {
				return nil, err
			}
			groupRef := ""
			if r.Group != nil {
				groupRef = r.Group.Ref
			}
			if snapshot.Group != nil {
				groupRef = snapshot.Group.Identity.Ref
			}
			if _, err := tx.Exec("UPDATE release_group SET display_provider=?,display_group_ref=? WHERE id=?", string(r.Identity.Provider), groupRef, id); err != nil {
				return nil, err
			}
		}
		if len(baseline.Resolution.Groups) > 1 {
			for _, group := range baseline.Resolution.Groups {
				if group.ID == result.GroupID {
					continue
				}
				for _, query := range []string{
					"UPDATE release SET release_group_id=?,revision=revision+1 WHERE release_group_id=?",
					"UPDATE release_group_ref SET group_id=? WHERE group_id=?",
					"UPDATE release_group_snapshot SET group_id=? WHERE group_id=?",
				} {
					if _, err := tx.Exec(query, result.GroupID, group.ID); err != nil {
						return nil, err
					}
				}
				if _, err := tx.Exec("DELETE FROM release_group WHERE id=?", group.ID); err != nil {
					return nil, err
				}
			}
		}
		native := r.Group
		if native == nil && snapshot.Group != nil && snapshot.Group.Identity.Ref != "" {
			native = &snapshot.Group.Identity
		}
		if native != nil && native.Ref != "" {
			if _, err := tx.Exec("INSERT INTO release_group_ref(group_id,provider,group_ref) VALUES(?,?,?) ON CONFLICT(provider,group_ref) DO NOTHING", result.GroupID, native.Provider, native.Ref); err != nil {
				return nil, err
			}
		}
		if snapshot.Group != nil && snapshot.Group.Identity.Ref != "" {
			if err := saveGroupSnapshotTx(tx, result.GroupID, snapshot.Group); err != nil {
				return nil, err
			}
		}
		if result.ReleaseID == 0 {
			source, ref := nonblank(string(r.Identity.Provider)), nonblank(r.Identity.Ref)
			row, err := tx.Exec("INSERT INTO release(release_group_id,provider,provider_ref,created_at,updated_at) VALUES(?,?,?,?,?)", result.GroupID, source, ref, now, now)
			if err != nil {
				return nil, err
			}
			id, err := row.LastInsertId()
			if err != nil {
				return nil, err
			}
			result.ReleaseID = id
			if err := emptyCurrentTx(tx, "release", id); err != nil {
				return nil, err
			}
		}
		if r.Identity.Ref != "" {
			if _, err := saveSnapshotTx(tx, "release", result.ReleaseID, r); err != nil {
				return nil, err
			}
		}
		fields, err := layerFields(r)
		if err != nil {
			return nil, err
		}
		if err := writeCurrentTx(tx, "release", result.ReleaseID, fields, string(r.Identity.Provider), false, nil); err != nil {
			return nil, err
		}
		if r.Tracklist.IsKnown() {
			var body string
			if err := tx.QueryRow("SELECT body FROM release_snapshot WHERE release_id=?", result.ReleaseID).Scan(&body); err != nil {
				return nil, err
			}
			var merged domain.ReleaseSnapshot
			if err := json.Unmarshal([]byte(body), &merged); err != nil {
				return nil, err
			}
			if err := syncSnapshotTracklistTx(tx, result.ReleaseID, merged); err != nil {
				return nil, err
			}
		}
		if err := refreshCreditsTx(tx, "release", result.ReleaseID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec("UPDATE release_group SET revision=revision+1,updated_at=? WHERE id=?", now, result.GroupID); err != nil {
			return nil, err
		}
	}
	if seed.MoveReleaseID != 0 {
		return result, nil
	}
	trackCopy := *snapshot.Track
	t := &trackCopy
	if !t.Year.IsKnown() && snapshot.Release != nil && snapshot.Release.Date.State == domain.Value {
		if year := (&Release{ReleaseDate: &snapshot.Release.Date.Value}).Year(); year != nil {
			t.Year = domain.Known(*year)
		}
	}
	source, ref := nonblank(string(t.Identity.Provider)), nonblank(t.Identity.Ref)
	state := EnrichSkipped
	if source != nil {
		state = EnrichAbsent
		if snapshot.Release != nil && snapshot.Release.Identity.Ref != "" {
			state = EnrichPending
		}
	}
	row, err := tx.Exec("INSERT INTO track(release_id,match_source,match_ref,matched_at,enrich_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", nullableID(result.ReleaseID), source, ref, now, string(state), now, now)
	if err != nil {
		return nil, err
	}
	id, err := row.LastInsertId()
	if err != nil {
		return nil, err
	}
	result.TrackID = id
	if err := emptyCurrentTx(tx, "track", id); err != nil {
		return nil, err
	}
	if t.Lyrics.IsKnown() {
		return nil, fmt.Errorf("歌词须经文件暂存事务提交")
	}
	if t.Identity.Ref != "" {
		if _, err := saveSnapshotTx(tx, "track", id, t); err != nil {
			return nil, err
		}
		if err := rebuildIdentifiersTx(tx, id, *t); err != nil {
			return nil, err
		}
	}
	fields, err := layerFields(t)
	if err != nil {
		return nil, err
	}
	if !t.Year.IsKnown() && snapshot.Release != nil {
		date := snapshot.Release.Date
		if date.State == domain.Value {
			fields["year"] = rawFromPointer((&Release{ReleaseDate: &date.Value}).Year())
		}
	}
	if err := writeCurrentTx(tx, "track", id, fields, string(t.Identity.Provider), false, nil); err != nil {
		return nil, err
	}
	if err := syncRolesTx(tx, id, t.Roles, string(t.Identity.Provider), false); err != nil {
		return nil, err
	}
	if err := refreshCreditsTx(tx, "track", id); err != nil {
		return nil, err
	}
	if err := syncFTS(tx, id); err != nil {
		return nil, err
	}
	if result.ReleaseID != 0 {
		if err := syncFTSForRelease(tx, result.ReleaseID); err != nil {
			return nil, err
		}
	}
	return result, nil
}
