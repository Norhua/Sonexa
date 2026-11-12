package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func c4Candidate(ref, title, artist, album, release, group, recording string, total int) provider.Candidate {
	return provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: ref,
		Title: sptr(title), ArtistName: sptr(artist), AlbumTitle: sptr(album), AlbumCredit: sptr(artist),
		ReleaseRef: sptr(release), ReleaseGroupRef: sptr(group), RecordingRef: sptr(recording),
		ReleaseTrackTotal: &total, ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
	}
}

func c4Enqueue(t *testing.T, st *store.Store, rows [][3]string) int64 {
	t.Helper()
	batchID, err := st.CreateBatch(&store.ImportBatch{TotalRows: len(rows)})
	if err != nil {
		t.Fatal(err)
	}
	var items []store.ImportItem
	for i, r := range rows {
		raw, _ := json.Marshal([]string{r[0], r[1], r[2]})
		title, artist, album := r[0], r[1], r[2]
		it := store.ImportItem{RowIndex: i, RawRow: string(raw), InTitle: &title, InArtist: &artist}
		if album != "" {
			it.InAlbum = &album
		}
		items = append(items, it)
	}
	if err := st.EnqueueItems(batchID, items); err != nil {
		t.Fatal(err)
	}
	return batchID
}

func c4Owned(t *testing.T, st *store.Store, ref, title, artist, album, release, group string) int64 {
	t.Helper()
	id, err := st.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: true,
		Title: title, Credit: sptr(artist), Album: sptr(album), AlbumCredit: sptr(artist),
		MatchSource: sptr("musicbrainz"), MatchRef: sptr(ref),
		Release: &store.ReleaseInput{Provider: sptr("musicbrainz"), ProviderRef: sptr(release), ReleaseGroupRef: sptr(group)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// P3：候选池大小不再决定同一首已拥有曲目是跳过还是复核。
func TestOwnedCorroborationIgnoresPoolSize(t *testing.T) {
	for _, size := range []int{1, 2} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			st := openStore(t)
			c4Owned(t, st, "track-1", "Song", "Artist", "Album", "rel-1", "rg-1")
			pool := []provider.Candidate{
				c4Candidate("track-1", "Song", "Artist", "Album", "rel-1", "rg-1", "rec-1", 10),
				c4Candidate("track-2", "Song", "Artist", "Album", "rel-2", "rg-1", "rec-1", 10),
			}[:size]
			batch := c4Enqueue(t, st, [][3]string{{"Song", "Artist feat. Guest", "Album"}})
			runner := newRunner(st, &recordingSearcher{byTitle: map[string][]provider.Candidate{"Song": pool}})
			t.Cleanup(runner.Close)
			runToCompletion(t, runner, batch)
			stats, _ := st.Stats(batch)
			if stats.Skipped != 1 || stats.Review != 0 {
				t.Fatalf("%+v", stats)
			}
		})
	}
}

// P4：上游只有相邻的另一首时不自动入库。
func TestFuzzySiblingIsNotAutoImported(t *testing.T) {
	st := openStore(t)
	wrong := c4Candidate("wrong", "Interlude 2", "Artist", "Album", "rel-1", "rg-1", "rec-wrong", 10)
	batch := c4Enqueue(t, st, [][3]string{{"Interlude", "Artist", "Album"}})
	runner := newRunner(st, &recordingSearcher{byTitle: map[string][]provider.Candidate{"Interlude 2": {wrong}, "Interlude": {wrong}}})
	t.Cleanup(runner.Close)
	runToCompletion(t, runner, batch)
	stats, _ := st.Stats(batch)
	if id, _ := st.FindByMatchRef("musicbrainz", "wrong"); id != 0 || stats.Imported != 0 {
		t.Fatalf("fuzzy sibling admitted: %+v", stats)
	}
}

// P2：确认同一发行的一首后，其余未手搜的复核条目重新排队并免确认入库。
func TestConfirmedReleaseRequeuesSiblings(t *testing.T) {
	st := openStore(t)
	c4Owned(t, st, "saved", "Existing", "Artist", "Album", "rel-1", "rg-1")
	byTitle := map[string][]provider.Candidate{}
	var rows [][3]string
	var cands []provider.Candidate
	for i := 1; i <= 3; i++ {
		title := fmt.Sprintf("Unique Song Number %c", 'A'+i-1)
		c := c4Candidate(fmt.Sprintf("t%d", i), title, "Artist", "Album", "rel-2", "rg-1", fmt.Sprintf("rec-%d", i), 3)
		byTitle[title] = []provider.Candidate{c}
		cands = append(cands, c)
		rows = append(rows, [3]string{title, "Artist", "Album"})
	}
	// 无关条目：另一个组的新发行，不应被重排。
	other := c4Candidate("o1", "Other Title Here", "Artist", "Elsewhere", "rel-9", "rg-9", "rec-9", 5)
	c4Owned(t, st, "saved9", "Existing 9", "Artist", "Elsewhere", "rel-8", "rg-9")
	byTitle["Other Title Here"] = []provider.Candidate{other}
	rows = append(rows, [3]string{"Other Title Here", "Artist", "Elsewhere"})
	batch := c4Enqueue(t, st, rows)
	runner := newRunner(st, &recordingSearcher{byTitle: byTitle})
	t.Cleanup(runner.Close)
	runToCompletion(t, runner, batch)
	items, _ := st.AllReviewItems()
	if len(items) != 4 {
		t.Fatalf("precondition: %d review items", len(items))
	}
	var first store.ImportItem
	for _, it := range items {
		if *it.InTitle == "Unique Song Number A" {
			first = it
		}
	}
	plan, err := runner.library.PrepareCandidate(context.Background(), cands[0], first.ID)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := runner.library.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.library.CommitAdmission(context.Background(), plan.ID, plan.Hash, *approval)
	if err != nil || len(result.RequeuedBatches) != 1 || result.RequeuedBatches[0] != batch {
		t.Fatal(result, err)
	}
	runToCompletion(t, runner, batch)
	stats, _ := st.Stats(batch)
	if stats.Imported != 3 || stats.Review != 1 {
		t.Fatalf("%+v", stats)
	}
	rest, _ := st.AllReviewItems()
	if len(rest) != 1 || *rest[0].InTitle != "Other Title Here" {
		t.Fatal("unrelated item was touched", rest)
	}
}
