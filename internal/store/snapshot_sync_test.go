package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

func snapshotTrack(t *testing.T, s *Store) (int64, *domain.Snapshot) {
	t.Helper()
	title, album, credit, ref := "Song", "Album", "Album Artist", "release"
	id, err := s.AdmitCandidate(provider.Candidate{Provider: "test", MatchRef: "track", Title: &title, AlbumTitle: &album, AlbumCredit: &credit, ReleaseRef: &ref})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &domain.Snapshot{Release: &domain.ReleaseSnapshot{Identity: domain.Identity{Provider: "test", Ref: ref}, Title: domain.Known(album), Credit: domain.Known(domain.Credit{Text: credit})}, Track: &domain.TrackSnapshot{Identity: domain.Identity{Provider: "test", Ref: "track"}, Title: domain.Known("Upstream Song"), Credit: domain.Known(domain.Credit{Text: "Creator"}), Year: domain.Known(2014), Roles: map[string]domain.Field[[]domain.Role]{"vocal": domain.Known([]domain.Role{{Role: "vocal", Artist: domain.Artist{Name: "初音未来", Identity: &domain.Identity{Provider: "test", Ref: "miku"}}, Attributes: []string{"合声", "guest"}}})}}}
	return id, snapshot
}
func applyTestSnapshot(t *testing.T, s *Store, id int64, snapshot *domain.Snapshot) {
	t.Helper()
	rev, err := s.TrackRevision(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySnapshot(id, snapshot, rev); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotUserFieldsRolesAndPartialResponses(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	applyTestSnapshot(t, s, id, snapshot)
	meta, err := s.GetMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Upstream Song" || meta.Year == nil || *meta.Year != 2014 {
		t.Fatalf("snapshot not followed: %+v", meta)
	}
	if err := s.ApplyMetadataEdits(id, map[string]string{"title": "My Song", "credit": "User Artist"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateReleaseFields(*meta.ReleaseID, map[string]string{"title": "My Album"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.withTx(func(tx *sql.Tx) error {
		return syncRolesTx(tx, id, map[string]domain.Field[[]domain.Role]{"lyricist": domain.Known([]domain.Role{{Artist: domain.Artist{Name: "用户作词"}, Role: "lyricist"}})}, "user", true)
	}); err != nil {
		t.Fatal(err)
	}
	snapshot.Track.Title = domain.Known("Changed")
	snapshot.Release.Title = domain.Cleared[string]()
	snapshot.Track.Credit = domain.Cleared[domain.Credit]()
	snapshot.Track.Year = domain.Missing[int]()
	snapshot.Track.Roles = map[string]domain.Field[[]domain.Role]{"lyricist": domain.Cleared[[]domain.Role]()}
	applyTestSnapshot(t, s, id, snapshot)
	meta, err = s.GetMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "My Song" || deref(meta.Credit) != "User Artist" || deref(meta.Album) != "My Album" || meta.Year == nil || *meta.Year != 2014 {
		t.Fatalf("user/unknown protection failed: %+v", meta)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM track_artist WHERE track_id=?", id).Scan(&n); err != nil || n != 2 {
		t.Fatalf("roles lost: %d %v", n, err)
	}
	var attrs, artistRef string
	if err := s.db.QueryRow("SELECT attributes_json,provider_ref FROM track_artist ta JOIN artist a ON a.id=ta.artist_id WHERE ta.track_id=? AND ta.role='vocal'", id).Scan(&attrs, &artistRef); err != nil || artistRef != "miku" || attrs != `["合声","guest"]` {
		t.Fatalf("role facts lost: %s %s %v", attrs, artistRef, err)
	}
	snapshot.Track.Roles = map[string]domain.Field[[]domain.Role]{"vocal": domain.Cleared[[]domain.Role]()}
	applyTestSnapshot(t, s, id, snapshot)
	if err := s.db.QueryRow("SELECT count(*) FROM track_artist WHERE track_id=? AND role='vocal'", id).Scan(&n); err != nil || n != 0 {
		t.Fatal("explicit empty did not clear follow", err)
	}
}

func TestSnapshotRevisionAndFailureAtomicity(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	rev, _ := s.TrackRevision(id)
	if err := s.ApplySnapshot(id, snapshot, rev-1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal(err)
	}
	s.beforeCommit = func(*sql.Tx) error { return errors.New("commit failure") }
	if err := s.ApplySnapshot(id, snapshot, rev); err == nil {
		t.Fatal("expected failure")
	}
	s.beforeCommit = nil
	meta, err := s.GetMetadata(id)
	if err != nil || meta.Title != "Song" {
		t.Fatalf("partial update: %+v %v", meta, err)
	}
	snapshot.Track.Identity.Ref = "wrong"
	if err := s.ApplySnapshot(id, snapshot, rev); !errors.Is(err, ErrSnapshotTarget) {
		t.Fatal(err)
	}
}

func TestLyricsDraftFailureCancelAndRestart(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	snapshot.Track.Lyrics = domain.Known("[00:01.20]初音未来\n第二行\n")
	applyTestSnapshot(t, s, id, snapshot)
	old, err := s.GetMetadata(id)
	if err != nil || old.Lyrics == nil {
		t.Fatal(err)
	}
	draft, err := s.StageLyrics(context.Background(), "[00:02]用户歌词")
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := s.GetMetadata(id)
	if *meta.Lyrics != *old.Lyrics {
		t.Fatal("draft applied early")
	}
	if err := s.DiscardLyrics(draft); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.StageLyrics(ctx, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	draft, err = s.StageLyrics(context.Background(), "replacement")
	if err != nil {
		t.Fatal(err)
	}
	s.beforeCommit = func(*sql.Tx) error { return errors.New("commit failure") }
	rev, _ := s.TrackRevision(id)
	if err := s.SaveLyrics(context.Background(), id, &draft, rev); err == nil {
		t.Fatal("expected commit failure")
	}
	s.beforeCommit = nil
	meta, _ = s.GetMetadata(id)
	if *meta.Lyrics != *old.Lyrics {
		t.Fatal("failed commit changed old lyrics")
	}
	if err := s.SaveLyrics(context.Background(), id, &draft, rev); err != nil {
		t.Fatal(err)
	}
	snapshot.Track.Lyrics = domain.Missing[string]()
	applyTestSnapshot(t, s, id, snapshot)
	meta, _ = s.GetMetadata(id)
	if *meta.Lyrics != "replacement" {
		t.Fatal("unknown removed user lyrics")
	}
	if err := os.WriteFile(filepath.Join(s.lyricsRoot, "current", "orphan.lrc"), []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.collectLyrics(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.lyricsRoot, "current", "orphan.lrc")); !os.IsNotExist(err) {
		t.Fatal("orphan not cleaned", err)
	}
	meta, _ = s.GetMetadata(id)
	if *meta.Lyrics != "replacement" {
		t.Fatal("collection deleted current lyrics")
	}
}
