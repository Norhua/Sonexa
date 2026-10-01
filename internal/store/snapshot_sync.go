package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

var ErrRevisionConflict = errors.New("数据已变更，请重新预览")
var ErrSnapshotTarget = errors.New("快照与当前精确身份不一致")

func (s *Store) TrackRevision(id int64) (int64, error) {
	var revision int64
	err := s.db.QueryRow("SELECT revision FROM track WHERE id=?", id).Scan(&revision)
	return revision, err
}

func (s *Store) ApplySnapshot(id int64, snapshot *domain.Snapshot, revision int64) error {
	if snapshot == nil || snapshot.Track == nil {
		return ErrSnapshotTarget
	}
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	copySnapshot := *snapshot
	copyTrack := *snapshot.Track
	copySnapshot.Track = &copyTrack
	lyrics := copyTrack.Lyrics
	copyTrack.Lyrics = domain.Missing[string]()
	var draft *LyricsDraft
	var prepared preparedLyrics
	if lyrics.State == domain.Value {
		staged, err := s.StageLyrics(context.Background(), lyrics.Value)
		if err != nil {
			return err
		}
		draft = &staged
		defer s.DiscardLyrics(staged)
		prepared, err = s.prepareLyrics(context.Background(), id, staged)
		if err != nil {
			return err
		}
	}
	err := s.withTx(func(tx *sql.Tx) error {
		if err := applySnapshotTx(tx, id, &copySnapshot, revision); err != nil {
			return err
		}
		if lyrics.IsKnown() {
			field := rawField{State: domain.Empty}
			var fileID *int64
			if draft != nil {
				v, err := registerLyricsTx(tx, prepared, string(copyTrack.Identity.Provider))
				if err != nil {
					return err
				}
				fileID = &v
				field = rawKnown(v)
			}
			if _, err := tx.Exec("UPDATE track_snapshot SET lyrics_file_id=?,lyrics_state=? WHERE track_id=?", fileID, lyrics.State, id); err != nil {
				return err
			}
			if err := writeCurrentTx(tx, "track", id, fieldMap{"lyricsFileId": field}, string(copyTrack.Identity.Provider), false, nil); err != nil {
				return err
			}
		}
		return nil
	})
	_ = s.collectLyrics()
	return err
}

// applySnapshotTx 同时更新事实、跟随值、角色、署名、清单、索引和修订；不改变任何归组。
func applySnapshotTx(tx *sql.Tx, id int64, snapshot *domain.Snapshot, revision int64) error {
	if snapshot == nil || snapshot.Track == nil {
		return ErrSnapshotTarget
	}
	var source, ref *string
	var releaseID *int64
	var actual int64
	if err := tx.QueryRow("SELECT match_source,match_ref,release_id,revision FROM track WHERE id=?", id).Scan(&source, &ref, &releaseID, &actual); err != nil {
		return err
	}
	if actual != revision {
		return ErrRevisionConflict
	}
	t := snapshot.Track
	if source == nil || ref == nil || string(t.Identity.Provider) != *source || t.Identity.Ref != *ref {
		return ErrSnapshotTarget
	}
	if t.Release != nil {
		if releaseID == nil {
			return ErrSnapshotTarget
		}
		var rp, rr *string
		if err := tx.QueryRow("SELECT provider,provider_ref FROM release WHERE id=?", *releaseID).Scan(&rp, &rr); err != nil {
			return err
		}
		if rp == nil || rr == nil || *rp != string(t.Release.Provider) || *rr != t.Release.Ref {
			return ErrSnapshotTarget
		}
	}
	if snapshot.Release != nil {
		if releaseID == nil {
			return ErrSnapshotTarget
		}
		r := snapshot.Release
		var rp, rr *string
		if err := tx.QueryRow("SELECT provider,provider_ref FROM release WHERE id=?", *releaseID).Scan(&rp, &rr); err != nil {
			return err
		}
		if rp == nil || rr == nil || *rp != string(r.Identity.Provider) || *rr != r.Identity.Ref {
			return ErrSnapshotTarget
		}
		if t.Release != nil && *t.Release != r.Identity {
			return ErrSnapshotTarget
		}
		if snapshot.Group != nil {
			var groupID int64
			err := tx.QueryRow(`SELECT gr.group_id FROM release_group_ref gr JOIN release r ON r.release_group_id=gr.group_id WHERE r.id=? AND gr.provider=? AND gr.group_ref=?`, *releaseID, snapshot.Group.Identity.Provider, snapshot.Group.Identity.Ref).Scan(&groupID)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if err == nil {
				if err := saveGroupSnapshotTx(tx, groupID, snapshot.Group); err != nil {
					return err
				}
			}
		}
		merged, err := saveSnapshotTx(tx, "release", *releaseID, r)
		if err != nil {
			return err
		}
		fields, err := layerFields(json.RawMessage(merged))
		if err != nil {
			return err
		}
		if err := writeCurrentTx(tx, "release", *releaseID, fields, *rp, false, nil); err != nil {
			return err
		}
		var saved domain.ReleaseSnapshot
		if err := json.Unmarshal(merged, &saved); err != nil {
			return err
		}
		if r.Tracklist.IsKnown() {
			if err := syncSnapshotTracklistTx(tx, *releaseID, saved); err != nil {
				return err
			}
		}
		if err := refreshCreditsTx(tx, "release", *releaseID); err != nil {
			return err
		}
		if r.Date.IsKnown() {
			if err := syncReleaseYearsTx(tx, *releaseID, r.Date, *rp); err != nil {
				return err
			}
		}
		if err := syncFTSForRelease(tx, *releaseID); err != nil {
			return err
		}
	}
	merged, err := saveSnapshotTx(tx, "track", id, t)
	if err != nil {
		return err
	}
	fields, err := layerFields(t)
	if err != nil {
		return err
	}
	if err := writeCurrentTx(tx, "track", id, fields, *source, false, nil); err != nil {
		return err
	}
	var saved domain.TrackSnapshot
	if err := json.Unmarshal(merged, &saved); err != nil {
		return err
	}
	if err := rebuildIdentifiersTx(tx, id, saved); err != nil {
		return err
	}
	if err := syncRolesTx(tx, id, t.Roles, *source, false); err != nil {
		return err
	}
	if err := refreshCreditsTx(tx, "track", id); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE track SET enrich_state='fetched',enrich_attempts=0,enrich_error=NULL,revision=revision+1,updated_at=? WHERE id=?", time.Now().UTC(), id); err != nil {
		return err
	}
	return syncFTS(tx, id)
}

func syncSnapshotTracklistTx(tx *sql.Tx, id int64, r domain.ReleaseSnapshot) error {
	{
		if _, err := tx.Exec("DELETE FROM release_track WHERE release_id=?", id); err != nil {
			return err
		}
	}
	for _, entry := range r.Tracklist.Value {
		if entry.DiscNumber <= 0 || entry.TrackNumber <= 0 {
			return fmt.Errorf("上游清单位置无效")
		}
		var track, recording, credit *string
		if entry.Track.State == domain.Value {
			track = nonblank(entry.Track.Value.Ref)
		}
		if entry.Recording.State == domain.Value {
			recording = nonblank(entry.Recording.Value.Ref)
		}
		if entry.Credit.State == domain.Value {
			credit = &entry.Credit.Value.Text
		}
		var duration *int
		if entry.DurationMS.State == domain.Value {
			duration = &entry.DurationMS.Value
		}
		if _, err := tx.Exec(`INSERT INTO release_track(release_id,disc_number,track_number,track_ref,recording_ref,title,track_credit,duration_ms) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(release_id,disc_number,track_number) DO UPDATE SET track_ref=excluded.track_ref,recording_ref=excluded.recording_ref,title=excluded.title,track_credit=excluded.track_credit,duration_ms=excluded.duration_ms`, id, entry.DiscNumber, entry.TrackNumber, track, recording, nonblank(entry.Title.Value), credit, duration); err != nil {
			return err
		}
	}
	state := string(r.TracklistCompleteness)
	if state == "" || state == string(domain.Unloaded) {
		state = string(TracklistPartial)
	}
	_, err := tx.Exec("UPDATE release SET tracklist_state=?,fetched_at=?,revision=revision+1 WHERE id=?", state, time.Now().UTC(), id)
	return err
}

func syncReleaseYearsTx(tx *sql.Tx, releaseID int64, date domain.Field[string], source string) error {
	field := rawField{State: domain.Unknown}
	if date.State == domain.Empty {
		field.State = domain.Empty
	} else if year := (&Release{ReleaseDate: &date.Value}).Year(); year != nil {
		field = rawKnown(*year)
	}
	if field.State == domain.Unknown {
		return nil
	}
	rows, err := tx.Query("SELECT id FROM track WHERE release_id=?", releaseID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		var body string
		err := tx.QueryRow("SELECT s.body FROM track_snapshot s JOIN track t ON t.id=s.track_id WHERE t.id=? AND t.match_source=?", id, source).Scan(&body)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			var snapshot domain.TrackSnapshot
			if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
				return err
			}
			raw, _ := json.Marshal(field)
			if err := json.Unmarshal(raw, &snapshot.Year); err != nil {
				return err
			}
			if _, err := saveSnapshotTx(tx, "track", id, snapshot); err != nil {
				return err
			}
		}
		if err := writeCurrentTx(tx, "track", id, fieldMap{"year": field}, source, false, nil); err != nil {
			return err
		}
	}
	return nil
}
