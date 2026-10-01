package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/norhua/sonexa/internal/domain"
)

// editLegacyFields 供切换期间的绑定委托使用，仍经统一字段状态和投影规则。
func (s *Store) editLegacyFields(kind string, id int64, edits map[string]string, provenance map[string]string, locked map[string]bool) error {
	return s.withTx(func(tx *sql.Tx) error {
		var body string
		err := tx.QueryRow("SELECT body FROM "+kind+"_snapshot WHERE "+entityKey(kind)+"=?", id).Scan(&body)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		snapshot, err := layerFields(json.RawMessage(body))
		if body != "" && err != nil {
			return err
		}
		for key, value := range edits {
			if _, ok := currentColumns[kind][key]; !ok {
				continue
			}
			f := rawKnown(value)
			if value == "" {
				f = rawField{State: domain.Empty}
			} else if key == "year" || key == "discNumber" || key == "trackNumber" || key == "discTotal" {
				n, err := strconv.Atoi(value)
				if err != nil || n <= 0 {
					return fmt.Errorf("%s 必须为正整数或留空", key)
				}
				f = rawKnown(n)
			}
			if err := writeCurrentTx(tx, kind, id, fieldMap{key: f}, provenance[key], true, snapshot); err != nil {
				return err
			}
		}
		if kind == "track" {
			return syncFTS(tx, id)
		}
		return syncFTSForRelease(tx, id)
	})
}
