package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
)

func recallForTest(t *testing.T, s *Store, inputs []matching.InputProfile, limit int) []matching.LocalRecallResult {
	t.Helper()
	out, err := s.RecallLocal(context.Background(), inputs, limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(inputs) {
		t.Fatalf("result count %d != input count %d", len(out), len(inputs))
	}
	return out
}
func requireRecallIDs(t *testing.T, result matching.LocalRecallResult, truncated bool, ids ...int64) {
	t.Helper()
	got := []int64{}
	for _, record := range result.Records {
		got = append(got, record.TrackID)
	}
	if len(got) != len(ids) || (len(ids) > 0 && !reflect.DeepEqual(got, ids)) || result.Truncated != truncated {
		t.Fatalf("recall IDs %v truncated=%v, want %v truncated=%v", got, result.Truncated, ids, truncated)
	}
}

func TestLocalRecallDirtyMarkersSurviveOuterUpserts(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	// The row is already dirty; nested snapshot/credit UPSERTs must not turn
	// repeated invalidation into a unique violation or suppress the fact write.
	applyTestSnapshot(t, s, id, snapshot)
	applyTestSnapshot(t, s, id, snapshot)
	for _, statement := range []string{
		`UPDATE OR ABORT track_current SET title='updated' WHERE track_id=?`,
		`INSERT INTO track_current(track_id,title) VALUES(?,'upserted') ON CONFLICT(track_id) DO UPDATE SET title=excluded.title`,
	} {
		if _, err := s.db.Exec(statement, id); err != nil {
			t.Fatal(err)
		}
	}
	var title string
	var count int
	if err := s.db.QueryRow("SELECT title FROM track_current WHERE track_id=?", id).Scan(&title); err != nil || title != "upserted" {
		t.Fatal(title, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM local_match_dirty WHERE track_id=?", id).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	requireRecallIDs(t, recallForTest(t, s, []matching.InputProfile{matching.NewInputProfile("upserted", "", "")}, 10)[0], false, id)
}

func TestLocalRecallKeepsCurrentAndBoundFactsSeparate(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	snapshot.Track.Release = &snapshot.Release.Identity
	snapshot.Track.Title = domain.Known("夕立（译名：骤雨）")
	snapshot.Track.DurationMS = domain.Known(620000)
	snapshot.Release.TracklistCompleteness = domain.Complete
	snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{{DiscNumber: 2, TrackNumber: 3,
		Track: domain.Known(snapshot.Track.Identity), Title: domain.Known("发行内的原名"), Credit: domain.Known(domain.Credit{Text: "清单署名"})}})
	applyTestSnapshot(t, s, id, snapshot)
	meta, err := s.GetMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyMetadataEdits(id, map[string]string{"title": "用户曲名", "credit": "用户署名"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateReleaseFields(*meta.ReleaseID, map[string]string{"title": "用户专辑"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	inputs := []matching.InputProfile{
		matching.NewInputProfile("骤雨", "", ""), matching.NewInputProfile("夕立", "", ""),
		matching.NewInputProfile("用户曲名", "", ""), matching.NewInputProfile("发行内的原名", "", ""),
		matching.NewInputProfile("", "", "Album"), matching.NewInputProfile("", "清单署名", ""),
	}
	for _, result := range recallForTest(t, s, inputs, 10) {
		requireRecallIDs(t, result, false, id)
		r := result.Records[0]
		if r.Current.Profile.Title.Raw != "用户曲名" || r.Current.Profile.Album.Raw != "用户专辑" || r.Current.Profile.Artist.Text.Raw != "用户署名" {
			t.Fatal("current values replaced by snapshot", r.Current)
		}
		if r.TrackSnapshot.Title.Value != "夕立（译名：骤雨）" || r.ReleaseSnapshot.Title.Value != "Album" || r.ReleaseSnapshot.TracklistCompleteness != domain.Complete || len(r.ReleaseSnapshot.Tracklist.Value) != 1 {
			t.Fatal("snapshot or list lost", r)
		}
		if r.Current.Profile.Duration.State != matching.ValueMissing || r.TrackSnapshot.DurationMS.Value != 620000 {
			t.Fatal("upstream duration spliced into current facts", r)
		}
		if r.TrackRevision == 0 || r.ReleaseRevision == 0 || r.TrackSnapshotRevision == 0 || r.ReleaseSnapshotRevision == 0 || r.Current.Origins["track.title"].Mode != domain.User {
			t.Fatal("fact revisions/origins lost", r)
		}
	}
	// The index retains qualifiers and unknown fragments, not just a stripped key.
	var profile string
	if err := s.db.QueryRow("SELECT profile_json FROM local_match_key WHERE track_id=? AND field='title' AND source='track_snapshot' LIMIT 1", id).Scan(&profile); err != nil || profile == "" {
		t.Fatal(profile, err)
	}
	var textProfile matching.TextProfile
	if err := json.Unmarshal([]byte(profile), &textProfile); err != nil || textProfile.Raw != "夕立（译名：骤雨）" || len(textProfile.Aliases) != 1 {
		t.Fatal("indexed profile lost original/alias", textProfile, err)
	}
	meta, err = s.GetMetadata(id)
	if err != nil || meta.Title != "用户曲名" || deref(meta.Album) != "用户专辑" {
		t.Fatal("recall wrote current fields", meta, err)
	}
}

func TestLocalRecallUnboundRelationsAndIncrementalInvalidation(t *testing.T) {
	s := openTestStore(t)
	id, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "Ｓｏｎｇ (A Remix)"})
	if err != nil {
		t.Fatal(err)
	}
	artist, err := s.UpsertArtist(&Artist{Name: "角色 A"})
	if err != nil {
		t.Fatal(err)
	}
	voice, err := s.UpsertArtist(&Artist{Name: "声优 X"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddTrackArtist(id, TrackArtist{ArtistID: artist, Role: RoleCharacter}); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkArtists(artist, voice, LinkVoiceOf); err != nil {
		t.Fatal(err)
	}
	inputs := []matching.InputProfile{matching.NewInputProfile("song", "", ""), matching.NewInputProfile("", "声优 X", "")}
	for _, result := range recallForTest(t, s, inputs, 10) {
		requireRecallIDs(t, result, false, id)
		r := result.Records[0]
		if r.TrackIdentity != nil || r.TrackSnapshot != nil || len(r.Current.Roles) != 1 || len(r.Current.ArtistLinks) != 1 || len(r.Current.Profile.Title.Qualifiers) != 1 {
			t.Fatal("unbound recall gained identity or lost qualifier/relations", r)
		}
	}
	if _, err := s.db.Exec("UPDATE artist SET name='声优新名' WHERE id=?", voice); err != nil {
		t.Fatal(err)
	}
	out := recallForTest(t, s, []matching.InputProfile{matching.NewInputProfile("", "声优 X", ""), matching.NewInputProfile("", "声优新名", "")}, 10)
	requireRecallIDs(t, out[0], false)
	requireRecallIDs(t, out[1], false, id)
	if _, err := s.db.Exec("DELETE FROM artist_link WHERE from_artist_id=?", artist); err != nil {
		t.Fatal(err)
	}
	requireRecallIDs(t, recallForTest(t, s, []matching.InputProfile{matching.NewInputProfile("", "声优新名", "")}, 10)[0], false)
	if err := s.ApplyMetadataEdits(id, map[string]string{"title": "新的曲名"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	out = recallForTest(t, s, []matching.InputProfile{matching.NewInputProfile("song", "", ""), matching.NewInputProfile("新的曲名", "", "")}, 10)
	requireRecallIDs(t, out[0], false)
	requireRecallIDs(t, out[1], false, id)
	if err := s.DeleteTrack(id); err != nil {
		t.Fatal(err)
	}
	requireRecallIDs(t, recallForTest(t, s, []matching.InputProfile{matching.NewInputProfile("新的曲名", "", "")}, 10)[0], false)
}

func TestLocalRecallRejectsStaleTrackAndReleaseSnapshots(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	snapshot.Track.Release = &snapshot.Release.Identity
	applyTestSnapshot(t, s, id, snapshot)
	if err := s.ApplyMetadataEdits(id, map[string]string{"title": "当前曲名", "credit": "当前署名"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	meta, err := s.GetMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateReleaseFields(*meta.ReleaseID, map[string]string{"title": "当前专辑"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	inputs := []matching.InputProfile{matching.NewInputProfile("Upstream Song", "", ""), matching.NewInputProfile("", "", "Album"), matching.NewInputProfile("当前曲名", "", "")}
	out := recallForTest(t, s, inputs, 10)
	requireRecallIDs(t, out[0], false, id)
	requireRecallIDs(t, out[1], false, id)
	if err := s.SetProvenance(id, "test", "other-track"); err != nil {
		t.Fatal(err)
	}
	out = recallForTest(t, s, inputs, 10)
	requireRecallIDs(t, out[0], false)
	requireRecallIDs(t, out[1], false, id)
	if out[2].Records[0].TrackSnapshot != nil || out[2].Records[0].TrackIdentity.Ref != "other-track" {
		t.Fatal("stale track snapshot used", out[2])
	}
	if _, err := s.db.Exec("UPDATE release SET provider_ref='other-release',revision=revision+1 WHERE id=?", *meta.ReleaseID); err != nil {
		t.Fatal(err)
	}
	out = recallForTest(t, s, inputs, 10)
	requireRecallIDs(t, out[1], false)
	requireRecallIDs(t, out[2], false, id)
	if out[2].Records[0].ReleaseSnapshot != nil {
		t.Fatal("stale release snapshot used", out[2])
	}
	// Restore only the track binding: its snapshot still asserts the old
	// release, so a matching track ID alone cannot make it valid again.
	if err := s.SetProvenance(id, "test", "track"); err != nil {
		t.Fatal(err)
	}
	out = recallForTest(t, s, inputs, 10)
	requireRecallIDs(t, out[0], false)
	if out[2].Records[0].TrackSnapshot != nil || len(out[2].Records[0].Issues) != 2 {
		t.Fatal("track/release relation conflict lost", out[2])
	}
}

func TestLocalRecallBeyondBrowseLimitAndExplicitTruncation(t *testing.T) {
	s := openTestStore(t)
	if err := s.withTx(func(tx *sql.Tx) error {
		for i := 1; i <= 2005; i++ {
			if _, err := tx.Exec("INSERT INTO track(id,created_at,updated_at) VALUES(?,datetime('now'),datetime('now'))", i); err != nil {
				return err
			}
			title := fmt.Sprintf("其它曲目 %d", i)
			if i >= 2003 {
				title = "目标发行原名 (Live at Tokyo)"
			}
			if _, err := tx.Exec("INSERT INTO track_current(track_id,title) VALUES(?,?)", i, title); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	input := matching.NewInputProfile("目标发行原名...", "", "")
	requireRecallIDs(t, recallForTest(t, s, []matching.InputProfile{input}, 2)[0], true, 2003, 2004)
	requireRecallIDs(t, recallForTest(t, s, []matching.InputProfile{input}, 3)[0], false, 2003, 2004, 2005)
	requireRecallIDs(t, recallForTest(t, s, []matching.InputProfile{matching.NewInputProfile("", "", "")}, 10)[0], false)
}

func TestLocalRecallRelationsMoveAcrossOwners(t *testing.T) {
	for _, table := range []string{"track_credit", "track_artist", "release_credit"} {
		t.Run(table, func(t *testing.T) {
			s := openTestStore(t)
			one, err := s.CreateTrack(&Track{Title: "one", Album: ptr("album one")})
			if err != nil {
				t.Fatal(err)
			}
			two, err := s.CreateTrack(&Track{Title: "two", Album: ptr("album two")})
			if err != nil {
				t.Fatal(err)
			}
			artist, err := s.UpsertArtist(&Artist{Name: "独立关系名字"})
			if err != nil {
				t.Fatal(err)
			}
			key, from, to := "track_id", one, two
			if table == "release_credit" {
				key = "release_id"
				for _, target := range []struct {
					track   int64
					release *int64
				}{{one, &from}, {two, &to}} {
					if err := s.db.QueryRow("SELECT release_id FROM track WHERE id=?", target.track).Scan(target.release); err != nil {
						t.Fatal(err)
					}
				}
			}
			columns, values := key+",artist_id", "?,?"
			if table == "track_artist" {
				columns += ",role"
				values += ",'vocal'"
			}
			if _, err := s.db.Exec("INSERT INTO "+table+"("+columns+") VALUES("+values+")", from, artist); err != nil {
				t.Fatal(err)
			}
			input := []matching.InputProfile{matching.NewInputProfile("", "独立关系名字", "")}
			requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false, one)
			if _, err := s.db.Exec("UPDATE "+table+" SET "+key+"=? WHERE "+key+"=? AND artist_id=?", to, from, artist); err != nil {
				t.Fatal(err)
			}
			requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false, two)
		})
	}
}

func TestLocalRecallLinkUpdateAndArtistCascade(t *testing.T) {
	s := openTestStore(t)
	one, err := s.CreateTrack(&Track{Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.CreateTrack(&Track{Title: "two"})
	if err != nil {
		t.Fatal(err)
	}
	var artists []int64
	for _, name := range []string{"成员一", "成员二", "团体名"} {
		id, err := s.UpsertArtist(&Artist{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		artists = append(artists, id)
	}
	for i, id := range []int64{one, two} {
		if err := s.AddTrackArtist(id, TrackArtist{ArtistID: artists[i], Role: RoleVocal}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.LinkArtists(artists[0], artists[2], LinkMemberOf); err != nil {
		t.Fatal(err)
	}
	input := []matching.InputProfile{matching.NewInputProfile("", "团体名", "")}
	requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false, one)
	if _, err := s.db.Exec("UPDATE artist_link SET from_artist_id=? WHERE from_artist_id=?", artists[1], artists[0]); err != nil {
		t.Fatal(err)
	}
	requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false, two)
	if _, err := s.db.Exec("DELETE FROM artist WHERE id=?", artists[2]); err != nil {
		t.Fatal(err)
	}
	requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false)
}

func TestLocalRecallRollbackPreservesFactsAndInvalidation(t *testing.T) {
	s := openTestStore(t)
	id, err := s.CreateTrack(&Track{Title: "旧名"})
	if err != nil {
		t.Fatal(err)
	}
	input := []matching.InputProfile{matching.NewInputProfile("旧名", "", ""), matching.NewInputProfile("新名", "", "")}
	requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false, id)
	s.beforeCommit = func(*sql.Tx) error { return errors.New("forced rollback") }
	if err := s.UpdateTrackFields(id, ptr("新名"), nil, true); err == nil {
		t.Fatal("write should roll back")
	}
	s.beforeCommit = nil
	out := recallForTest(t, s, input, 10)
	requireRecallIDs(t, out[0], false, id)
	requireRecallIDs(t, out[1], false)
	if err := s.UpdateTrackFields(id, ptr("新名"), nil, true); err != nil {
		t.Fatal(err)
	}
	// Abort midway through reindexing; old keys and dirty markers must survive
	// together, so the next read can retry instead of claiming an empty pool.
	if _, err := s.db.Exec(`CREATE TRIGGER reject_match_key BEFORE INSERT ON local_match_key BEGIN SELECT RAISE(ABORT,'forced index failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecallLocal(context.Background(), input, 10); err == nil {
		t.Fatal("index failure swallowed")
	}
	var key string
	var dirty int
	if err := s.db.QueryRow("SELECT name_key FROM local_match_key WHERE track_id=? AND field='title' LIMIT 1", id).Scan(&key); err != nil || key != matching.Normalize("旧名") {
		t.Fatal(key, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM local_match_dirty WHERE track_id=?", id).Scan(&dirty); err != nil || dirty != 1 {
		t.Fatal(dirty, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_match_key"); err != nil {
		t.Fatal(err)
	}
	out = recallForTest(t, s, input, 10)
	requireRecallIDs(t, out[0], false)
	requireRecallIDs(t, out[1], false, id)
}

func TestLocalRecallBatchKeysAndConcurrentSnapshots(t *testing.T) {
	s := openTestStore(t)
	id, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "唯一名字", Credit: ptr("并发召回"), MatchSource: ptr("test"), MatchRef: ptr("0")})
	if err != nil {
		t.Fatal(err)
	}
	profile := matching.NewInputProfile("不存在", "", "")
	for i := 0; i < 1200; i++ {
		profile.Title.Aliases = append(profile.Title.Aliases, matching.NameForm{Text: fmt.Sprintf("alias-%d", i)})
	}
	profile.Title.Aliases = append(profile.Title.Aliases, matching.NameForm{Text: "唯一名字"})
	requireRecallIDs(t, recallForTest(t, s, []matching.InputProfile{profile}, 10)[0], false, id)
	if _, err := s.RecallLocal(context.Background(), make([]matching.InputProfile, 129), 10); err == nil {
		t.Fatal("oversized batch accepted")
	}
	if out, err := s.RecallLocal(context.Background(), nil, 10); err != nil || len(out) != 0 {
		t.Fatal(out, err)
	}
	input := []matching.InputProfile{matching.NewInputProfile("", "并发召回", "")}
	var wg sync.WaitGroup
	errors := make(chan error, 4)
	start := make(chan struct{})
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(writer bool) {
			defer wg.Done()
			<-start
			for i := 0; i < 12; i++ {
				if writer {
					err := s.withTx(func(tx *sql.Tx) error {
						if _, err := tx.Exec("UPDATE track SET match_ref=?,revision=revision+1 WHERE id=?", fmt.Sprint(i), id); err != nil {
							return err
						}
						_, err := tx.Exec("UPDATE track_current SET title=? WHERE track_id=?", "版本"+fmt.Sprint(i), id)
						return err
					})
					if err != nil {
						errors <- err
						return
					}
				} else {
					out, err := s.RecallLocal(context.Background(), input, 10)
					if err != nil {
						errors <- err
						return
					}
					if len(out[0].Records) != 1 || out[0].Truncated {
						errors <- fmt.Errorf("lost concurrent record: %+v", out)
						return
					}
					r := out[0].Records[0]
					if r.Current.Profile.Title.Raw != "唯一名字" && r.Current.Profile.Title.Raw != "版本"+r.TrackIdentity.Ref {
						errors <- fmt.Errorf("mixed transaction facts: %+v", r)
						return
					}
				}
			}
		}(worker == 0)
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestLocalRecallRebuildVersionRestartAndCancelledRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recall.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	id, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "持久曲名"})
	if err != nil {
		t.Fatal(err)
	}
	input := []matching.InputProfile{matching.NewInputProfile("持久曲名", "", "")}
	requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false, id)
	if _, err := s.db.Exec("DELETE FROM local_match_key; UPDATE local_match_state SET rule_version=0"); err != nil {
		t.Fatal(err)
	}
	requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false, id)
	if err := s.RebuildLocalMatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false, id)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	requireRecallIDs(t, recallForTest(t, s, input, 10)[0], false, id)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.RecallLocal(ctx, input, 10); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation became an empty result", err)
	}
	if _, err := s.db.Exec("ALTER TABLE local_match_key RENAME TO unavailable_match_keys"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecallLocal(context.Background(), input, 10); err == nil {
		t.Fatal("query failure became an empty result")
	}
}
