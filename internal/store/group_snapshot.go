package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

// saveGroupSnapshotTx 不授予组身份映射；调用前必须已经确认该映射属于本地组。
func saveGroupSnapshotTx(tx *sql.Tx, id int64, snapshot *domain.GroupSnapshot) error {
	var mapped int64
	if err := tx.QueryRow("SELECT group_id FROM release_group_ref WHERE provider=? AND group_ref=?", snapshot.Identity.Provider, snapshot.Identity.Ref).Scan(&mapped); err != nil {
		return err
	}
	if mapped != id {
		return ErrSnapshotTarget
	}
	var old string
	if err := tx.QueryRow("SELECT body FROM release_group_snapshot WHERE provider=? AND group_ref=?", snapshot.Identity.Provider, snapshot.Identity.Ref).Scan(&old); err != nil && err != sql.ErrNoRows {
		return err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	merged, err := mergeSnapshotJSON([]byte(old), raw)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`INSERT INTO release_group_snapshot(group_id,provider,group_ref,body,fetched_at) VALUES(?,?,?,?,?) ON CONFLICT(provider,group_ref) DO UPDATE SET body=excluded.body,fetched_at=excluded.fetched_at,revision=revision+1`, id, snapshot.Identity.Provider, snapshot.Identity.Ref, string(merged), now); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE release_group SET display_provider=COALESCE(display_provider,?),display_group_ref=COALESCE(display_group_ref,?),revision=revision+1,updated_at=? WHERE id=?", snapshot.Identity.Provider, snapshot.Identity.Ref, now, id); err != nil {
		return err
	}
	var displayProvider, displayRef string
	if err := tx.QueryRow("SELECT display_provider,display_group_ref FROM release_group WHERE id=?", id).Scan(&displayProvider, &displayRef); err != nil {
		return err
	}
	if displayProvider != string(snapshot.Identity.Provider) || displayRef != snapshot.Identity.Ref {
		return nil
	}
	fields, err := layerFields(snapshot)
	if err != nil {
		return err
	}
	return writeCurrentTx(tx, "release_group", id, fields, displayProvider, false, nil)
}
