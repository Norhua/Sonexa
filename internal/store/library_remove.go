package store

import (
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) RemoveFromLibrary(ids []int64) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	err := s.withTx(func(tx *sql.Tx) error {
		// Validate the whole selection before touching any relationship.
		for _, id := range ids {
			var member bool
			if err := tx.QueryRow(`SELECT in_library FROM track WHERE id=?`, id).Scan(&member); err != nil {
				if err == sql.ErrNoRows {
					return ErrPlanStale
				}
				return err
			}
			if !member {
				return fmt.Errorf("曲目不在音乐库中，不能删除同步专属曲目")
			}
		}
		seen := map[int64]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, err := tx.Exec(`DELETE FROM collection_item WHERE track_id=?`, id); err != nil {
				return err
			}
			var referenced bool
			if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM synced_playlist_entry WHERE track_id=? AND local_state='imported')`, id).Scan(&referenced); err != nil {
				return err
			}
			if referenced {
				if err := setLibraryMembershipTx(tx, id, false, time.Now().UTC()); err != nil {
					return err
				}
			} else if err := deleteTrackTx(tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		_ = s.collectLyrics()
	}
	return err
}

func (s *Store) DeleteSyncedPlaylist(id int64) error {
	s.fileMu.Lock()
	err := s.withTx(func(tx *sql.Tx) error {
		state, err := readPlaylistStateTx(tx, id)
		if err != nil {
			return err
		}
		if state == nil {
			return ErrPlanStale
		}
		tracks := map[int64]bool{}
		for _, entry := range state.Entries {
			if err := detachSyncedEntryDemandsTx(tx, entry.ID, "binding_deleted"); err != nil {
				return err
			}
			if entry.TrackID != nil {
				tracks[*entry.TrackID] = true
			}
		}
		if _, err := tx.Exec(`DELETE FROM synced_playlist WHERE id=?`, id); err != nil {
			return err
		}
		for trackID := range tracks {
			var collect bool
			if err := tx.QueryRow(`SELECT in_library=0 AND NOT EXISTS(SELECT 1 FROM synced_playlist_entry WHERE track_id=track.id AND local_state='imported') FROM track WHERE id=?`, trackID).Scan(&collect); err != nil {
				return err
			}
			if collect {
				if err := deleteTrackTx(tx, trackID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err == nil {
		_ = s.collectLyrics()
	}
	s.fileMu.Unlock()
	if err == nil {
		s.notifyImportInvalidation()
	}
	return err
}
