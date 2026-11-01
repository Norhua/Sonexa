package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestCandidateActionsKeepSameISRCDifferentTracksIndependent(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	first := candidateRow().toCandidate()
	first.MatchRef = "f0a33d1a-628a-4a53-bc4e-6af077505956"
	first.Title, first.AlbumTitle = sptr("you"), sptr("特典 CD")
	first.ISRC, first.RecordingRef = sptr("JPE301500361"), sptr("2283ceda-78b1-4d20-8f52-bdf73bd53686")
	second := first
	second.MatchRef = "f4458acc-aa40-40b0-a0b7-aaa27df24b04"
	second.ReleaseRef, second.ReleaseGroupRef, second.AlbumTitle = sptr(fixtureMBID("compilation-release")), sptr(fixtureMBID("compilation-group")), sptr("歌曲合集")
	firstID, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: first}))
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.store.GetTrack(firstID)
	if err != nil {
		t.Fatal(err)
	}
	candidates := []matching.ScoredCandidate{{Candidate: first}, {Candidate: second}}
	for _, mode := range []matching.CandidateAction{matching.CandidateAccept, matching.CandidateAdd} {
		rows := a.rowsFrom(candidates, mode)
		want := []matching.CandidateAction{matching.CandidateSkip, matching.CandidateAccept}
		if mode == matching.CandidateAdd {
			want = []matching.CandidateAction{matching.CandidateOwned, matching.CandidateAdd}
		}
		if rows[0].Action != want[0] || rows[1].Action != want[1] || rows[0].Ownership.TrackID != firstID || rows[1].Ownership.State != matching.OwnershipUnowned || rows[1].LocalTrackID != 0 {
			t.Fatalf("wrong independent actions: %+v", rows)
		}
		if len(rows[0].Badges) != 2 || len(rows[1].Badges) != 1 {
			t.Fatal("local badge leaked", rows)
		}
	}
	// Persisted ownership is not trusted; the second candidate remains addable.
	stored := rowFrom(candidates[1])
	stored.LocalTrackID, stored.Action = firstID, matching.CandidateSkip
	stored.Ownership = matching.LocalMatch{State: matching.OwnershipOwned, TrackID: firstID}
	raw, err := json.Marshal([]CandidateRow{stored})
	if err != nil {
		t.Fatal(err)
	}
	restored := a.parseCandidateSnapshot(string(raw))
	if len(restored) != 1 || restored[0].Action != matching.CandidateAccept || restored[0].LocalTrackID != 0 {
		t.Fatal("trusted old ownership", restored)
	}
	itemID := enqueueOneItem(t, a, "you", *second.ArtistName, *second.AlbumTitle)
	plan, err := a.PrepareAdmission(restored[0], itemID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{})
	if err != nil || result.Existing || result.TrackID == firstID {
		t.Fatal("second edition replaced by first", result, err)
	}
	binding, err := a.store.FindExactBinding(domain.Identity{Provider: provider.MusicBrainz, Ref: second.MatchRef})
	if err != nil || binding == nil || binding.TrackID != result.TrackID || binding.Release.Ref != *second.ReleaseRef {
		t.Fatal("wrong selected track", binding, err)
	}
	after, err := a.store.GetTrack(firstID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("first edition modified", after, err)
	}
	if n, err := a.store.CountTracks(store.TrackFilter{}); err != nil || n != 2 {
		t.Fatal(n, err)
	}
}
