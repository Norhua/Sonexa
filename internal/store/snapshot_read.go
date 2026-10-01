package store

import (
	"database/sql"
	"encoding/json"

	"github.com/norhua/sonexa/internal/domain"
)

// GetReleaseSnapshot 只返回与当前绑定精确相符的快照，离线读取不会补全或创建实体。
func (s *Store) GetReleaseSnapshot(id int64) (*domain.ReleaseSnapshot, error) {
	var body string
	var source, ref *string
	err := s.db.QueryRow(`SELECT s.body,r.provider,r.provider_ref FROM release r JOIN release_snapshot s ON s.release_id=r.id WHERE r.id=?`, id).Scan(&body, &source, &ref)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var snapshot domain.ReleaseSnapshot
	if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
		return nil, err
	}
	if source == nil || ref == nil || snapshot.Identity != (domain.Identity{Provider: domain.ProviderID(*source), Ref: *ref}) {
		return nil, nil
	}
	return &snapshot, nil
}
