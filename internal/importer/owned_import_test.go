package importer

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestOwnedTetraAlbumAbbreviationSkips(t *testing.T) {
	st := openStore(t)
	c := mbCandidate("tetra-track", "Tetra.", "原田ひとみ", "Trinoline Original Soundtrack ~LINES~")
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Tetra.": {c}}}
	runner := newRunner(st, searcher)
	// A search-page admission has no old import relation to help the first test.
	p, err := runner.library.PrepareCandidate(context.Background(), c, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runner.library.CommitAdmission(context.Background(), p.ID, p.Hash, domain.AdmissionApproval{}); err != nil {
		t.Fatal(err)
	}
	b := enqueueOne(t, st, "Tetra.", "原田瞳（原田ひとみ）", "LINES")
	runToCompletion(t, runner, b)
	stats, _ := st.Stats(b)
	if stats.Skipped != 1 || stats.Review != 0 {
		t.Fatal(stats)
	}
	if searcher.queryCount() == 0 {
		t.Fatal("abbreviation must be corroborated by exact upstream candidate")
	}
	// Learned input makes the next import entirely offline.
	before := searcher.queryCount()
	b = enqueueOne(t, st, "Tetra.", "原田瞳（原田ひとみ）", "LINES")
	runToCompletion(t, runner, b)
	stats, _ = st.Stats(b)
	if stats.Skipped != 1 || searcher.queryCount() != before {
		t.Fatal(stats, searcher.queryCount(), before)
	}
}

func TestOwnedOtherProviderExcludedBeforeDefaultSearch(t *testing.T) {
	st := openStore(t)
	source, ref := "qq", "qq-tsubasa"
	_, err := st.CreateTrackWithProvenance(store.NewTrackInput{Title: "ツバサ (羽翼)", Credit: sptr("4円"), Album: sptr("side-c"), MatchSource: &source, MatchRef: &ref})
	if err != nil {
		t.Fatal(err)
	}
	searcher := &recordingSearcher{failFor: map[string]error{"ツバサ": errors.New("default source must not be called")}}
	b := enqueueOne(t, st, "ツバサ（羽翼）", "4円（ヨエン）", "side-c")
	runToCompletion(t, newRunner(st, searcher), b)
	stats, _ := st.Stats(b)
	if stats.Skipped != 1 || stats.Review != 0 || searcher.queryCount() != 0 {
		t.Fatal(stats, searcher.queryCount())
	}
}

func TestOwnedMultiplePositionsAreNotResolvedBySingleOwnedHit(t *testing.T) {
	st := openStore(t)
	c := mbCandidate("one", "Song", "Artist", "Album")
	other := c
	other.MatchRef = "two"
	for _, v := range []provider.Candidate{c, other} {
		if _, err := st.AdmitCandidate(v); err != nil {
			t.Fatal(err)
		}
	}
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Song": {c, other}}}
	b := enqueueOne(t, st, "Song", "Artist", "Album")
	runToCompletion(t, newRunner(st, searcher), b)
	stats, _ := st.Stats(b)
	if stats.Skipped != 0 || stats.Review != 1 {
		t.Fatal(stats)
	}
}
