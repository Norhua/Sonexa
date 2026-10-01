package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

type GroupProposal struct {
	ID               int64                  `json:"id"`
	ReleaseID        int64                  `json:"releaseId"`
	BaselineRevision int64                  `json:"baselineRevision"`
	Status           string                 `json:"status"`
	Resolution       domain.GroupResolution `json:"resolution"`
	Snapshot         domain.Snapshot        `json:"snapshot"`
}
type proposalBody struct {
	Resolution domain.GroupResolution `json:"resolution"`
	Snapshot   domain.Snapshot        `json:"snapshot"`
}

func proposeGroupChangeTx(tx *sql.Tx, releaseID int64, group *domain.GroupSnapshot) error {
	var currentGroup, revision int64
	var body string
	if err := tx.QueryRow("SELECT r.release_group_id,r.revision,s.body FROM release r JOIN release_snapshot s ON s.release_id=r.id WHERE r.id=?", releaseID).Scan(&currentGroup, &revision, &body); err != nil {
		return err
	}
	var release domain.ReleaseSnapshot
	if err := json.Unmarshal([]byte(body), &release); err != nil {
		return err
	}
	snapshot := domain.Snapshot{Release: &release, Group: group}
	resolution, err := resolveGroupTx(tx, snapshot)
	if err != nil {
		return err
	}
	needed := false
	for _, g := range resolution.Groups {
		if g.ID != currentGroup {
			needed = true
		}
	}
	native := release.Group
	if native == nil && group != nil && group.Identity.Ref != "" {
		native = &group.Identity
	}
	if native != nil && native.Ref != "" {
		var count int
		if err := tx.QueryRow("SELECT count(*) FROM release_group_ref WHERE group_id=? AND provider=? AND group_ref=?", currentGroup, native.Provider, native.Ref).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			needed = true
		}
	}
	if !needed {
		return nil
	}
	fingerprint, err := json.Marshal(struct {
		Evidence []domain.GroupEvidence `json:"evidence"`
		Native   *domain.Identity       `json:"native"`
	}{resolution.Evidence, native})
	if err != nil {
		return err
	}
	sum := sha256.Sum256(fingerprint)
	hash := hex.EncodeToString(sum[:])
	var count int
	if err := tx.QueryRow("SELECT count(*) FROM group_change_proposal WHERE release_id=? AND evidence_hash=?", releaseID, hash).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	data, err := json.Marshal(proposalBody{resolution, snapshot})
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO group_change_proposal(release_id,evidence_json,evidence_hash,baseline_revision,created_at,updated_at) VALUES(?,?,?,?,?,?)", releaseID, string(data), hash, revision, time.Now().UTC(), time.Now().UTC())
	return err
}
func (s *Store) ListGroupProposals() ([]GroupProposal, error) {
	rows, err := s.db.Query("SELECT id,release_id,evidence_json,baseline_revision,status FROM group_change_proposal WHERE status='pending' ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GroupProposal{}
	for rows.Next() {
		p, err := scanGroupProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}
func scanGroupProposal(row interface{ Scan(...any) error }) (*GroupProposal, error) {
	p := &GroupProposal{}
	var raw string
	if err := row.Scan(&p.ID, &p.ReleaseID, &raw, &p.BaselineRevision, &p.Status); err != nil {
		return nil, err
	}
	var body proposalBody
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return nil, err
	}
	p.Resolution, p.Snapshot = body.Resolution, body.Snapshot
	return p, nil
}
func (s *Store) GetGroupProposal(id int64) (*GroupProposal, error) {
	return scanGroupProposal(s.db.QueryRow("SELECT id,release_id,evidence_json,baseline_revision,status FROM group_change_proposal WHERE id=?", id))
}
func (s *Store) DismissGroupProposal(id, revision int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		result, err := tx.Exec("UPDATE group_change_proposal SET status='dismissed',updated_at=? WHERE id=? AND baseline_revision=? AND status='pending'", time.Now().UTC(), id, revision)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return ErrPlanStale
		}
		return nil
	})
}
func (s *Store) ReleaseSnapshot(id int64) (*domain.ReleaseSnapshot, error) {
	var raw string
	if err := s.db.QueryRow("SELECT body FROM release_snapshot WHERE release_id=?", id).Scan(&raw); err != nil {
		return nil, err
	}
	var snapshot domain.ReleaseSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}
