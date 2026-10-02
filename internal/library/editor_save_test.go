package library

import (
	"context"
	"encoding/json"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
	"reflect"
	"testing"
)

func TestEditSaveResearchVersionsBothDirections(t *testing.T) {
	for _, pair := range [][2]string{{"0bcd51de-dabe-4106-9118-716f67fef4db", "8c4d9698-bcac-456a-a5ac-1bd90d64c4ff"}, {"bb08c318-b5c2-426e-9c43-58c520ae116c", "d642286a-4ff9-476a-8708-116176151f97"}} {
		for direction := 0; direction < 2; direction++ {
			t.Run(pair[direction], func(t *testing.T) {
				s, st, _ := newTestService(t)
				source, target := researchRelease(t, pair[direction]), researchRelease(t, pair[1-direction])
				ids := []int64{}
				choices := []domain.EditMapping{}
				for _, entry := range source.Release.Tracklist.Value {
					track := trackFromAlbumEntry(entry, *source.Release)
					snapshot := source
					snapshot.Track = &track
					result := commit(t, s, prepare(t, s, domain.AdmissionSeed{Snapshot: snapshot}), true)
					ids = append(ids, result.TrackID)
					indexes, _ := matchAlbumEntries(&track, target.Release.Tracklist.Value)
					if len(indexes) == 1 {
						choices = append(choices, domain.EditMapping{TrackID: result.TrackID, Selected: true, TargetRef: target.Release.Tracklist.Value[indexes[0]].Track.Value.Ref})
					}
				}
				current, _ := st.ReadEditorSession(ids[0])
				oldRelease, _ := st.GetRelease(current.ReleaseID)
				targetTrack := trackFromAlbumEntry(target.Release.Tracklist.Value[0], *target.Release)
				target.Track = &targetTrack
				preview, err := s.storeEditPreview(target)
				if err != nil {
					t.Fatal(err)
				}
				draft := domain.EditDraft{TrackID: ids[0], Revisions: current.Revisions, BindingAction: "set", PreviewID: preview.ID, Mappings: choices}
				plan, err := s.PlanEditSave(context.Background(), draft)
				if err != nil {
					t.Fatal(err)
				}
				approval, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash)
				if err != nil {
					t.Fatal(err)
				}
				result, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, *approval)
				if err != nil {
					t.Fatal(err)
				}
				tracks, err := st.ListTracks(store.TrackFilter{ReleaseID: result.ReleaseID})
				if err != nil || len(tracks) != 14 {
					t.Fatalf("migration: %d %v", len(tracks), err)
				}
				oldAfter, _ := st.GetRelease(current.ReleaseID)
				if !reflect.DeepEqual(oldRelease, oldAfter) {
					t.Fatal("source release changed")
				}
				for _, choice := range choices {
					after, err := st.ReadEditorSession(choice.TrackID)
					if err != nil {
						t.Fatal(err)
					}
					if after.TrackIdentity == nil || after.TrackIdentity.Ref != choice.TargetRef || after.ReleaseID != result.ReleaseID {
						t.Fatal("wrong target binding")
					}
					var snapshot domain.Snapshot
					if err := json.Unmarshal([]byte(after.SnapshotJSON), &snapshot); err != nil {
						t.Fatal(err)
					}
					if snapshot.Track.Release == nil || *snapshot.Track.Release != target.Release.Identity {
						t.Fatal("wrong snapshot release")
					}
				}
			})
		}
	}
}

func TestEditSaveReusesTargetWithoutOverwritingUserValues(t *testing.T) {
	s, st, _ := newTestService(t)
	source := commit(t, s, prepare(t, s, seed("source", "source-song", "g", "Album", "Artist")), true)
	target := commit(t, s, prepare(t, s, seed("target", "owned-song", "g", "Album", "Artist")), true)
	if err := st.UpdateReleaseFields(target.ReleaseID, map[string]string{"title": "用户标题"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ReadEditorSession(source.TrackID)
	snapshot := seed("target", "new-song", "g", "Album", "Artist").Snapshot
	snapshot.Release.TracklistCompleteness = domain.Complete
	snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{{Track: domain.Known(snapshot.Track.Identity), DiscNumber: 1, TrackNumber: 1, Title: domain.Known("new-song")}})
	preview, err := s.storeEditPreview(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	draft := domain.EditDraft{TrackID: source.TrackID, Revisions: before.Revisions, BindingAction: "set", PreviewID: preview.ID, Mappings: []domain.EditMapping{{TrackID: source.TrackID, Selected: true, TargetRef: "new-song"}}, Fields: domain.EditFields{"release.title": {State: domain.Value, ValueJSON: `"不应覆盖"`}}}
	plan, err := s.PlanEditSave(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleaseID != target.ReleaseID {
		t.Fatal("did not reuse target")
	}
	release, _ := st.GetRelease(target.ReleaseID)
	if release.Title != "用户标题" {
		t.Fatal("overwrote target user value")
	}
}

func TestEditSaveRejectsChangedPlanAndConcurrentTarget(t *testing.T) {
	s, st, _ := newTestService(t)
	source := commit(t, s, prepare(t, s, seed("source", "source-song", "g", "Album", "Artist")), true)
	before, _ := st.ReadEditorSession(source.TrackID)
	draft := domain.EditDraft{TrackID: source.TrackID, Revisions: before.Revisions, BindingAction: "clear_release", Mappings: []domain.EditMapping{{TrackID: source.TrackID, Selected: true}}}
	plan, err := s.PlanEditSave(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	changed := *plan
	var content domain.EditSaveContent
	if err := json.Unmarshal([]byte(changed.Seed.EditJSON), &content); err != nil {
		t.Fatal(err)
	}
	content.Album.Rows[0].TrackID = 999
	body, _ := json.Marshal(content)
	changed.Seed.EditJSON = string(body)
	changed.Hash, _ = store.AdmissionHash(changed)
	if _, err := st.CommitEdit(context.Background(), changed, *approval, nil); err == nil {
		t.Fatal("changed sealed payload accepted")
	}
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, *approval); err == nil {
		t.Fatal("ordinary admission accepted edit plan")
	}
	commit(t, s, prepare(t, s, seed("source", "another-song", "g", "Album", "Artist")), true)
	if _, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, *approval); err == nil {
		t.Fatal("membership changed without invalidating edit")
	}
	after, _ := st.ReadEditorSession(source.TrackID)
	if after.ReleaseID != before.ReleaseID || !reflect.DeepEqual(after.Current, before.Current) {
		t.Fatal("failed commit modified source")
	}
}
