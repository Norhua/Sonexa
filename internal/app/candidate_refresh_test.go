package app

import (
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestCandidateRefreshRechecksBindingAndOwnReleaseWithoutSearch(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	var requests atomic.Int32
	a.client.HTTP.Transport = countingTransport{&requests}
	first := candidateRow()
	first.ISRC = sptr("JPE301500361")
	first.Confidence, first.MatchedVariant = 87, "原始查询"
	id, err := a.AddToLibrary(first)
	if err != nil {
		t.Fatal(err)
	}
	track, err := a.store.GetTrack(id)
	if err != nil {
		t.Fatal(err)
	}
	releaseID := *track.ReleaseID
	if _, err := a.coverSvc.Library().ImportLocal(releaseID, coverPNG(t, 900, 600), 0); err != nil {
		t.Fatal(err)
	}
	second := first
	second.MatchRef, second.ReleaseRef = fixtureMBID("other-track"), sptr(fixtureMBID("other-release"))
	second.ReleaseID, second.CoverRevision, second.LocalTrackID = releaseID, 99, id // stale/related IDs must be ignored
	refreshers := []struct {
		name   string
		action matching.CandidateAction
		run    func([]CandidateRow) ([]CandidateRow, error)
	}{
		{"search", matching.CandidateAdd, a.RefreshSearchCandidates},
		{"review", matching.CandidateAccept, a.RefreshReviewCandidates},
	}
	for _, refresh := range refreshers {
		rows, err := refresh.run([]CandidateRow{first, second})
		if err != nil || len(rows) != 2 {
			t.Fatal(rows, err)
		}
		if rows[0].Ownership.State != matching.OwnershipOwned || rows[0].ReleaseID != releaseID || rows[0].CoverRevision != 1 {
			t.Fatal(rows[0])
		}
		if rows[1].Action != refresh.action || rows[1].LocalTrackID != 0 || rows[1].ReleaseID != 0 || rows[1].CoverRevision != 0 {
			t.Fatalf("%s borrowed related cover: %+v", refresh.name, rows[1])
		}
		if !reflect.DeepEqual(rows[1].toCandidate(), second.toCandidate()) || rows[1].Confidence != 87 || rows[1].MatchedVariant != "原始查询" {
			t.Fatal("changed candidate facts", rows[1])
		}
	}
	refs := a.BuildCoverRefs([]CandidateRow{second})
	if len(refs) != 1 || refs[0].Target.ReleaseID != 0 || refs[0].Target.Ref != *second.ReleaseRef {
		t.Fatal("prefetch trusted unrelated local ID", refs)
	}
	// Stored review rows must clear old cover IDs as well, even without explicit refresh.
	raw, err := json.Marshal([]CandidateRow{second})
	if err != nil {
		t.Fatal(err)
	}
	rows := a.parseCandidateSnapshot(string(raw))
	if len(rows) != 1 || rows[0].ReleaseID != 0 || rows[0].Action != matching.CandidateAccept {
		t.Fatal("snapshot kept stale cover", rows)
	}
	// Rebinding a track does not make its old source candidate owned. Its own release may still exist.
	if err := a.store.SetProvenance(id, "musicbrainz", fixtureMBID("rebound-track")); err != nil {
		t.Fatal(err)
	}
	rows, err = a.RefreshReviewCandidates([]CandidateRow{first})
	if err != nil || rows[0].Action != matching.CandidateAccept || rows[0].ReleaseID != releaseID {
		t.Fatal("rebind not reflected", rows, err)
	}
	// Rebinding the release removes the old candidate's local picture association.
	if _, err := a.store.DB().Exec("UPDATE release SET provider_ref=?,revision=revision+1 WHERE id=?", fixtureMBID("rebound-release"), releaseID); err != nil {
		t.Fatal(err)
	}
	first.ReleaseID, first.CoverRevision = releaseID, 1
	rows, err = a.RefreshReviewCandidates([]CandidateRow{first})
	if err != nil || rows[0].ReleaseID != 0 || rows[0].CoverRevision != 0 {
		t.Fatal("release rebind kept stale picture", rows, err)
	}
	if err := a.store.DeleteTrack(id); err != nil {
		t.Fatal(err)
	}
	rows, err = a.RefreshSearchCandidates([]CandidateRow{first})
	if err != nil || rows[0].Action != matching.CandidateAdd || rows[0].ReleaseID != 0 {
		t.Fatal(rows, err)
	}
	if requests.Load() != 0 {
		t.Fatal("local refresh searched upstream", requests.Load())
	}
}

func TestCandidateRefreshReportsDatabaseAndCoverLookupErrors(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	row := candidateRow()
	id, err := a.AddToLibrary(row)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := a.RefreshSearchCandidates([]CandidateRow{row})
	if err != nil || rows[0].LocalTrackID != id {
		t.Fatal(rows, err)
	}
	// Fail only cover lookup: ownership remains exact, but no stale cover is exposed.
	if _, err := a.store.DB().Exec("ALTER TABLE release_cover_selection RENAME TO unavailable_cover_selection"); err != nil {
		t.Fatal(err)
	}
	failed, err := a.RefreshSearchCandidates(rows)
	if err != nil || failed[0].Ownership.State != matching.OwnershipOwned || failed[0].CoverError == "" || failed[0].ReleaseID != 0 {
		t.Fatal("cover error hidden", failed, err)
	}
	if _, err := a.store.DB().Exec("ALTER TABLE unavailable_cover_selection RENAME TO release_cover_selection"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.DB().Exec("ALTER TABLE track RENAME TO unavailable_track"); err != nil {
		t.Fatal(err)
	}
	failed, err = a.RefreshReviewCandidates(rows)
	if err != nil || failed[0].Ownership.State != matching.OwnershipError || failed[0].Action != matching.CandidateUnavailable || failed[0].LocalTrackID != 0 || failed[0].Note == "" {
		t.Fatal("query error became unowned", failed, err)
	}
	if _, err := a.store.DB().Exec("ALTER TABLE unavailable_track RENAME TO track"); err != nil {
		t.Fatal(err)
	}
	recovered, err := a.RefreshReviewCandidates(failed)
	if err != nil || recovered[0].Action != matching.CandidateSkip || recovered[0].Note != "" || recovered[0].CoverError != "" {
		t.Fatal("refresh did not recover", recovered, err)
	}
	if n, err := a.store.CountTracks(store.TrackFilter{}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
