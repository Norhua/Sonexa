package importer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func seedLocalRunnerTrack(t *testing.T, st *store.Store, complete bool, repeated bool) (int64, provider.Candidate) {
	t.Helper()
	c := mbCandidate("local-track", "Song", "Artist", "Album")
	disc, position, duration := 2, 3, 620000
	c.DiscNumber, c.TrackNumber, c.DurationMS = &disc, &position, &duration
	c.RecordingRef = sptr("recording")
	id, err := st.AdmitCandidate(c)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := provider.CandidateSnapshot(c.Provider, c)
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		snapshot.Release.TracklistCompleteness = domain.Complete
		snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{{DiscNumber: 2, TrackNumber: 3, Track: domain.Known(snapshot.Track.Identity), Title: snapshot.Track.Title, Credit: snapshot.Track.Credit, DurationMS: snapshot.Track.DurationMS, Recording: snapshot.Track.Recording}})
		if repeated {
			entry := snapshot.Release.Tracklist.Value[0]
			entry.TrackNumber = 4
			entry.Track = domain.Known(domain.Identity{Provider: c.Provider, Ref: "other-position"})
			snapshot.Release.Tracklist.Value = append(snapshot.Release.Tracklist.Value, entry)
		}
	}
	revision, err := st.TrackRevision(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ApplySnapshot(id, snapshot, revision); err != nil {
		t.Fatal(err)
	}
	return id, c
}

func TestRunnerLocalFactsSkipWithoutHistoryOrNetwork(t *testing.T) {
	st := openStore(t)
	id, _ := seedLocalRunnerTrack(t, st, true, false)
	if err := st.ApplyMetadataEdits(id, map[string]string{"title": "用户改名"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	before, err := st.GetMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	searcher := &recordingSearcher{}
	runner := newRunner(st, searcher)
	libraryEvents, enrichments := 0, 0
	runner.SetLibraryChanged(func() { libraryEvents++ })
	runner.SetEnricher(func(int64, string, string) { enrichments++ })
	runner.fetchOnMatch = true
	for i := 0; i < 2; i++ {
		batch := enqueueOne(t, st, "Song（译名：歌曲）", "Artist", "Album")
		events := runToCompletion(t, runner, batch)
		stats, err := st.Stats(batch)
		if err != nil || stats.Skipped != 1 || stats.Imported != 0 || stats.Review != 0 {
			t.Fatal(stats, err, events)
		}
		var target int64
		var reason, raw string
		if err := st.DB().QueryRow("SELECT track_id,resolution_reason,raw_row FROM import_item WHERE batch_id=?", batch).Scan(&target, &reason, &raw); err != nil || target != id || reason != store.ResolutionExistingTrack || !strings.Contains(raw, "译名") {
			t.Fatal(target, reason, raw, err)
		}
		found := false
		for _, event := range events {
			if strings.Contains(event.Line, "[已存在跳过]") {
				found = true
			}
		}
		if !found {
			t.Fatal("skip was logged as a new import", events)
		}
		if _, err := st.DB().Exec("DELETE FROM import_batch WHERE id=?", batch); err != nil {
			t.Fatal(err)
		}
	}
	if searcher.queryCount() != 0 || libraryEvents != 0 || enrichments != 0 {
		t.Fatal("pure skip caused upstream/enrichment/library work", searcher.queryCount(), libraryEvents, enrichments)
	}
	after, err := st.GetMetadata(id)
	afterJSON, _ := json.Marshal(after)
	if err != nil || string(beforeJSON) != string(afterJSON) {
		t.Fatal("skip changed collection", err)
	}
}

func TestRunnerSparseOrAmbiguousLocalFactsStillSearch(t *testing.T) {
	for _, scenario := range []string{"sparse", "no_album", "repeated_position", "different_album"} {
		t.Run(scenario, func(t *testing.T) {
			st := openStore(t)
			id, _ := seedLocalRunnerTrack(t, st, scenario != "sparse", scenario == "repeated_position")
			album := "Album"
			if scenario == "no_album" {
				album = ""
			}
			if scenario == "different_album" {
				album = "Different Album"
			}
			searcher := &recordingSearcher{}
			batch := enqueueOne(t, st, "Song", "Artist", album)
			runToCompletion(t, newRunner(st, searcher), batch)
			stats, err := st.Stats(batch)
			if err != nil || stats.Review != 1 || stats.Skipped != 0 || stats.Imported != 0 || searcher.queryCount() == 0 {
				t.Fatal(stats, err, searcher.queryCount())
			}
			if current, err := st.GetTrack(id); err != nil || current == nil {
				t.Fatal(current, err)
			}
		})
	}
}

func TestRunnerLocalKnownPositionCanDisambiguateRepeatedRecording(t *testing.T) {
	st := openStore(t)
	id, _ := seedLocalRunnerTrack(t, st, true, true)
	batch := enqueueOne(t, st, "Song", "Artist", "Album")
	// Use a new input profile, without mutating immutable parsed columns.
	items, err := st.DB().Query("SELECT id FROM import_item WHERE batch_id=?", batch)
	if err != nil {
		t.Fatal(err)
	}
	var itemID int64
	if items.Next() {
		if err := items.Scan(&itemID); err != nil {
			t.Fatal(err)
		}
	}
	items.Close()
	profile := matching.NewInputProfile("Song", "Artist", "Album")
	profile.Disc = ParsePositionEvidence("2", "disc")
	profile.Track = ParsePositionEvidence("3", "track")
	raw, _ := json.Marshal(profile)
	if _, err := st.DB().Exec("UPDATE import_item SET input_profile=? WHERE id=?", string(raw), itemID); err != nil {
		t.Fatal(err)
	}
	searcher := &recordingSearcher{}
	runner := newRunner(st, searcher)
	item, err := st.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	runner.processItem(context.Background(), item)
	finished, err := st.GetItem(item.ID)
	if err != nil || finished.Status != store.StatusSkipped || finished.TrackID == nil || *finished.TrackID != id || searcher.queryCount() != 0 {
		t.Fatal(finished, err, searcher.queryCount())
	}
}
