package app

import (
	"errors"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestProblemResearchAppRejectsMemberChangeAndKeepsQuery(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	row := candidateRow()
	row.Title = strPtr("Song")
	one := enqueueOneItem(t, a, "Song", *row.Artist, *row.Album)
	if err := a.store.MarkReview(one, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	scope, err := a.store.ItemProblemScope(one)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := a.registry.Get(provider.MusicBrainz)
	searcher := &delayedReviewSearcher{MetadataProvider: base, started: make(chan struct{}), release: make(chan struct{}), hit: row.toCandidate()}
	a.registry.Register(searcher)
	result := make(chan error, 1)
	go func() {
		_, err := a.ResearchReviewProblem(scope, store.ReviewQuery{Version: store.ReviewProtocolVersion, Title: "Song", Artist: *row.Artist, Album: *row.Album, Provider: "musicbrainz"})
		result <- err
	}()
	select {
	case <-searcher.started:
	case err := <-result:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("search not started")
	}
	batch, err := a.store.CreateBatch(&store.ImportBatch{Filename: "second.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.EnqueueItems(batch, []store.ImportItem{{RawRow: "[]", InTitle: row.Title, InArtist: row.Artist, InAlbum: row.Album}}); err != nil {
		t.Fatal(err)
	}
	two, err := a.store.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.MarkReview(two.ID, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	close(searcher.release)
	if err := <-result; !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("late expanded query accepted", err)
	}
	var query string
	if err := a.store.DB().QueryRow("SELECT query_json FROM review_problem WHERE id=?", scope.ProblemID).Scan(&query); err != nil || query == "" {
		t.Fatal("draft lost", query, err)
	}
	if _, err := a.ResearchReviewItem(one, "Song", *row.Artist, *row.Album, "musicbrainz", ""); err == nil {
		t.Fatal("legacy single-row search silently widened scope")
	}
	current, err := a.GetReviewProblemScope(scope.ProblemID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.NoneMatchedProblem(current); err != nil {
		t.Fatal(err)
	}
	queue, err := a.GetReviewQueue()
	if err != nil || len(queue.Items) != 2 || len(queue.Items[0].Candidates) != 0 {
		t.Fatal(queue, err)
	}
}
