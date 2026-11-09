package store

import (
	"database/sql"
)

// SetImportInvalidationHandler is installed by the single App-owned Runner.
// It runs only after commit and must not hold a store transaction or do network I/O.
func (s *Store) SetImportInvalidationHandler(handler func()) {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	s.importInvalidated = handler
}
func (s *Store) notifyImportInvalidation() {
	s.planMu.RLock()
	handler := s.importInvalidated
	s.planMu.RUnlock()
	if handler != nil {
		handler()
	}
}
func (s *Store) HasScheduledImportWork(batchID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM import_item WHERE batch_id=? AND status IN ('queued','matching'))`, batchID).Scan(&exists)
	return exists, err
}

func (s *Store) ImportWorkRevision(itemID int64) (int64, bool, error) {
	var revision int64
	var active bool
	err := s.db.QueryRow(`SELECT revision,status='matching' AND EXISTS(SELECT 1 FROM import_item i JOIN import_destination d ON d.input_id=i.id
 WHERE (i.id=import_item.id OR i.merged_item_id=import_item.id) AND d.state='active') FROM import_item WHERE id=?`, itemID).Scan(&revision, &active)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return revision, active, err
}
