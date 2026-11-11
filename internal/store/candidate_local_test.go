package store

import "testing"

func TestCandidateReleaseAndGroupStatesUseExactIndependentIdentities(t *testing.T) {
	s := openTestStore(t)
	track, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false,
		Title: "Other song", Album: ptr("Album"), AlbumCredit: ptr("Artist"),
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("saved"),
		Release: &ReleaseInput{Provider: ptr("musicbrainz"), ProviderRef: ptr("release"), ReleaseGroupRef: ptr("group")},
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.GetTrackView(track)
	if err != nil {
		t.Fatal(err)
	}
	// A native group may have several source identities. Query the mapping,
	// not the display identity or a title guess.
	release, err := s.GetRelease(*view.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO release_group_ref(group_id,provider,group_ref) VALUES(?,?,?)", *release.ReleaseGroupID, "qq", "other-group"); err != nil {
		t.Fatal(err)
	}
	keys := []CandidateLocalKey{
		{Provider: "musicbrainz", Ref: "new", ReleaseRef: "release", ReleaseGroupRef: "group"},
		{Provider: "musicbrainz", Ref: "new", ReleaseRef: "new-release", ReleaseGroupRef: "group"},
		{Provider: "qq", Ref: "saved", ReleaseRef: "release", ReleaseGroupRef: "group"},
		{Provider: "qq", ReleaseGroupRef: "other-group"},
		{Provider: "musicbrainz", Ref: "saved", ReleaseRef: "release", ReleaseGroupRef: "unknown-group"},
		{Provider: "musicbrainz"},
		{ReleaseRef: "release", ReleaseGroupRef: "group"},
	}
	states, err := s.CandidateLocalStates(keys)
	if err != nil || len(states) != len(keys) {
		t.Fatal(states, err)
	}
	if states[0].ReleaseID != release.ID || states[0].GroupID != *release.ReleaseGroupID || !states[0].ReleaseHasTracks || states[0].TrackID != 0 || states[0].InLibrary {
		t.Fatal("release must not become a track/library match", states[0])
	}
	if states[1].ReleaseID != 0 || states[1].GroupID != *release.ReleaseGroupID || states[1].ReleaseHasTracks {
		t.Fatal("group-only match", states[1])
	}
	if states[2] != (CandidateLocalState{}) || states[3].GroupID != *release.ReleaseGroupID {
		t.Fatal("source isolation or native alias mapping", states)
	}
	if states[4].TrackID != track || states[4].ReleaseID != release.ID || states[4].GroupID != 0 || !states[4].ReleaseHasTracks {
		t.Fatal("unknown incoming group must not borrow the release's group identity", states[4])
	}
	for _, i := range []int{5, 6} {
		if states[i] != (CandidateLocalState{}) {
			t.Fatal("empty identity matched", i, states[i])
		}
	}
	if err := s.DeleteTrack(track); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOrphanRelease(release.ID); err != nil {
		t.Fatal(err)
	}
	states, err = s.CandidateLocalStates(keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state != (CandidateLocalState{}) {
			t.Fatal("deleted structure still marked", state)
		}
	}
}

func TestEmptyReleaseIsNotAutomaticImportPreference(t *testing.T) {
	s := openTestStore(t)
	id, err := s.UpsertRelease(&Release{Provider: ptr("test"), ProviderRef: ptr("empty"), Title: "Album"})
	if err != nil {
		t.Fatal(err)
	}
	states, err := s.CandidateLocalStates([]CandidateLocalKey{{Provider: "test", ReleaseRef: "empty"}})
	if err != nil || len(states) != 1 || states[0].ReleaseID != id || states[0].ReleaseHasTracks {
		t.Fatal(states, err)
	}
}
