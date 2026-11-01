package app

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// Search facts are fixed and offline; identity validation is the registered
// MusicBrainz adapter. No application data or previous human choices are used.
type pairedImportProvider struct {
	provider.MetadataProvider
	candidate provider.Candidate
	calls     atomic.Int32
}

func (p *pairedImportProvider) SearchTracks(_ context.Context, _ provider.Query) ([]provider.Candidate, error) {
	p.calls.Add(1)
	return []provider.Candidate{p.candidate}, nil
}
func TestSubmitImportPairedCSVTSVIdentityAndProblems(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	base, _ := a.registry.Get(provider.MusicBrainz)
	c := fullCandidate("paired-track")
	c.Title, c.ArtistName, c.AlbumTitle, c.AlbumCredit = strPtr("Song"), strPtr("Artist"), strPtr("Album"), strPtr("Artist")
	disc, position, duration, year := 2, 3, 620000, 2020
	c.DiscNumber, c.TrackNumber, c.DurationMS, c.Year = &disc, &position, &duration, &year
	p := &pairedImportProvider{MetadataProvider: base, candidate: c}
	a.registry.Register(p)
	a.rebuildRunner(a.settings.Get())
	var batches []int64
	for index, filename := range []string{"original.csv", "aliases.tsv"} {
		path := filepath.Join("..", "..", "testdata", "track-identity", filename)
		delim := ","
		if index == 1 {
			delim = "\t"
		}
		batch, err := a.SubmitImport(path, "", []string{"title", "artist", "album", "duration", "disc_no", "track_no", "year", ""}, "UTF-8", delim, true)
		if err != nil {
			t.Fatal(err)
		}
		batches = append(batches, batch)
		waitFor(t, "paired import finished", func() bool { return !a.runner.IsRunning(batch) })
		stats, err := a.store.Stats(batch)
		if err != nil || stats.Total != 2 || stats.Review != 1 || stats.Failed != 0 || stats.Imported != 1-index || stats.ExistingSkipped != index {
			t.Fatal(filename, stats, err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		reader := csv.NewReader(file)
		reader.Comma = []rune(delim)[0]
		originals, err := reader.ReadAll()
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		rows, err := a.store.DB().Query("SELECT raw_row,status,track_id FROM import_item WHERE batch_id=? ORDER BY row_index", batch)
		if err != nil {
			t.Fatal(err)
		}
		i := 1
		for rows.Next() {
			var raw, status string
			var track *int64
			if err := rows.Scan(&raw, &status, &track); err != nil {
				t.Fatal(err)
			}
			var fields []string
			if err := json.Unmarshal([]byte(raw), &fields); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fields, originals[i]) {
				t.Fatal("original row altered", fields, originals[i])
			}
			if i == 1 {
				if track == nil {
					t.Fatal("missing exact target", status)
				}
				var ref string
				err := a.store.DB().QueryRow("SELECT match_ref FROM track WHERE id=?", *track).Scan(&ref)
				if err != nil || ref != c.MatchRef {
					t.Fatal(ref, err)
				}
			} else if track != nil || status != "review" {
				t.Fatal("unmatched row auto resolved", status, track)
			}
			i++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if i != 3 {
			t.Fatal("row missing", i)
		}
		target, err := a.store.FindExactBinding(domain.Identity{Provider: provider.MusicBrainz, Ref: c.MatchRef})
		if err != nil || target == nil || target.Release.Ref != *c.ReleaseRef {
			t.Fatal(target, err)
		}
		if index == 0 {
			// Discard historical terminal input after verifying its original row;
			// the second file must still identify the current collection.
			if _, err := a.store.DB().Exec("DELETE FROM import_item WHERE batch_id=? AND status='imported'", batch); err != nil {
				t.Fatal(err)
			}
		}
	}
	if p.calls.Load() != 7 {
		t.Fatal("unexpected necessary search count", p.calls.Load())
	}
	queue, err := a.GetReviewProblemQueue()
	if err != nil || queue.Problems != (store.ProblemStats{ReviewProblems: 1, PendingProblems: 1, SourceRows: 2}) || len(queue.Items[0].Sources) != 2 {
		t.Fatal(queue, err)
	}
	if n, err := a.store.CountTracks(store.TrackFilter{}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if queue.Items[0].Sources[0].BatchID != batches[0] || queue.Items[0].Sources[1].BatchID != batches[1] {
		t.Fatal("cross-batch provenance lost", queue)
	}
	t.Logf("paired files: new_tracks=1 existing_skips=1 independent_problems=1 source_rows=2 false_auto_actions=0 search_calls=%d", p.calls.Load())
}
