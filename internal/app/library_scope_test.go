package app

import (
	"encoding/json"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestLibraryScopeExportEmptyStateAndReferenceAwareDelete(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	hidden, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "Sync only", Album: strPtrOrNil("Hidden album"), Credit: strPtrOrNil("Hidden artist")})
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.ListTracks(store.TrackFilter{})
	if err != nil || page.Total != 0 || len(page.Tracks) != 0 {
		t.Fatal("sync-only database is an empty library", page, err)
	}
	if artists, err := a.ListArtists(); err != nil || len(artists) != 0 {
		t.Fatal(artists, err)
	}
	if albums, err := a.ListAlbums(); err != nil || len(albums) != 0 {
		t.Fatal(albums, err)
	}
	visible, err := a.CreateTrackManual("Library song", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []ExportRequest{{Scope: "all"}, {Scope: "selection", TrackIDs: []int64{hidden, visible, hidden, visible}}, {Scope: "filtered", Filter: store.TrackFilter{}}, {Scope: "filtered", Filter: store.TrackFilter{IncludeSynced: true, Limit: 1}}, {Scope: "selection", TrackIDs: []int64{hidden}}} {
		tracks, err := a.resolveExportScope(request)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if len(request.TrackIDs) == 1 {
			want = 0
		}
		if len(tracks) != want {
			t.Fatal(request, tracks)
		}
		for _, track := range tracks {
			if track.Title != "Library song" {
				t.Fatal("sync-only export leaked", track)
			}
		}
	}
	if err := a.DeleteTracks([]int64{visible, hidden}); err == nil {
		t.Fatal("mixed illegal delete accepted")
	}
	if track, err := a.store.GetTrack(visible); err != nil || track == nil {
		t.Fatal("partial delete", track, err)
	}
}

func TestGhostExactHiddenTrackExplicitlyAddsExistingIdentity(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	source := "musicbrainz"
	ref := "hidden-exact"
	releaseRef := "release"
	hidden, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "User title", MatchSource: &source, MatchRef: &ref, Release: &store.ReleaseInput{Provider: &source, ProviderRef: &releaseRef}})
	if err != nil {
		t.Fatal(err)
	}
	track, err := a.store.GetTrack(hidden)
	if err != nil {
		t.Fatal(err)
	}
	// Keep this release visible through another real library member.
	_, err = a.store.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: true, ExistingReleaseID: track.ReleaseID, Title: "Visible"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := a.store.GetReleaseSnapshot(*track.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.TracklistCompleteness = domain.Complete
	snapshot.Tracklist = domain.Known([]domain.TracklistEntry{{DiscNumber: 1, TrackNumber: 1, Track: domain.Known(domain.Identity{Provider: "musicbrainz", Ref: ref}), Title: domain.Known("Upstream")}})
	revision, err := a.store.ReleaseRevision(*track.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.ApplyReleaseSnapshot(*track.ReleaseID, &domain.Snapshot{Release: snapshot}, revision); err != nil {
		t.Fatal(err)
	}
	detail, err := a.GetAlbumDetail(*track.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	ghost := false
	for _, row := range detail.Rows {
		if row.TrackRef != nil && *row.TrackRef == ref {
			ghost = row.Ghost
			if row.TrackID != 0 || row.AddedAt != nil {
				t.Fatal(row)
			}
		}
	}
	if !ghost {
		t.Fatal("hidden identity must remain missing in library tracklist", detail)
	}
	id, err := a.AddGhostRowToLibrary(*track.ReleaseID, 1, 1)
	if err != nil || id != hidden {
		t.Fatal(id, err)
	}
	updated, err := a.store.GetTrack(hidden)
	if err != nil || !updated.InLibrary || updated.Title != "User title" {
		t.Fatal(updated, err)
	}
	// Search explicit add shares the same membership primitive, without copying.
	second, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "Search hidden", MatchSource: &source, MatchRef: strPtrOrNil("search-hidden")})
	if err != nil {
		t.Fatal(err)
	}
	candidate := fullCandidate("search-hidden")
	id, err = a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: candidate}))
	if err != nil || id != second {
		t.Fatal(id, err)
	}
	view, err := a.store.GetTrackView(second)
	if err != nil || !view.InLibrary {
		t.Fatal(view, err)
	}
	// Timestamps are strings on both existing detail and list wire models.
	if _, err := json.Marshal(view); err != nil {
		t.Fatal(err)
	}
}
