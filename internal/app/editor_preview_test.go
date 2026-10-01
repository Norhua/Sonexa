package app

import (
	"encoding/json"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
	"reflect"
	"testing"
)

func TestEditorOfflineAndUpstreamPreviewNeverWriteEntities(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	candidate := snowCandidate()
	id, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: candidate}))
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.store.ReadEditorSession(id)
	if err != nil {
		t.Fatal(err)
	}
	requests := replayEnrichment(t, a, enrichmentRelease)
	session, err := a.GetEditSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 || session.Editor.TrackID != id || len(session.Providers) != 3 {
		t.Fatalf("offline session: %+v %v", session, requests.Load())
	}
	if session.Editor.Current["track.title"].ValueJSON != `"綿雪の夢"` || session.Editor.Current["track.trackNumber"].ValueJSON != "4" {
		t.Fatal(session.Editor.Current)
	}
	rows, err := a.SearchEditCandidates(id, "musicbrainz", "綿雪の夢", "ハトリアヤ", "狼と巫女とお菓子の家", "")
	if err != nil || len(rows) == 0 {
		t.Fatalf("search: %v %v", rows, err)
	}
	selected, err := a.PreviewEditCandidate(rows[0])
	if err != nil || selected.ID == "" {
		t.Fatalf("candidate: %+v %v", selected, err)
	}
	preview, err := a.LoadEditUpstream("musicbrainz", *candidate.ReleaseRef, candidate.MatchRef, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Fields["track.title"].ValueJSON != `"綿雪の夢"` || preview.Fields["track.roles.vocal"].State != domain.Value {
		t.Fatal(preview.Fields)
	}
	if requests.Load() != 1 {
		t.Fatal("preview did not fetch exact target", requests.Load())
	}
	after, err := a.store.ReadEditorSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("preview changed entities\nbefore=%+v\nafter=%+v", before, after)
	}
	if _, err := a.LoadEditUpstream("musicbrainz", *candidate.ReleaseRef, "bad-ref", "", true); err == nil {
		t.Fatal("invalid binding accepted")
	}
	if n, _ := a.store.CountTracks(store.TrackFilter{}); n != 1 {
		t.Fatal("preview created collection")
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	var decoded domain.EditPreview
	if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(*preview, decoded) {
		t.Fatalf("stable DTO roundtrip: %v", err)
	}
}

func TestEditorLayerCountsAndEmptyLocalTrack(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	c := fullCandidate("one")
	id, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c}))
	if err != nil {
		t.Fatal(err)
	}
	c.MatchRef = "two"
	if _, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c})); err != nil {
		t.Fatal(err)
	}
	c.MatchRef = "other"
	c.ReleaseRef = strPtrOrNil("other-release")
	plan, err := a.PrepareAdmission(rowFrom(matching.ScoredCandidate{Candidate: c}), 0)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := a.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitAdmission(plan.ID, plan.Hash, *approval); err != nil {
		t.Fatal(err)
	}
	session, err := a.GetEditSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if session.Editor.OtherReleaseTracks != 1 || session.Editor.OtherGroupTracks != 1 || len(session.Editor.ReleaseTrackIDs) != 2 {
		t.Fatalf("counts: %+v", session.Editor)
	}
	empty, err := a.CreateTrackManual("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	local, err := a.GetEditSession(empty)
	if err != nil {
		t.Fatal(err)
	}
	if local.Editor.ReleaseID != 0 || local.Editor.TrackIdentity != nil || local.Editor.Current["track.title"].State != domain.Empty {
		t.Fatal(local.Editor)
	}
}

func TestEditorBindingAndSharedImpactRemainDraftOnly(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	c := fullCandidate("first")
	id, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c}))
	if err != nil {
		t.Fatal(err)
	}
	c.MatchRef = "second"
	second, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c}))
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.store.ReadEditorSession(id)
	if err != nil {
		t.Fatal(err)
	}
	draft := domain.EditDraft{TrackID: id, Revisions: before.Revisions, BindingAction: "clear_track"}
	impact, err := a.PreviewEditSave(draft)
	if err != nil || impact.NeedsScope || impact.TargetReleaseID != before.ReleaseID || !impact.DetachesTrack {
		t.Fatalf("clear song: %+v %v", impact, err)
	}
	draft.BindingAction = "clear_release"
	impact, err = a.PreviewEditSave(draft)
	if err != nil || !impact.NeedsScope {
		t.Fatalf("shared: %+v %v", impact, err)
	}
	draft.Scope = "all"
	impact, err = a.PreviewEditSave(draft)
	if err != nil || !reflect.DeepEqual(impact.SourceTrackIDs, []int64{id, second}) || impact.CreatesRelease {
		t.Fatalf("whole release: %+v %v", impact, err)
	}
	draft.Scope = "single"
	impact, err = a.PreviewEditSave(draft)
	if err != nil || !impact.CreatesRelease || impact.Admission == nil || !impact.Admission.RequiresConfirmation {
		t.Fatalf("partial detach: %+v %v", impact, err)
	}
	draft.BindingAction = "keep"
	draft.Fields = domain.EditFields{"release.credit": {State: domain.Value, ValueJSON: `"新专辑署名"`}}
	draft.Scope = ""
	impact, err = a.PreviewEditSave(draft)
	if err != nil || !impact.NeedsScope {
		t.Fatalf("album credit bypassed scope: %+v %v", impact, err)
	}
	draft.Fields = domain.EditFields{"track.year": {State: domain.Value, ValueJSON: `0`}}
	if _, err := a.PreviewEditSave(draft); err == nil {
		t.Fatal("invalid number accepted")
	}
	draft.Fields = domain.EditFields{"release.country": {State: domain.Value, ValueJSON: `"JP"`}}
	if _, err := a.PreviewEditSave(draft); err == nil {
		t.Fatal("read-only fact editable")
	}
	after, err := a.store.ReadEditorSession(id)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("preview wrote data: %v", err)
	}
}

func TestEditorSingleTrackStillPreviewsAdmissionAndProtectsExistingTarget(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	c := fullCandidate("source")
	id, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c}))
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.store.ReadEditorSession(id)
	if err != nil {
		t.Fatal(err)
	}
	c.MatchRef = "target-song"
	c.ReleaseRef = strPtrOrNil("target-release")
	preview, err := a.PreviewEditCandidate(rowFrom(matching.ScoredCandidate{Candidate: c}))
	if err != nil {
		t.Fatal(err)
	}
	draft := domain.EditDraft{TrackID: id, Revisions: before.Revisions, BindingAction: "set", PreviewID: preview.ID}
	impact, err := a.PreviewEditSave(draft)
	if err != nil || impact.NeedsScope || !impact.CreatesRelease || impact.Admission == nil || !impact.Admission.RequiresConfirmation {
		t.Fatalf("single skipped membership: %+v %v", impact, err)
	}
	plan, err := a.PrepareAdmission(rowFrom(matching.ScoredCandidate{Candidate: c}), 0)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := a.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	target, err := a.CommitAdmission(plan.ID, plan.Hash, *approval)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.UpdateReleaseFields(target.ReleaseID, map[string]string{"title": "目标用户标题"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	// 新增组成员会推进源组修订，必须重新打开基线。
	before, err = a.store.ReadEditorSession(id)
	if err != nil {
		t.Fatal(err)
	}
	draft.Revisions = before.Revisions
	impact, err = a.PreviewEditSave(draft)
	if err != nil || impact.TargetReleaseID != target.ReleaseID || !impact.PreservesTargetValues || len(impact.TargetTrackIDs) != 1 {
		t.Fatalf("existing target: %+v %v", impact, err)
	}
	after, err := a.store.GetRelease(target.ReleaseID)
	if err != nil || after.Title != "目标用户标题" {
		t.Fatal("preview changed target", err)
	}
	local, err := a.CreateTrackManual("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	base, err := a.store.ReadEditorSession(local)
	if err != nil {
		t.Fatal(err)
	}
	impact, err = a.PreviewEditSave(domain.EditDraft{TrackID: local, Revisions: base.Revisions, Fields: domain.EditFields{"release.primaryType": {State: domain.Value, ValueJSON: `"album"`}}})
	if err != nil || !impact.CreatesRelease {
		t.Fatalf("unnamed release: %+v %v", impact, err)
	}
}
