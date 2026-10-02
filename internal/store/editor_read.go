package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"strconv"
)

func (s *Store) ReadEditorSession(id int64) (*domain.EditorSession, error) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := readEditorSessionTx(tx, id)
	if err != nil {
		return nil, err
	}
	var lyricsID *int64
	if err := tx.QueryRow("SELECT lyrics_file_id FROM track_current WHERE track_id=?", id).Scan(&lyricsID); err != nil {
		return nil, err
	}
	if lyricsID != nil {
		text, err := s.readLyrics(*lyricsID)
		if err != nil {
			return nil, err
		}
		f := result.Current["track.lyrics"]
		f.State = domain.Value
		b, _ := json.Marshal(text)
		f.ValueJSON = string(b)
		result.Current["track.lyrics"] = f
	}
	// 已知上游歌词保存在不可变文件，不从当前用户歌词反推快照。
	var upstreamID *int64
	var state string
	err = tx.QueryRow("SELECT lyrics_file_id,lyrics_state FROM track_snapshot WHERE track_id=?", id).Scan(&upstreamID, &state)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if result.TrackIdentity != nil && upstreamID != nil {
		text, err := s.readLyrics(*upstreamID)
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(text)
		result.Upstream["track.lyrics"] = domain.EditValue{State: domain.Value, ValueJSON: string(b)}
	} else if result.TrackIdentity != nil && state == string(domain.Empty) {
		result.Upstream["track.lyrics"] = domain.EditValue{State: domain.Empty}
	}
	return result, nil
}

func readEditorSessionTx(tx *sql.Tx, id int64) (*domain.EditorSession, error) {
	result := &domain.EditorSession{TrackID: id, Current: domain.EditFields{}, Revisions: map[string]int64{}, ReleaseTrackIDs: []int64{}}
	var source, ref *string
	var releaseID *int64
	var revision int64
	if err := tx.QueryRow("SELECT match_source,match_ref,release_id,revision FROM track WHERE id=?", id).Scan(&source, &ref, &releaseID, &revision); err != nil {
		return nil, err
	}
	result.Revisions["track:"+strconv.FormatInt(id, 10)] = revision
	if source != nil && ref != nil {
		result.TrackIdentity = &domain.Identity{Provider: domain.ProviderID(*source), Ref: *ref}
	}
	if err := readEditCurrentTx(tx, "track", id, result.Current); err != nil {
		return nil, err
	}
	roles, err := readEditRolesTx(tx, id)
	if err != nil {
		return nil, err
	}
	origins, err := readOriginsTx(tx, "track", id)
	if err != nil {
		return nil, err
	}
	for _, role := range []string{"vocal", "lyricist", "composer"} {
		list := roles[role]
		if list == nil {
			list = []domain.Role{}
		}
		b, _ := json.Marshal(list)
		origin := origins["roles."+role]
		result.Current["track.roles."+role] = domain.EditValue{State: domain.Value, ValueJSON: string(b), Mode: origin.Mode, Source: origin.Source}
	}
	// 歌词值在文件锁保护下补入；来源状态与文件 ID 无关。
	origin := origins["lyricsFileId"]
	result.Current["track.lyrics"] = domain.EditValue{State: domain.Empty, Mode: origin.Mode, Source: origin.Source}
	snapshot := domain.Snapshot{}
	if result.TrackIdentity != nil {
		var body string
		revision = 0
		err := tx.QueryRow("SELECT body,revision FROM track_snapshot WHERE track_id=?", id).Scan(&body, &revision)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		result.Revisions["track_snapshot:"+strconv.FormatInt(id, 10)] = revision
		if body != "" {
			var t domain.TrackSnapshot
			if err := json.Unmarshal([]byte(body), &t); err != nil {
				return nil, err
			}
			if t.Identity == *result.TrackIdentity {
				snapshot.Track = &t
			}
		}
	}
	if releaseID != nil {
		result.ReleaseID = *releaseID
		var rp, rr *string
		if err := tx.QueryRow("SELECT provider,provider_ref,release_group_id,revision FROM release WHERE id=?", *releaseID).Scan(&rp, &rr, &result.GroupID, &revision); err != nil {
			return nil, err
		}
		result.Revisions["release:"+strconv.FormatInt(*releaseID, 10)] = revision
		if rp != nil && rr != nil {
			result.ReleaseIdentity = &domain.Identity{Provider: domain.ProviderID(*rp), Ref: *rr}
		}
		if err := readEditCurrentTx(tx, "release", *releaseID, result.Current); err != nil {
			return nil, err
		}
		if result.ReleaseIdentity != nil {
			var body string
			revision = 0
			err := tx.QueryRow("SELECT body,revision FROM release_snapshot WHERE release_id=?", *releaseID).Scan(&body, &revision)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
			result.Revisions["release_snapshot:"+strconv.FormatInt(*releaseID, 10)] = revision
			if body != "" {
				var r domain.ReleaseSnapshot
				if err := json.Unmarshal([]byte(body), &r); err != nil {
					return nil, err
				}
				if r.Identity == *result.ReleaseIdentity {
					snapshot.Release = &r
				}
			}
		}
		if err := tx.QueryRow("SELECT revision FROM release_group WHERE id=?", result.GroupID).Scan(&revision); err != nil {
			return nil, err
		}
		result.Revisions["release_group:"+strconv.FormatInt(result.GroupID, 10)] = revision
		rows, err := tx.Query("SELECT id FROM track WHERE release_id=? ORDER BY id", *releaseID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var member int64
			if err := rows.Scan(&member); err != nil {
				rows.Close()
				return nil, err
			}
			result.ReleaseTrackIDs = append(result.ReleaseTrackIDs, member)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		result.OtherReleaseTracks = len(result.ReleaseTrackIDs) - 1
		if err := tx.QueryRow("SELECT count(*) FROM track t JOIN release r ON r.id=t.release_id WHERE r.release_group_id=? AND r.id<>?", result.GroupID, *releaseID).Scan(&result.OtherGroupTracks); err != nil {
			return nil, err
		}
		var imageID *int64
		var cleared bool
		err = tx.QueryRow("SELECT revision,image_id,image_id IS NULL FROM release_cover_selection WHERE release_id=?", *releaseID).Scan(&result.CoverRevision, &imageID, &cleared)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		result.Revisions["cover_selection:"+strconv.FormatInt(*releaseID, 10)] = result.CoverRevision
		// 封面选择作为显式当前字段参与 dirty，预览 URL 仍按发行身份解析。
		b, _ := json.Marshal(map[string]any{"releaseId": *releaseID, "imageId": imageID, "cleared": cleared})
		result.Current["release.cover"] = domain.EditValue{State: domain.Value, ValueJSON: string(b)}
		if snapshot.Release != nil && snapshot.Release.Group != nil {
			group := snapshot.Release.Group
			var body string
			err := tx.QueryRow("SELECT body,revision FROM release_group_snapshot WHERE group_id=? AND provider=? AND group_ref=?", result.GroupID, group.Provider, group.Ref).Scan(&body, &revision)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
			if body != "" {
				var g domain.GroupSnapshot
				if err := json.Unmarshal([]byte(body), &g); err != nil {
					return nil, err
				}
				snapshot.Group = &g
				result.Revisions["group_snapshot:"+string(group.Provider)+":"+group.Ref] = revision
			}
		}
	}
	result.Upstream, err = domain.EditorFields(snapshot)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	result.SnapshotJSON = string(b)
	return result, nil
}

func readEditCurrentTx(tx *sql.Tx, kind string, id int64, out domain.EditFields) error {
	origins, err := readOriginsTx(tx, kind, id)
	if err != nil {
		return err
	}
	for field, column := range currentColumns[kind] {
		if field == "lyricsFileId" {
			continue
		}
		var raw any
		if err := tx.QueryRow("SELECT "+column+" FROM "+kind+"_current WHERE "+entityKey(kind)+"=?", id).Scan(&raw); err != nil {
			return err
		}
		f := domain.EditValue{State: domain.Empty, Mode: origins[field].Mode, Source: origins[field].Source}
		if raw != nil && raw != "" {
			f.State = domain.Value
			if field == "media" || field == "secondaryTypes" {
				f.ValueJSON = fmt.Sprint(raw)
			} else {
				b, err := json.Marshal(raw)
				if err != nil {
					return err
				}
				f.ValueJSON = string(b)
			}
		}
		out[kind+"."+field] = f
	}
	return nil
}
func readEditRolesTx(tx *sql.Tx, id int64) (map[string][]domain.Role, error) {
	rows, err := tx.Query(`SELECT ta.role,ta.position,ta.attributes_json,a.name,a.sort_name,a.type,a.provider,a.provider_ref FROM track_artist ta JOIN artist a ON a.id=ta.artist_id WHERE ta.track_id=? ORDER BY ta.role,ta.position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]domain.Role{}
	for rows.Next() {
		var r domain.Role
		var attrs string
		var sortName, kind, source, ref *string
		if err := rows.Scan(&r.Role, &r.Position, &attrs, &r.Artist.Name, &sortName, &kind, &source, &ref); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(attrs), &r.Attributes); err != nil {
			return nil, err
		}
		r.Artist.SortName = deref(sortName)
		r.Artist.Type = deref(kind)
		if source != nil && ref != nil {
			r.Artist.Identity = &domain.Identity{Provider: domain.ProviderID(*source), Ref: *ref}
		}
		out[r.Role] = append(out[r.Role], r)
	}
	return out, rows.Err()
}

// ReadEditorReleaseCurrent also supports a saved upstream release with no collection.
func (s *Store) ReadEditorReleaseCurrent(id int64) (domain.EditFields, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	fields := domain.EditFields{}
	if err := readEditCurrentTx(tx, "release", id, fields); err != nil {
		return nil, err
	}
	return fields, nil
}
