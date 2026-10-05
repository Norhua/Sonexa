package library

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func TestEditAlbumActionWithUnchangedUpstream(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		for _, binding := range []string{"keep", "set"} {
			t.Run(action+"/"+binding, func(t *testing.T) {
				s, st, _ := newTestService(t)
				seed1 := seed("album", "song1", "group", "Album", "Artist")
				first := commit(t, s, prepare(t, s, seed1), true)
				second := commit(t, s, prepare(t, s, seed("album", "song2", "group", "Album", "Artist")), true)
				other := commit(t, s, prepare(t, s, seed("other", "song3", "group", "Album", "Artist")), true)
				before, _ := st.ReadEditorSession(first.TrackID)
				sibling, _ := st.ReadEditorSession(second.TrackID)
				otherBefore, _ := st.ReadEditorSession(other.TrackID)
				draft := domain.EditDraft{TrackID: first.TrackID, Revisions: before.Revisions, AlbumAction: action, BindingAction: binding,
					Fields: domain.EditFields{"release.title": {State: domain.Value, ValueJSON: `"Renamed"`}, "release.credit": {State: domain.Value, ValueJSON: `"New Artist"`}, "track.title": {State: domain.Value, ValueJSON: `"Edited Song"`}}}
				if binding == "set" {
					preview, err := s.storeEditPreview(seed1.Snapshot)
					if err != nil {
						t.Fatal(err)
					}
					draft.PreviewID = preview.ID
				}
				impact, err := s.PreviewEditSave(context.Background(), draft)
				if err != nil || impact.UpdatesRelease != (action == "update") || impact.NeedsAlbumConfirmation != (action == "create") {
					t.Fatalf("impact: %+v %v", impact, err)
				}
				if action == "create" {
					draft.Mappings = []domain.EditMapping{{TrackID: first.TrackID, Selected: true}}
				}
				plan, err := s.PlanEditSave(context.Background(), draft)
				if err != nil {
					t.Fatal(err)
				}
				var approval domain.AdmissionApproval
				if plan.Baseline.RequiresConfirmation {
					approved, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash)
					if err != nil {
						t.Fatal(err)
					}
					approval = *approved
				}
				result, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, approval)
				if err != nil {
					t.Fatal(err)
				}
				after, _ := st.ReadEditorSession(first.TrackID)
				siblingAfter, _ := st.ReadEditorSession(second.TrackID)
				otherAfter, _ := st.ReadEditorSession(other.TrackID)
				if after.Current["release.title"].ValueJSON != `"Renamed"` || after.Current["track.title"].ValueJSON != `"Edited Song"` {
					t.Fatal("draft fields were not saved")
				}
				if !reflect.DeepEqual(otherBefore.Current, otherAfter.Current) || otherBefore.SnapshotJSON != otherAfter.SnapshotJSON || siblingAfter.Current["track.title"] != sibling.Current["track.title"] {
					t.Fatal("unrelated song or other release changed")
				}
				if action == "update" {
					if plan.Baseline.RequiresConfirmation || result.ReleaseID != before.ReleaseID || result.GroupID != before.GroupID || !reflect.DeepEqual(after.TrackIdentity, before.TrackIdentity) || !reflect.DeepEqual(after.ReleaseIdentity, before.ReleaseIdentity) {
						t.Fatal("in-place update changed membership or binding")
					}
					if siblingAfter.ReleaseID != sibling.ReleaseID || siblingAfter.Current["release.title"].ValueJSON != `"Renamed"` || siblingAfter.Current["release.credit"].ValueJSON != `"New Artist"` || siblingAfter.SnapshotJSON != sibling.SnapshotJSON || !reflect.DeepEqual(siblingAfter.TrackIdentity, sibling.TrackIdentity) {
						t.Fatal("shared album update changed sibling song facts")
					}
					if after.Current["release.title"].Mode != domain.User || after.Upstream["release.title"].ValueJSON != `"Album"` {
						t.Fatal("user value did not remain separate from upstream")
					}
					var count int
					if err := st.DB().QueryRow("SELECT count(*) FROM track_fts WHERE track_fts MATCH 'Renamed'").Scan(&count); err != nil || count != 2 {
						t.Fatalf("shared search index: %d %v", count, err)
					}
					// Applying the bound upstream value restores follow mode without detaching.
					plan, err = s.PlanEditSave(context.Background(), domain.EditDraft{TrackID: first.TrackID, Revisions: after.Revisions, AlbumAction: "update", Fields: domain.EditFields{"release.title": before.Upstream["release.title"]}})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil {
						t.Fatal(err)
					}
					after, _ = st.ReadEditorSession(first.TrackID)
					if after.Current["release.title"].Mode != domain.Follow || after.Current["release.title"].Source != "test" {
						t.Fatal("upstream value did not restore follow mode")
					}
				} else if result.ReleaseID == before.ReleaseID || after.TrackIdentity != nil || after.ReleaseIdentity != nil || !reflect.DeepEqual(sibling.Current, siblingAfter.Current) {
					t.Fatal("new local album did not detach only selected song")
				}
			})
		}
	}
}

func TestEditAlbumActionsKeepChangedUpstream(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		t.Run(action, func(t *testing.T) {
			s, st, _ := newTestService(t)
			first := commit(t, s, prepare(t, s, seed("old", "song", "g", "Album", "Artist")), true)
			before, _ := st.ReadEditorSession(first.TrackID)
			snapshot := seed("new", "new-song", "g", "New Album", "Artist").Snapshot
			snapshot.Release.TracklistCompleteness = domain.Complete
			snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{{Track: domain.Known(snapshot.Track.Identity), DiscNumber: 1, TrackNumber: 1}})
			preview, err := s.storeEditPreview(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			draft := domain.EditDraft{TrackID: first.TrackID, Revisions: before.Revisions, AlbumAction: action, BindingAction: "set", PreviewID: preview.ID}
			if _, err := s.PlanEditSave(context.Background(), draft); err == nil {
				t.Fatal("upstream change bypassed explicit migration")
			}
			draft.Mappings = []domain.EditMapping{{TrackID: first.TrackID, Selected: true, TargetRef: "new-song"}}
			plan, err := s.PlanEditSave(context.Background(), draft)
			if action == "update" {
				if err == nil {
					t.Fatal("upstream change accepted in-place update")
				}
				after, _ := st.ReadEditorSession(first.TrackID)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("rejected update changed source")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			approval, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, *approval); err != nil {
				t.Fatal(err)
			}
			after, _ := st.ReadEditorSession(first.TrackID)
			if after.ReleaseIdentity == nil || *after.ReleaseIdentity != snapshot.Release.Identity || after.TrackIdentity == nil || *after.TrackIdentity != snapshot.Track.Identity {
				t.Fatal("changed upstream was detached")
			}
		})
	}
}

func TestEditAlbumUpdateValidatesStructureAndMemberRevisions(t *testing.T) {
	s, st, _ := newTestService(t)
	ids := []int64{}
	for i := 1; i <= 2; i++ {
		item := seed("album", fmt.Sprint(i), "g", "Album", "Artist")
		item.Snapshot.Release.DiscTotal = domain.Known(2)
		item.Snapshot.Track.DiscNumber = domain.Known(i)
		item.Snapshot.Track.TrackNumber = domain.Known(1)
		ids = append(ids, commit(t, s, prepare(t, s, item), true).TrackID)
	}
	before, _ := st.ReadEditorSession(ids[0])
	draft := domain.EditDraft{TrackID: ids[0], Revisions: before.Revisions, AlbumAction: "update", Fields: domain.EditFields{"release.discTotal": {State: domain.Value, ValueJSON: "1"}}}
	if _, err := s.PlanEditSave(context.Background(), draft); err == nil {
		t.Fatal("structure change invalidated a sibling song position")
	}
	draft.Fields = domain.EditFields{"release.title": {State: domain.Value, ValueJSON: `"Renamed"`}}
	plan, err := s.PlanEditSave(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	// A concurrent edit to another member invalidates the shared update plan.
	title := "Concurrent edit"
	if err := st.UpdateTrackFields(ids[1], &title, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); err == nil {
		t.Fatal("stale shared update accepted")
	}
	after, _ := st.ReadEditorSession(ids[0])
	if !reflect.DeepEqual(before.Current, after.Current) {
		t.Fatal("failed update partially saved")
	}
	draft.BindingAction = "clear_release"
	if _, err := s.PlanEditSave(context.Background(), draft); err == nil {
		t.Fatal("in-place update silently ignored explicit unbinding")
	}
	tracks, _ := st.ListTracks(store.TrackFilter{ReleaseID: before.ReleaseID})
	if len(tracks) != 2 {
		t.Fatal("failed update changed membership")
	}
}
