package importer

import (
	"testing"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestRunnerReusesSyncOnlyReleaseAfterSafeSelection(t *testing.T) {
	for _, target := range []string{"file", "sync", "mixed"} {
		t.Run(target, func(t *testing.T) {
			st := openStore(t)
			// A different saved song establishes the release; it cannot cause a
			// track duplicate shortcut for the song being imported.
			saved, err := st.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false,
				Title: "Other song", Credit: sptr("Artist"), Album: sptr("Album"), AlbumCredit: sptr("Artist"),
				MatchSource: sptr("musicbrainz"), MatchRef: sptr("saved"),
				Release: &store.ReleaseInput{Provider: sptr("musicbrainz"), ProviderRef: sptr("rel-Album"), ReleaseGroupRef: sptr("rg-Album")},
			})
			if err != nil {
				t.Fatal(err)
			}
			var batchID, playlistID int64
			if target != "file" {
				_, binding := importSyncFixture(t, st)
				batchID, playlistID = binding.BatchID, binding.PlaylistID
			}
			if target != "sync" {
				fileBatch := enqueueOne(t, st, "Song", "Artist", "Album")
				if target == "file" {
					batchID = fileBatch
				}
			}
			a := mbCandidate("existing-release-next", "Song", "Artist", "Album")
			b := mbCandidate("new-release-next", "Song", "Artist", "Album")
			b.ReleaseRef = sptr("new-release")
			a.RecordingRef, b.RecordingRef = sptr("same-recording"), sptr("same-recording")
			// No total is needed when the existing-release subset is unique.
			searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Song": {b, a}}}
			runner := newRunner(st, searcher)
			t.Cleanup(runner.Close)
			runToCompletion(t, runner, batchID)
			stats, err := st.Stats(batchID)
			if err != nil || stats.Imported != 1 || stats.Review != 0 || stats.Skipped != 0 || stats.Failed != 0 {
				t.Fatal("existing release should import, not skip the other song", stats, err)
			}
			id, err := st.FindByMatchRef("musicbrainz", a.MatchRef)
			if err != nil || id == 0 || id == saved {
				t.Fatal(id, err)
			}
			track, err := st.GetTrack(id)
			if err != nil || track.InLibrary != (target != "sync") {
				t.Fatal("wrong destination membership", track, err)
			}
			original, err := st.GetTrack(saved)
			if err != nil || original.InLibrary || *track.ReleaseID != *original.ReleaseID {
				t.Fatal("did not reuse release or changed original membership", original, err)
			}
			if release, err := st.GetReleaseByRef("musicbrainz", "new-release"); err != nil || release != nil {
				t.Fatal("created unapproved version", release, err)
			}
			if playlistID != 0 {
				entries, err := st.SyncedPlaylistState(playlistID)
				if err != nil || len(entries) != 1 || entries[0].LocalState != playlists.Imported || entries[0].TrackID == nil || *entries[0].TrackID != id {
					t.Fatal("target delivery missing", entries, err)
				}
			}
			queries := searcher.queryCount()
			runToCompletion(t, runner, batchID)
			if searcher.queryCount() != queries {
				t.Fatal("successful terminal work was repeated")
			}
		})
	}
}

func TestRunnerExistingReleaseCannotResolveDifferentRecordings(t *testing.T) {
	st := openStore(t)
	if _, err := st.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false,
		Title: "Other song", Album: sptr("Album"), AlbumCredit: sptr("Artist"),
		MatchSource: sptr("musicbrainz"), MatchRef: sptr("saved"),
		Release: &store.ReleaseInput{Provider: sptr("musicbrainz"), ProviderRef: sptr("rel-Album"), ReleaseGroupRef: sptr("rg-Album")},
	}); err != nil {
		t.Fatal(err)
	}
	a := mbCandidate("cd-track", "Song", "Artist", "Album")
	b := mbCandidate("dvd-track", "Song", "Artist", "Album")
	a.RecordingRef, b.RecordingRef = sptr("cd-recording"), sptr("dvd-recording")
	n := 21
	a.ReleaseTrackTotal, b.ReleaseTrackTotal = &n, &n
	batch := enqueueOne(t, st, "Song", "Artist", "Album")
	runner := newRunner(st, &recordingSearcher{byTitle: map[string][]provider.Candidate{"Song": {a, b}}})
	t.Cleanup(runner.Close)
	runToCompletion(t, runner, batch)
	items, err := st.AllReviewItems()
	if err != nil || len(items) != 1 || items[0].ReviewReason == nil || *items[0].ReviewReason != "ambiguous" {
		t.Fatal("same release did not preserve recording ambiguity", items, err)
	}
	for _, ref := range []string{a.MatchRef, b.MatchRef} {
		if id, err := st.FindByMatchRef("musicbrainz", ref); err != nil || id != 0 {
			t.Fatal("ambiguous recording automatically admitted", ref, id, err)
		}
	}
}
