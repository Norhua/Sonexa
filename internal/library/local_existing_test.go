package library

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestLocalExistingCommitsOnlyExactSkipAndDoesNotWriteLibrary(t *testing.T) {
	for _, inputName := range []string{"Song", "用户曲名"} {
		t.Run(inputName, func(t *testing.T) {
			s, st, id, _ := factFixture(t)
			if err := st.ApplyMetadataEdits(id, map[string]string{"title": "用户曲名"}, nil, nil); err != nil {
				t.Fatal(err)
			}
			item := queueInput(t, st)
			input := matching.NewInputProfile(inputName, "Artist", "Album")
			// Derived index rebuild is a read-side preparation, not a skip write.
			if _, err := s.CandidateFacts(context.Background(), input, nil); err != nil {
				t.Fatal(err)
			}
			before := libraryTables(t, st)
			selected, err := s.PrepareLocalExisting(context.Background(), input, item.ID, item.Revision)
			if err != nil || selected == nil || selected.Candidate.MatchRef != "track" || selected.Plan.Seed.Operation != domain.AdmissionSkipExisting {
				t.Fatal(selected, err)
			}
			if selected.Plan.Seed.Snapshot.Track.Title.State == domain.Value {
				t.Fatal("skip seed copied current name into upstream facts", selected.Plan)
			}
			result := commit(t, s, selected.Plan, false)
			assertExistingInput(t, st, item, id)
			if !result.Existing || libraryTables(t, st) != before {
				t.Fatal("local skip mutated collection", result)
			}
		})
	}
}

func TestLocalExistingRejectsStaleOrCancelledScope(t *testing.T) {
	for _, mutation := range []string{"cancel", "input", "delete", "rebind", "release", "commit_failure"} {
		t.Run(mutation, func(t *testing.T) {
			s, st, id, _ := factFixture(t)
			item := queueInput(t, st)
			input := matching.NewInputProfile("Song", "Artist", "Album")
			ctx := context.Background()
			if mutation == "cancel" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				if selected, err := s.PrepareLocalExisting(cancelled, input, item.ID, item.Revision); selected != nil || !errors.Is(err, context.Canceled) {
					t.Fatal(selected, err)
				}
				return
			}
			selected, err := s.PrepareLocalExisting(ctx, input, item.ID, item.Revision)
			if err != nil || selected == nil {
				t.Fatal(selected, err)
			}
			switch mutation {
			case "input":
				err = st.MarkReviewAtRevision(item.ID, item.Revision, store.ReviewAmbiguous, `[]`, `[]`)
			case "delete":
				err = st.DeleteTrack(id)
			case "rebind":
				err = st.SetProvenance(id, "test", "different-track")
			case "release":
				identity := selected.Plan.Seed.Snapshot.Release.Identity
				release, readErr := st.GetReleaseByRef(string(identity.Provider), identity.Ref)
				if readErr != nil || release == nil {
					t.Fatal("skip target release missing", release, readErr)
				}
				result, updateErr := st.DB().Exec("UPDATE release SET revision=revision+1 WHERE id=?", release.ID)
				if updateErr != nil {
					t.Fatal(updateErr)
				}
				if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
					t.Fatal("fixture did not change exactly one release", count, countErr)
				}
			case "commit_failure":
				_, err = st.DB().Exec(`CREATE TRIGGER fail_local_skip BEFORE INSERT ON operation_result BEGIN SELECT RAISE(ABORT,'forced failure'); END`)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := st.GetItem(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.CommitAdmission(ctx, selected.Plan.ID, selected.Plan.Hash, domain.AdmissionApproval{}); err == nil {
				t.Fatal("stale/failed skip succeeded")
			}
			after, err := st.GetItem(item.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("failed local skip changed input", after, err)
			}
		})
	}
}

func TestLocalExistingUnknownPositionsAndExplicitConstraints(t *testing.T) {
	for _, scenario := range []string{"unknown_competitor", "partial", "position", "duration", "version", "deluxe"} {
		t.Run(scenario, func(t *testing.T) {
			s, st, _, c := factFixture(t)
			item := queueInput(t, st)
			input := matching.ProfileFromCandidate(c)
			release, err := st.GetReleaseByRef("test", "release")
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := st.GetReleaseSnapshot(release.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "unknown_competitor":
				input.Disc = matching.MissingNumber("number", "disc")
				input.Track = matching.MissingNumber("number", "track")
				input.Duration = matching.MissingNumber("ms", "duration")
				snapshot.Tracklist.Value = append(snapshot.Tracklist.Value, domain.TracklistEntry{Track: domain.Known(domain.Identity{Provider: "test", Ref: "unknown"}), DiscNumber: 2, TrackNumber: 4})
			case "partial":
				snapshot.TracklistCompleteness = domain.Partial
			case "position":
				input.Track.Value = 4
			case "duration":
				input.Duration.Value += 10001
			case "version":
				input.Title = matching.ParseMetadataText("Song (Off Vocal)", matching.MetadataTitle)
			case "deluxe":
				input.Album = matching.ParseMetadataText("Album (Deluxe Edition)", matching.MetadataAlbum)
			}
			if scenario == "partial" {
				// Model a genuinely partial snapshot, not a partial patch merged
				// into an already complete snapshot by the normal sync rules.
				if _, err := st.DB().Exec("UPDATE release_snapshot SET body=json_set(body,'$.tracklistCompleteness','partial'),revision=revision+1 WHERE release_id=?", release.ID); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "unknown_competitor" {
				rev, err := st.ReleaseRevision(release.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err := st.ApplyReleaseSnapshot(release.ID, &domain.Snapshot{Release: snapshot}, rev); err != nil {
					t.Fatal(err)
				}
			}
			selected, err := s.PrepareLocalExisting(context.Background(), input, item.ID, item.Revision)
			if err != nil || selected != nil {
				t.Fatal("insufficient/conflicting facts skipped", selected, err)
			}
		})
	}
}

func TestLocalExistingCurrentAlbumCannotHideRepeatedPosition(t *testing.T) {
	s, st, id, _ := factFixture(t)
	track, err := st.GetTrack(id)
	if err != nil || track.ReleaseID == nil {
		t.Fatal(track, err)
	}
	release, err := st.GetReleaseSnapshot(*track.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	// The other position uses a different textual alias but the same recording.
	release.Tracklist.Value = append(release.Tracklist.Value, domain.TracklistEntry{Track: domain.Known(domain.Identity{Provider: "test", Ref: "repeat"}), DiscNumber: 2, TrackNumber: 4, Title: domain.Known("Another name"), Recording: domain.Known(domain.Identity{Provider: "test", Ref: "recording"})})
	revision, err := st.ReleaseRevision(*track.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ApplyReleaseSnapshot(*track.ReleaseID, &domain.Snapshot{Release: release}, revision); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateReleaseFields(*track.ReleaseID, map[string]string{"title": "用户专辑"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	item := queueInput(t, st)
	selected, err := s.PrepareLocalExisting(context.Background(), matching.NewInputProfile("Song", "Artist", "用户专辑"), item.ID, item.Revision)
	if err != nil || selected != nil {
		t.Fatal("renamed album or recording alias hid repeated position", selected, err)
	}
}

func TestLocalExistingMultipleVersionsUseOnlyCompatibleTargets(t *testing.T) {
	for _, scenario := range []string{"compatible", "explicit_album", "unknown_total", "different_content"} {
		t.Run(scenario, func(t *testing.T) {
			s, st, id, _ := factFixture(t)
			release, err := st.GetReleaseByRef("test", "release")
			if err != nil || release == nil {
				t.Fatal(release, err)
			}
			snapshot, err := st.GetReleaseSnapshot(release.ID)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.ReleaseTrackTotal = domain.Known(12)
			revision, err := st.ReleaseRevision(release.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.ApplyReleaseSnapshot(release.ID, &domain.Snapshot{Release: snapshot}, revision); err != nil {
				t.Fatal(err)
			}
			other := seed("other-release", "other-track", "group", "Album", "Artist")
			other.Snapshot.Track.Title = domain.Known("Song")
			other.Snapshot.Track.Credit = domain.Known(domain.Credit{Text: "Artist"})
			other.Snapshot.Track.Recording = domain.Known(domain.Identity{Provider: "test", Ref: "recording"})
			other.Snapshot.Track.DiscNumber, other.Snapshot.Track.TrackNumber = domain.Known(1), domain.Known(1)
			other.Snapshot.Release.ReleaseTrackTotal = domain.Known(15)
			if scenario == "unknown_total" {
				other.Snapshot.Release.ReleaseTrackTotal = domain.Missing[int]()
			}
			if scenario == "different_content" {
				other.Snapshot.Track.Recording = domain.Known(domain.Identity{Provider: "test", Ref: "another-recording"})
			}
			if scenario == "explicit_album" {
				other.Snapshot.Release.Title = domain.Known("Album (Deluxe Edition)")
			}
			other.Snapshot.Release.TracklistCompleteness = domain.Complete
			other.Snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{{Track: domain.Known(other.Snapshot.Track.Identity), DiscNumber: 1, TrackNumber: 1, Title: other.Snapshot.Track.Title, Credit: other.Snapshot.Track.Credit, Recording: other.Snapshot.Track.Recording}})
			added := commit(t, s, prepare(t, s, other), true)
			item := queueInput(t, st)
			selected, err := s.PrepareLocalExisting(context.Background(), matching.NewInputProfile("Song", "Artist", "Album"), item.ID, item.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "unknown_total" || scenario == "different_content" {
				if selected != nil {
					t.Fatal("unknown total/content ambiguity chose an owned target", selected)
				}
				return
			}
			want := added.TrackID
			if scenario == "explicit_album" {
				want = id
			}
			if selected == nil || selected.Plan.Baseline.ExistingTrackID != want {
				t.Fatal("preference overrode compatible target subset", selected, want)
			}
		})
	}
}

// Mutate a binding after CandidateFacts checked it, but before skip preparation
// obtains its baseline. The newer revision must not refresh old evidence.
type localChangingSource struct {
	*summaryProvider
	calls  int
	change func()
}

func (p *localChangingSource) InspectCandidate(c provider.Candidate) matching.CandidateIdentity {
	p.calls++
	if p.calls == 6 {
		p.change()
	}
	return p.summaryProvider.InspectCandidate(c)
}
func TestLocalExistingRejectsBindingChangeDuringPreparation(t *testing.T) {
	s, st, id, _ := factFixture(t)
	item := queueInput(t, st)
	p := &localChangingSource{summaryProvider: &summaryProvider{}, change: func() {
		if err := st.SetProvenance(id, "test", "track"); err != nil {
			t.Fatal(err)
		}
	}}
	sources, err := matching.NewCandidateSources(p)
	if err != nil {
		t.Fatal(err)
	}
	s.SetCandidateSources(sources)
	selected, err := s.PrepareLocalExisting(context.Background(), matching.NewInputProfile("Song", "Artist", "Album"), item.ID, item.Revision)
	if selected != nil || !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("preparation silently upgraded stale evidence", selected, err, p.calls)
	}
}
