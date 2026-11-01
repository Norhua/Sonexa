package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
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
