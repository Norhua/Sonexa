package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
)

func TestCurrentValuesCanReturnToFollow(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	applyTestSnapshot(t, s, id, snapshot)
	title := "User"
	if err := s.UpdateTrackFields(id, &title, nil, true); err != nil {
		t.Fatal(err)
	}
	title = snapshot.Track.Title.Value
	if err := s.UpdateTrackFields(id, &title, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := s.withTx(func(tx *sql.Tx) error {
		if err := syncRolesTx(tx, id, snapshot.Track.Roles, "user", true); err != nil {
			return err
		}
		origins, err := readOriginsTx(tx, "track", id)
		if err != nil {
			return err
		}
		if origins["title"].Mode != domain.Follow || origins["roles.vocal"].Mode != domain.Follow {
			t.Fatalf("equal saved values did not follow: %+v", origins)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snapshot.Track.Title = domain.Cleared[string]()
	snapshot.Track.Roles["vocal"] = domain.Cleared[[]domain.Role]()
	applyTestSnapshot(t, s, id, snapshot)
	meta, err := s.GetMetadata(id)
	if err != nil || meta.Title != "" {
		t.Fatalf("explicit clear: %+v %v", meta, err)
	}
}

func TestSnapshotNestedPartialAndStructuralChanges(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	snapshot.Release.DiscTotal = domain.Known(2)
	snapshot.Release.Media = domain.Known([]domain.Medium{{Number: 1, TrackTotal: domain.Known(2), Format: domain.Known("CD")}, {Number: 2, TrackTotal: domain.Known(1), Format: domain.Known("DVD")}})
	entry := func(n int, ref string) domain.TracklistEntry {
		return domain.TracklistEntry{DiscNumber: 1, TrackNumber: n, Track: domain.Known(domain.Identity{Provider: "test", Ref: ref}), Title: domain.Known(ref), DurationMS: domain.Known(3000)}
	}
	snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{entry(1, "a"), entry(2, "b")})
	snapshot.Release.TracklistCompleteness = domain.Complete
	applyTestSnapshot(t, s, id, snapshot)
	snapshot.Release.DiscTotal = domain.Missing[int]()
	snapshot.Release.Media = domain.Known([]domain.Medium{{Number: 1, TrackTotal: domain.Missing[int](), Format: domain.Missing[string]()}})
	partial := entry(1, "a")
	partial.DurationMS = domain.Missing[int]()
	partial.Title = domain.Cleared[string]()
	snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{partial})
	snapshot.Release.TracklistCompleteness = domain.Partial
	applyTestSnapshot(t, s, id, snapshot)
	var body string
	if err := s.db.QueryRow("SELECT body FROM release_snapshot").Scan(&body); err != nil {
		t.Fatal(err)
	}
	var saved domain.ReleaseSnapshot
	if err := json.Unmarshal([]byte(body), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Media.Value) != 2 || saved.Media.Value[0].TrackTotal.Value != 2 || len(saved.Tracklist.Value) != 2 || saved.Tracklist.Value[0].DurationMS.Value != 3000 || saved.Tracklist.Value[0].Title.State != domain.Empty || saved.TracklistCompleteness != domain.Complete {
		t.Fatalf("partial downgraded facts: %+v", saved)
	}
	snapshot.Release.DiscTotal = domain.Known(1)
	snapshot.Release.Media = domain.Known([]domain.Medium{{Number: 1, TrackTotal: domain.Known(1)}})
	replacement := entry(1, "c")
	replacement.DurationMS = domain.Missing[int]()
	snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{replacement})
	snapshot.Release.TracklistCompleteness = domain.Complete
	applyTestSnapshot(t, s, id, snapshot)
	if err := s.db.QueryRow("SELECT body FROM release_snapshot").Scan(&body); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(body), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Media.Value) != 1 || saved.Media.Value[0].Format.Value != "CD" || len(saved.Tracklist.Value) != 1 || saved.Tracklist.Value[0].DurationMS.IsKnown() {
		t.Fatalf("structural replacement retained removed facts: %+v", saved)
	}
}

func TestClearBindingPreservesCurrentButDropsFacts(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	snapshot.Track.Lyrics = domain.Known("[00:01]保留歌词")
	snapshot.Track.Identifiers = domain.Known([]domain.Identifier{{Kind: "ISRC", Value: "AA1234567890"}, {Kind: "ISRC", Value: "BB1234567890"}})
	applyTestSnapshot(t, s, id, snapshot)
	before, _ := s.GetMetadata(id)
	if err := s.ClearProvenance(id); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetMetadata(id)
	if err != nil || after.Title != before.Title || after.Lyrics == nil || *after.Lyrics != *before.Lyrics || after.ReleaseID == nil || *after.ReleaseID != *before.ReleaseID || after.ISRC != nil || after.MatchRef != nil {
		t.Fatalf("clear lost current or kept stale fact: %+v %v", after, err)
	}
	for _, table := range []string{"track_snapshot", "track_identifier"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM "+table+" WHERE track_id=?", id).Scan(&n); err != nil || n != 0 {
			t.Fatalf("stale %s: %d %v", table, n, err)
		}
	}
}

func TestLyricsRestartWriteFailureAndConcurrentReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, snapshot := snapshotTrack(t, s)
	snapshot.Track.Lyrics = domain.Known("[00:01]旧歌词\n")
	applyTestSnapshot(t, s, id, snapshot)
	if _, err := s.StageLyrics(context.Background(), string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	draft, err := s.StageLyrics(context.Background(), "new")
	if err != nil {
		t.Fatal(err)
	}
	// A second connection must not delete a live draft.
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.lyricsRoot, "drafts", draft.Token)); err != nil {
		t.Fatal("live draft deleted", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	currentDir := filepath.Join(s.lyricsRoot, "current")
	if err := os.Rename(currentDir, currentDir+".bak"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentDir, []byte("blocked directory"), 0600); err != nil {
		t.Fatal(err)
	}
	rev, _ := s.TrackRevision(id)
	if err := s.SaveLyrics(context.Background(), id, &draft, rev); err == nil {
		t.Fatal("write failure accepted")
	}
	if err := os.Remove(currentDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(currentDir+".bak", currentDir); err != nil {
		t.Fatal(err)
	}
	old, err := s.GetMetadata(id)
	if err != nil || old.Lyrics == nil || *old.Lyrics != "[00:01]旧歌词\n" {
		t.Fatal("write failure lost old lyric", err)
	}
	// Concurrent reads, refresh and collection never expose an unreferenced file.
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 8; n++ {
				meta, err := s.GetMetadata(id)
				if err != nil || meta.Lyrics == nil {
					t.Errorf("concurrent read: %v", err)
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		rev, _ := s.TrackRevision(id)
		err := s.ApplySnapshot(id, snapshot, rev)
		if err != nil && !errors.Is(err, ErrRevisionConflict) {
			t.Fatal(err)
		}
	}
	wg.Wait()
	orphan := filepath.Join(currentDir, "orphan.lrc")
	if err := os.WriteFile(orphan, []byte("orphan"), 0600); err != nil {
		t.Fatal(err)
	}
	root := s.lyricsRoot
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("restart did not clean orphan", err)
	}
	if _, err := os.Stat(filepath.Join(root, "drafts", draft.Token)); !os.IsNotExist(err) {
		t.Fatal("restart did not clean abandoned draft", err)
	}
	meta, err := s.GetMetadata(id)
	if err != nil || meta.Lyrics == nil || *meta.Lyrics != *old.Lyrics {
		t.Fatal("restart lost live lyric", err)
	}
	if err := s.DeleteTrack(id); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(currentDir)
	if err != nil || len(files) != 0 {
		t.Fatalf("delete retained unreferenced lyrics: %v %v", files, err)
	}
	var releases int
	if err := s.db.QueryRow("SELECT count(*) FROM release").Scan(&releases); err != nil || releases != 0 {
		t.Fatal("empty upstream release retained", err)
	}
}

func TestGroupSnapshotsAndAlbumCreditProjection(t *testing.T) {
	s := openTestStore(t)
	provider, groupRef, releaseRef, trackRef := "test", "native-group", "release", "track"
	id, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "Song", Album: ptr("Release title"), AlbumCredit: ptr("Album Artist"), MatchSource: &provider, MatchRef: &trackRef, Release: &ReleaseInput{Provider: &provider, ProviderRef: &releaseRef, ReleaseGroupRef: &groupRef}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &domain.Snapshot{Group: &domain.GroupSnapshot{Identity: domain.Identity{Provider: "test", Ref: groupRef}, Title: domain.Known("Group title"), Credit: domain.Known(domain.Credit{Text: "Group Artist"})}, Release: &domain.ReleaseSnapshot{Identity: domain.Identity{Provider: "test", Ref: releaseRef}, Credit: domain.Known(domain.Credit{Text: "New Album Artist"})}, Track: &domain.TrackSnapshot{Identity: domain.Identity{Provider: "test", Ref: trackRef}}}
	applyTestSnapshot(t, s, id, snapshot)
	var title, credit string
	if err := s.db.QueryRow("SELECT title,credit FROM release_group_current").Scan(&title, &credit); err != nil || title != "Group title" || credit != "Group Artist" {
		t.Fatalf("group snapshot not followed: %s %s %v", title, credit, err)
	}
	hits, err := s.SearchTrackIDs("New Album Artist", 10)
	if err != nil || len(hits) != 1 {
		t.Fatal("album credit not indexed", hits, err)
	}
	snapshot.Group.Title = domain.Missing[string]()
	snapshot.Release.Credit = domain.Cleared[domain.Credit]()
	applyTestSnapshot(t, s, id, snapshot)
	hits, err = s.SearchTrackIDs("New Album Artist", 10)
	if err != nil || len(hits) != 0 {
		t.Fatal("old album credit still indexed", hits, err)
	}
	if err := s.db.QueryRow("SELECT title FROM release_group_current").Scan(&title); err != nil || title != "Group title" {
		t.Fatal("unknown erased group title", title, err)
	}
	localID, err := s.CreateTrack(&Track{Album: ptr("独立本地发行")})
	if err != nil {
		t.Fatal(err)
	}
	local, _ := s.GetTrack(localID)
	localRelease, _ := s.GetRelease(*local.ReleaseID)
	if err := s.DeleteTrack(localID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM release_group WHERE id=?", *localRelease.ReleaseGroupID).Scan(&n); err != nil || n != 0 {
		t.Fatal("orphan local group retained", err)
	}
	upstream, _ := s.GetTrack(id)
	if err := s.DeleteTrack(id); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM release WHERE id=?", *upstream.ReleaseID).Scan(&n); err != nil || n != 0 {
		t.Fatal("empty upstream member retained", err)
	}
}
