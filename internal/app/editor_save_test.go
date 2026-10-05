package app

import (
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"reflect"
	"testing"
)

func TestEditSaveLocalMigrationIsAtomicAndSelectedOnly(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	c := fullCandidate("save-first")
	first, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c}))
	if err != nil {
		t.Fatal(err)
	}
	c.MatchRef = "save-second"
	second, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c}))
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.store.ReadEditorSession(first)
	if err != nil {
		t.Fatal(err)
	}
	untouched, err := a.store.ReadEditorSession(second)
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	draft := domain.EditDraft{TrackID: first, Revisions: before.Revisions, BindingAction: "clear_release", Fields: domain.EditFields{"track.title": {State: domain.Value, ValueJSON: `"改名"`}, "track.lyrics": {State: domain.Value, ValueJSON: `"[00:01]歌词"`}}, Mappings: []domain.EditMapping{{TrackID: first, Selected: true, DiscNumber: &one, TrackNumber: &one}}}
	plan, err := a.PlanEditSave(draft)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Baseline.RequiresConfirmation {
		t.Fatal("missing member approval")
	}
	if _, err := a.SaveEdit(plan.ID, plan.Hash, domain.AdmissionApproval{}); err == nil {
		t.Fatal("unapproved edit committed")
	}
	check, _ := a.store.ReadEditorSession(first)
	if !reflect.DeepEqual(before, check) {
		t.Fatal("failed save changed source")
	}
	approval, err := a.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.SaveEdit(plan.ID, plan.Hash, *approval)
	if err != nil {
		t.Fatal(err)
	}
	after, err := a.store.ReadEditorSession(first)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReleaseID == before.ReleaseID || after.TrackIdentity != nil || after.ReleaseIdentity != nil || after.Current["track.title"].ValueJSON != `"改名"` || after.Current["track.lyrics"].ValueJSON != `"[00:01]歌词"` {
		t.Fatalf("bad migration: %+v", after)
	}
	unchanged, _ := a.store.ReadEditorSession(second)
	// The group gains a member; source release, track, current values and snapshots stay intact.
	if unchanged.ReleaseID != untouched.ReleaseID || !reflect.DeepEqual(unchanged.Current, untouched.Current) || unchanged.SnapshotJSON != untouched.SnapshotJSON || !reflect.DeepEqual(unchanged.TrackIdentity, untouched.TrackIdentity) {
		t.Fatal("unselected track changed")
	}
	repeat, err := a.SaveEdit(plan.ID, plan.Hash, *approval)
	if err != nil || !reflect.DeepEqual(result, repeat) {
		t.Fatalf("not idempotent: %v", err)
	}
}

func TestEditSaveRejectsEmptyConflictingAndStalePlans(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	id, err := a.CreateTrackManual("旧", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := a.store.ReadEditorSession(id)
	draft := domain.EditDraft{TrackID: id, Revisions: before.Revisions, Fields: domain.EditFields{"release.title": {State: domain.Value, ValueJSON: `"新"`}}}
	if _, err := a.PlanEditSave(draft); err == nil {
		t.Fatal("empty selection accepted")
	}
	draft.Fields = domain.EditFields{"track.title": {State: domain.Empty}}
	plan, err := a.PlanEditSave(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.UpdateTrackFields(id, strPtrOrNil("并发修改"), nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveEdit(plan.ID, plan.Hash, domain.AdmissionApproval{}); err == nil {
		t.Fatal("stale plan accepted")
	}
	after, _ := a.store.ReadEditorSession(id)
	if after.Current["track.title"].ValueJSON != `"并发修改"` {
		t.Fatal("stale save overwrote value")
	}
}

func TestEditSaveCoverFailureAndSuccessfulAtomicFiles(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	id, err := a.CreateTrackManual("旧", nil, strPtrOrNil("专辑"))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := a.store.ReadEditorSession(id)
	draft := domain.EditDraft{TrackID: id, Revisions: before.Revisions, CoverAction: "replace", CoverToken: "missing", Fields: domain.EditFields{"track.title": {State: domain.Value, ValueJSON: `"新"`}, "track.lyrics": {State: domain.Value, ValueJSON: `"歌词"`}}, Mappings: []domain.EditMapping{{TrackID: id, Selected: true}}}
	plan, err := a.PlanEditSave(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveEdit(plan.ID, plan.Hash, domain.AdmissionApproval{}); err == nil {
		t.Fatal("missing cover accepted")
	}
	unchanged, _ := a.store.ReadEditorSession(id)
	if !reflect.DeepEqual(before, unchanged) {
		t.Fatal("file failure partially saved")
	}
	image, err := a.coverSvc.Library().StageArtwork(coverPNG(t, 30, 30), 0)
	if err != nil {
		t.Fatal(err)
	}
	draft.CoverToken = image.Token
	plan, err = a.PlanEditSave(draft)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := a.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.DB().Exec("CREATE TRIGGER fail_edit_result BEFORE INSERT ON operation_result BEGIN SELECT RAISE(ABORT,'failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveEdit(plan.ID, plan.Hash, *approval); err == nil {
		t.Fatal("database failure not propagated")
	}
	unchanged, _ = a.store.ReadEditorSession(id)
	if !reflect.DeepEqual(before, unchanged) {
		t.Fatal("database failure partially saved")
	}
	var covers, lyrics int
	if err := a.store.DB().QueryRow("SELECT count(*) FROM cover_file").Scan(&covers); err != nil {
		t.Fatal(err)
	}
	if err := a.store.DB().QueryRow("SELECT count(*) FROM lyrics_file").Scan(&lyrics); err != nil {
		t.Fatal(err)
	}
	if covers != 0 || lyrics != 0 {
		t.Fatal("failed transaction retained file references")
	}
	if _, err := a.store.DB().Exec("DROP TRIGGER fail_edit_result"); err != nil {
		t.Fatal(err)
	}
	result, err := a.SaveEdit(plan.ID, plan.Hash, *approval)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := a.store.GetCoverSelection(result.ReleaseID)
	if err != nil || selection == nil || selection.Cleared {
		t.Fatalf("cover: %+v %v", selection, err)
	}
	files, err := a.store.ListReleaseCoverFiles(result.ReleaseID)
	if err != nil || len(files) != 1 {
		t.Fatalf("files: %v %v", files, err)
	}
	if _, err := a.coverSvc.Library().Read(files[0]); err != nil {
		t.Fatal(err)
	}
	after, _ := a.store.ReadEditorSession(id)
	if after.Current["track.title"].ValueJSON != `"新"` || after.Current["track.lyrics"].ValueJSON != `"歌词"` {
		t.Fatal("fields and files not saved together")
	}
}

func TestEditSaveKeepsSourceAdoptedCoverForLocalAlbum(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	id, err := a.CreateTrackManual("歌曲", nil, strPtrOrNil("原专辑"))
	if err != nil {
		t.Fatal(err)
	}
	initial, _ := a.store.ReadEditorSession(id)
	if _, err := a.coverSvc.Library().ImportLocal(initial.ReleaseID, coverPNG(t, 24, 24), 0); err != nil {
		t.Fatal(err)
	}
	before, _ := a.store.ReadEditorSession(id)
	oldFiles, _ := a.store.ListReleaseCoverFiles(before.ReleaseID)
	plan, err := a.PlanEditSave(domain.EditDraft{TrackID: id, Revisions: before.Revisions, Fields: domain.EditFields{"release.title": {State: domain.Value, ValueJSON: `"新专辑"`}}, Mappings: []domain.EditMapping{{TrackID: id, Selected: true}}})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := a.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.SaveEdit(plan.ID, plan.Hash, *approval)
	if err != nil {
		t.Fatal(err)
	}
	files, err := a.store.ListReleaseCoverFiles(result.ReleaseID)
	if err != nil || len(files) != 1 {
		t.Fatalf("copied cover: %v %v", files, err)
	}
	if files[0].RelativePath == oldFiles[0].RelativePath || files[0].ContentHash != oldFiles[0].ContentHash {
		t.Fatal("cover not independently copied")
	}
	if _, err := a.coverSvc.Library().Read(oldFiles[0]); err != nil {
		t.Fatal("source cover no longer readable")
	}
}

func TestEditUpdateReplacesAndClearsSharedCoverAtomically(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	first, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: fullCandidate("cover-first")}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: fullCandidate("cover-second")}))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := a.store.ReadEditorSession(first)
	if _, err := a.coverSvc.Library().ImportLocal(before.ReleaseID, coverPNG(t, 24, 24), 0); err != nil {
		t.Fatal(err)
	}
	before, _ = a.store.ReadEditorSession(first)
	draft := domain.EditDraft{TrackID: first, Revisions: before.Revisions, AlbumAction: "update", CoverAction: "replace", CoverToken: "missing", Fields: domain.EditFields{"release.title": {State: domain.Value, ValueJSON: `"更新封面"`}}}
	plan, err := a.PlanEditSave(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveEdit(plan.ID, plan.Hash, domain.AdmissionApproval{}); err == nil {
		t.Fatal("missing cover was accepted")
	}
	unchanged, _ := a.store.ReadEditorSession(first)
	if !reflect.DeepEqual(before, unchanged) {
		t.Fatal("failed replacement partially saved")
	}
	image, err := a.coverSvc.Library().StageArtwork(coverPNG(t, 32, 32), 0)
	if err != nil {
		t.Fatal(err)
	}
	draft.CoverToken = image.Token
	plan, err = a.PlanEditSave(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveEdit(plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil {
		t.Fatal(err)
	}
	after, _ := a.store.ReadEditorSession(first)
	sibling, _ := a.store.ReadEditorSession(second)
	if after.ReleaseID != before.ReleaseID || sibling.ReleaseID != before.ReleaseID || after.CoverRevision <= before.CoverRevision || sibling.CoverRevision != after.CoverRevision || !reflect.DeepEqual(after.TrackIdentity, before.TrackIdentity) || !reflect.DeepEqual(after.ReleaseIdentity, before.ReleaseIdentity) {
		t.Fatal("cover replacement changed binding or failed to update shared revision")
	}
	plan, err = a.PlanEditSave(domain.EditDraft{TrackID: first, Revisions: after.Revisions, AlbumAction: "update", CoverAction: "clear"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveEdit(plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil {
		t.Fatal(err)
	}
	selection, err := a.store.GetCoverSelection(before.ReleaseID)
	if err != nil || selection == nil || !selection.Cleared || selection.Revision <= after.CoverRevision {
		t.Fatalf("shared clear failed: %+v %v", selection, err)
	}
}
