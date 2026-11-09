package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

func TestLibraryQueriesExcludeSyncOnlyEntities(t *testing.T) {
	s := openTestStore(t)
	local, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: true, Title: "Search Song", Credit: ptr("Library Artist"), Album: ptr("Library Album")})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "Search Song", Credit: ptr("Hidden Artist"), Album: ptr("Hidden Album"), MatchSource: ptr("test"), MatchRef: ptr("hidden")})
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []TrackFilter{{}, {RecentOnly: true}, {Query: "Search"}, {Title: "Song"}} {
		tracks, err := s.ListTracks(filter)
		if err != nil || len(tracks) != 1 || tracks[0].ID != local {
			t.Fatal(tracks, err)
		}
		count, err := s.CountTracks(filter)
		if err != nil || count != 1 {
			t.Fatal(count, err)
		}
	}
	artists, err := s.ListArtists()
	if err != nil || len(artists) != 1 || artists[0].Name != "Library Artist" {
		t.Fatal(artists, err)
	}
	var artistID int64
	if err := s.db.QueryRow(`SELECT id FROM artist WHERE name='Hidden Artist'`).Scan(&artistID); err != nil {
		t.Fatal(err)
	}
	facets, err := s.RoleFacets(artistID)
	if err != nil {
		t.Fatal(err)
	}
	for _, facet := range facets {
		if facet.Count != 0 {
			t.Fatal(facet)
		}
	}
	albums, err := s.ListAlbums()
	if err != nil || len(albums) != 1 {
		t.Fatal(albums, err)
	}
	if id, err := s.FindByMatchRef("test", "hidden"); err != nil || id != hidden {
		t.Fatal("identity filtered", id, err)
	}
	view, err := s.GetTrackView(hidden)
	if err != nil || view == nil || view.InLibrary {
		t.Fatal(view, err)
	}
	// All real members remain visible to structural queries.
	count, err := s.ReleaseTrackCount(*view.ReleaseID)
	if err != nil || count != 1 {
		t.Fatal("hidden structure lost", count, err)
	}
}

func TestFTSScopePrecedesLimitAndSyncedRowsKeepOccurrences(t *testing.T) {
	s := openTestStore(t)
	var hidden, visible int64
	if err := s.withTx(func(tx *sql.Tx) error {
		for i := 0; i < 2005; i++ {
			id, err := insertTrackTx(tx, NewTrackInput{InLibrary: false, Title: "Needle"})
			if err != nil {
				return err
			}
			hidden = id
		}
		var err error
		visible, err = insertTrackTx(tx, NewTrackInput{InLibrary: true, Title: "Needle"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchTrackIDs("Needle", 1)
	if err != nil || len(hits) != 1 || hits[0] != visible {
		t.Fatal(hits, err)
	}
	tracks, err := s.ListTracks(TrackFilter{Query: "Needle"})
	if err != nil || len(tracks) != 1 || tracks[0].ID != visible {
		t.Fatal(tracks, err)
	}
	snapshot := syncTracks("one", "one")
	p, _, _ := saveSyncFixture(t, s, snapshot, false)
	if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET local_state='imported',track_id=? WHERE playlist_id=?`, hidden, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET upstream_present=0 WHERE playlist_id=? AND position=2`, p); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListSyncedTracks(p, "Needle")
	if err != nil || len(rows) != 2 || rows[0].TrackID != hidden || rows[0].EntryID == rows[1].EntryID || rows[1].UpstreamPresent {
		t.Fatal(rows, err)
	}
}

func TestLibraryAdditionAtomicFirstTimeAndStale(t *testing.T) {
	s := openTestStore(t)
	a, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateTrack(&Track{Title: "b"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewLibraryAddition([]int64{a, b, a})
	if err != nil || len(preview.WillAdd) != 1 || len(preview.AlreadyInLibrary) != 1 {
		t.Fatal(preview, err)
	}
	before, err := s.GetTrack(a)
	if err != nil || before.LibraryAddedAt != nil {
		t.Fatal(before, err)
	}
	result, err := s.CommitLibraryAddition(context.Background(), "add-1", "hash", []int64{a, b, a})
	if err != nil || len(result.WillAdd) != 1 || len(result.AlreadyInLibrary) != 1 {
		t.Fatal(result, err)
	}
	first, err := s.GetTrack(a)
	if err != nil || first.LibraryAddedAt == nil {
		t.Fatal(first, err)
	}
	if err := s.withTx(func(tx *sql.Tx) error { return setLibraryMembershipTx(tx, a, false, time.Now()) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitLibraryAddition(context.Background(), "add-2", "hash", []int64{a}); err != nil {
		t.Fatal(err)
	}
	again, err := s.GetTrack(a)
	if err != nil || !again.LibraryAddedAt.Equal(*first.LibraryAddedAt) {
		t.Fatal(again, err)
	}
	if _, err := s.CommitLibraryAddition(context.Background(), "bad", "hash", []int64{a, 99999}); !errors.Is(err, ErrPlanStale) {
		t.Fatal(err)
	}
	if _, err := s.CommitLibraryAddition(context.Background(), "add-1", "forged", []int64{a}); !errors.Is(err, ErrPlanStale) {
		t.Fatal(err)
	}
}

func TestCollectionRejectsMixedNonLibrarySelection(t *testing.T) {
	s := openTestStore(t)
	local, err := s.CreateTrack(&Track{Title: "local"})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "hidden"})
	if err != nil {
		t.Fatal(err)
	}
	collection, err := s.CreateCollection("List", KindPlaylist, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddToCollection(collection, []int64{local, hidden}); err == nil {
		t.Fatal("mixed non-library accepted")
	}
	view, err := s.GetCollection(collection)
	if err != nil || view.TrackCount != 0 {
		t.Fatal(view, err)
	}
	if err := s.AddToCollection(collection, []int64{local, 999}); err == nil {
		t.Fatal("missing selection accepted")
	}
	if err := s.AddToCollection(collection, []int64{local, local}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCollection("sync", CollectionKind("synced_playlist"), nil); err == nil {
		t.Fatal("sync created as writable collection")
	}
}

func TestLibraryRemovalAndBindingDeletionRespectAllReferences(t *testing.T) {
	s := openTestStore(t)
	track, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: true, Title: "shared", Album: ptr("Album")})
	if err != nil {
		t.Fatal(err)
	}
	p1, _, _ := saveSyncFixture(t, s, syncTracks("one"), false)
	second := syncTracks("one")
	second.Ref.ID = "other"
	p2, _, _ := saveSyncFixture(t, s, second, false)
	if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET local_state='imported',track_id=?`, track); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET upstream_present=0 WHERE playlist_id=?`, p2); err != nil {
		t.Fatal(err)
	}
	collection, err := s.CreateCollection("ordinary", KindPlaylist, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddToCollection(collection, []int64{track}); err != nil {
		t.Fatal(err)
	}
	before, err := s.GetTrack(track)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTrack(track); err == nil {
		t.Fatal("physical deletion bypassed FK")
	}
	s.beforeCommit = func(*sql.Tx) error { return errors.New("injected") }
	if err := s.RemoveFromLibrary([]int64{track}); err == nil {
		t.Fatal("expected rollback")
	}
	s.beforeCommit = nil
	still, err := s.GetTrack(track)
	if err != nil || !still.InLibrary {
		t.Fatal(still, err)
	}
	if err := s.RemoveFromLibrary([]int64{track}); err != nil {
		t.Fatal(err)
	}
	still, err = s.GetTrack(track)
	if err != nil || still == nil || still.InLibrary || !still.LibraryAddedAt.Equal(*before.LibraryAddedAt) {
		t.Fatal(still, err)
	}
	view, err := s.GetCollection(collection)
	if err != nil || view.TrackCount != 0 {
		t.Fatal(view, err)
	}
	if err := s.RemoveFromLibrary([]int64{track}); err == nil {
		t.Fatal("direct sync-only removal allowed")
	}
	if err := s.DeleteSyncedPlaylist(p1); err != nil {
		t.Fatal(err)
	}
	still, err = s.GetTrack(track)
	if err != nil || still == nil {
		t.Fatal("absent ref ignored", still, err)
	}
	rows, err := s.ListSyncedTracks(p2, "")
	if err != nil || len(rows) != 1 || rows[0].UpstreamPresent {
		t.Fatal(rows, err)
	}
	if err := s.DeleteSyncedPlaylist(p2); err != nil {
		t.Fatal(err)
	}
	still, err = s.GetTrack(track)
	if err != nil || still != nil {
		t.Fatal("unreferenced hidden track leaked", still, err)
	}
	release, err := s.GetRelease(*before.ReleaseID)
	if err != nil || release != nil {
		t.Fatal("last track release leaked", release, err)
	}
}

func TestBindingDeletionRollbackPreservesSharedFilesAndLibraryTracks(t *testing.T) {
	s := openTestStore(t)
	shared, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "shared", Album: ptr("Album")})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: true, Title: "library", Album: ptr("Album")})
	if err != nil {
		t.Fatal(err)
	}
	track, err := s.GetTrack(shared)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{shared, member} {
		draft, err := s.StageLyrics(context.Background(), "[00:01]shared lyrics")
		if err != nil {
			t.Fatal(err)
		}
		rev, err := s.TrackRevision(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveLyrics(context.Background(), id, &draft, rev); err != nil {
			t.Fatal(err)
		}
	}
	// 相同文本默认按曲目保存不同路径；明确构造共享文件 FK，验证引用保护。
	if _, err := s.db.Exec(`UPDATE track_current SET lyrics_file_id=(SELECT lyrics_file_id FROM track_current WHERE track_id=?) WHERE track_id=?`, shared, member); err != nil {
		t.Fatal(err)
	}
	var relative string
	if err := s.db.QueryRow(`SELECT relative_path FROM lyrics_file WHERE id=(SELECT lyrics_file_id FROM track_current WHERE track_id=?)`, shared).Scan(&relative); err != nil {
		t.Fatal(err)
	}
	catalog := mustSaveCatalog(t, s, CoverCatalogKey{Source: "local", ObjectType: "local-image", SourceRef: "shared"}, sampleCoverImages())
	file := sampleCoverFile(catalog.Images[0].Variants[0].ID, *track.ReleaseID, "albums/shared/Front.jpg")
	if _, err := s.AdoptCoverFile(file, 0); err != nil {
		t.Fatal(err)
	}
	p, _, _ := saveSyncFixture(t, s, syncTracks("shared", "library"), false)
	if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET local_state='imported',track_id=CASE position WHEN 1 THEN ? ELSE ? END WHERE playlist_id=?`, shared, member, p); err != nil {
		t.Fatal(err)
	}
	s.beforeCommit = func(*sql.Tx) error { return errors.New("injected") }
	if err := s.DeleteSyncedPlaylist(p); err == nil {
		t.Fatal("expected rollback")
	}
	s.beforeCommit = nil
	if rows, err := s.ListSyncedTracks(p, ""); err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	if err := s.DeleteSyncedPlaylist(p); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.GetTrack(shared); err != nil || deleted != nil {
		t.Fatal(deleted, err)
	}
	if kept, err := s.GetTrack(member); err != nil || kept == nil || !kept.InLibrary {
		t.Fatal(kept, err)
	}
	meta, err := s.GetMetadata(member)
	if err != nil || meta.Lyrics == nil || *meta.Lyrics != "[00:01]shared lyrics" {
		t.Fatal(meta, err)
	}
	if _, err := os.Stat(filepath.Join(s.lyricsRoot, relative)); err != nil {
		t.Fatal("shared lyrics file removed", err)
	}
	files, err := s.ListReleaseCoverFiles(*track.ReleaseID)
	if err != nil || len(files) != 1 || !files[0].Protected {
		t.Fatal("shared cover reference lost", files, err)
	}
	if err := s.RemoveFromLibrary([]int64{member}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.lyricsRoot, relative)); !os.IsNotExist(err) {
		t.Fatal("orphan lyrics file retained", err)
	}
	for _, table := range []string{"release", "release_group", "release_cover_selection", "cover_file"} {
		var n int
		if err := s.db.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
}

func TestLibraryAdditionDeletedIDNeverAliasesNewTrack(t *testing.T) {
	s := openTestStore(t)
	id, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "old"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewLibraryAddition([]int64{id, other}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTrack(id); err != nil {
		t.Fatal(err)
	}
	replacement, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "new"})
	if err != nil || replacement == id {
		t.Fatal(replacement, err)
	}
	if _, err := s.CommitLibraryAddition(context.Background(), "stale", "hash", []int64{id, other}); !errors.Is(err, ErrPlanStale) {
		t.Fatal(err)
	}
	track, err := s.GetTrack(other)
	if err != nil || track.InLibrary {
		t.Fatal("partial addition", track, err)
	}
	s.beforeCommit = func(*sql.Tx) error { return errors.New("injected") }
	if _, err := s.CommitLibraryAddition(context.Background(), "rollback", "hash", []int64{other, replacement}); err == nil {
		t.Fatal("expected rollback")
	}
	s.beforeCommit = nil
	for _, id := range []int64{other, replacement} {
		track, err := s.GetTrack(id)
		if err != nil || track.InLibrary {
			t.Fatal(track, err)
		}
	}
}

func TestLibraryRecentOrderingUsesFirstAdditionNotCreation(t *testing.T) {
	s := openTestStore(t)
	delayed, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "delayed"})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := s.CreateTrack(&Track{Title: "ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.withTx(func(tx *sql.Tx) error {
		return setLibraryMembershipTx(tx, delayed, true, time.Now().UTC().Add(time.Hour))
	}); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.ListTracks(TrackFilter{RecentOnly: true})
	if err != nil || len(tracks) != 2 || tracks[0].ID != delayed || tracks[1].ID != ordinary {
		t.Fatal(tracks, err)
	}
}

func TestSyncOnlyReleaseStaysInAdmissionButNotLibraryRepresentative(t *testing.T) {
	s := openTestStore(t)
	makeTrack := func(inLibrary bool, ref string) int64 {
		t.Helper()
		id, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: inLibrary, Title: ref, Album: ptr("Album"), AlbumCredit: ptr("Artist"), MatchSource: ptr("test"), MatchRef: ptr(ref), Release: &ReleaseInput{Provider: ptr("test"), ProviderRef: ptr(ref), ReleaseGroupRef: ptr("group")}})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	hidden := makeTrack(false, "hidden")
	visible := makeTrack(true, "visible")
	hiddenView, _ := s.GetTrackView(hidden)
	visibleView, _ := s.GetTrackView(visible)
	rel, err := s.GetRelease(*hiddenView.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	members, err := s.ListGroupReleases(*rel.ReleaseGroupID)
	if err != nil || len(members) != 2 {
		t.Fatal(members, err)
	}
	owned, err := s.ListLibraryGroupReleases(*rel.ReleaseGroupID)
	if err != nil || len(owned) != 1 || owned[0].ID != *visibleView.ReleaseID {
		t.Fatal(owned, err)
	}
	rep, err := s.RepresentativeRelease(*rel.ReleaseGroupID)
	if err != nil || rep.ID != *visibleView.ReleaseID {
		t.Fatal(rep, err)
	}
	resolution, err := s.ResolveGroup(domain.Snapshot{Release: &domain.ReleaseSnapshot{Group: &domain.Identity{Provider: "test", Ref: "group"}}})
	if err != nil || len(resolution.Groups) != 1 || len(resolution.Groups[0].Members) != 2 {
		t.Fatal(resolution, err)
	}
	if err := s.DeleteOrphanRelease(*hiddenView.ReleaseID); err != nil {
		t.Fatal(err)
	}
	rel, err = s.GetRelease(*hiddenView.ReleaseID)
	if err != nil || rel == nil {
		t.Fatal("hidden reference deleted", rel, err)
	}
}
