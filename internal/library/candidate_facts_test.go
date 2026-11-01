package library

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func ptr[T any](value T) *T { return &value }

func factFixture(t *testing.T) (*Service, *store.Store, int64, provider.Candidate) {
	t.Helper()
	s, st, _ := newTestService(t)
	c := candidate("release", "track", nil)
	c.ArtistName, c.DiscNumber, c.TrackNumber, c.DurationMS = ptr("Artist"), ptr(2), ptr(3), ptr(620000)
	c.ISRC = ptr("same-isrc")
	input := seed("release", "track", "group", "Album", "Artist")
	input.Snapshot.Track.Title = domain.Known("Song")
	input.Snapshot.Track.Credit = domain.Known(domain.Credit{Text: "Artist"})
	input.Snapshot.Track.DiscNumber, input.Snapshot.Track.TrackNumber = domain.Known(2), domain.Known(3)
	input.Snapshot.Track.DurationMS = domain.Known(620000)
	input.Snapshot.Track.Recording = domain.Known(domain.Identity{Provider: "test", Ref: "recording"})
	input.Snapshot.Release.TracklistCompleteness = domain.Complete
	input.Snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{{DiscNumber: 2, TrackNumber: 3, Track: domain.Known(input.Snapshot.Track.Identity), Title: domain.Known("Song")}})
	result := commit(t, s, prepare(t, s, input), false)
	return s, st, result.TrackID, c
}

func TestCandidateFactsUnifiesExactTargetWithoutOverwritingUserValues(t *testing.T) {
	s, st, id, remote := factFixture(t)
	if err := st.ApplyMetadataEdits(id, map[string]string{"title": "用户改名"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	input := matching.NewInputProfile("Song", "Artist", "Album")
	pool, err := s.CandidateFacts(context.Background(), input, []provider.Candidate{remote})
	if err != nil {
		t.Fatal(err)
	}
	if len(pool.Targets) != 1 || len(pool.Related) != 0 || pool.Truncated {
		t.Fatal(pool)
	}
	target := pool.Targets[0]
	if target.Identity.Track.Ref != "track" || target.Ownership.State != matching.OwnershipOwned || target.Ownership.TrackID != id || len(target.Facts) != 4 || len(target.Conflicts) != 0 {
		t.Fatal(target)
	}
	if result := target.Check(input); result.Eligibility != matching.MetadataEligible {
		t.Fatal(result)
	}
	foundCurrent, foundSnapshot := false, false
	for _, fact := range target.Facts {
		if fact.Kind == "current" {
			foundCurrent = fact.Metadata.Names.Title.Raw == "用户改名"
		}
		if fact.Kind == "snapshot" {
			foundSnapshot = *fact.Metadata.Candidate.Title == "Song" && fact.TrackRevision > 0 && fact.ReleaseRevision > 0
		}
	}
	if !foundCurrent || !foundSnapshot || *remote.Title != "Song" {
		t.Fatal("facts were spliced or mutated", target)
	}
	current, err := st.GetTrack(id)
	if err != nil || current.Title != "用户改名" {
		t.Fatal(current, err)
	}
	// A source snapshot can independently resolve a reliable alias even when
	// all current display fields have been changed.
	localOnly, err := s.CandidateFacts(context.Background(), input, nil)
	if err != nil || len(localOnly.Targets) != 1 || localOnly.Targets[0].Check(input).Eligibility != matching.MetadataEligible {
		t.Fatal(localOnly, err)
	}
}

func TestCandidateFactsReadsExactBindingEvenWithoutAnyTextRecall(t *testing.T) {
	s, st, id, remote := factFixture(t)
	remote.Title, remote.ArtistName, remote.AlbumTitle = ptr("不相交曲名"), ptr("不相交署名"), ptr("不相交专辑")
	input := matching.NewInputProfile(*remote.Title, *remote.ArtistName, *remote.AlbumTitle)
	local, err := st.RecallLocal(context.Background(), []matching.InputProfile{input}, 100)
	if err != nil || len(local[0].Records) != 0 {
		t.Fatal("fixture must not be recalled by text", local, err)
	}
	pool, err := s.CandidateFacts(context.Background(), input, []provider.Candidate{remote})
	if err != nil || len(pool.Targets) != 1 || len(pool.Targets[0].Facts) != 4 {
		t.Fatal(pool, err)
	}
	target := pool.Targets[0]
	if target.Ownership.TrackID != id || len(target.Conflicts) == 0 || target.Check(input).Eligibility != matching.MetadataUnresolved {
		t.Fatal("missing exact local facts concealed conflicts", target)
	}
	rows, err := st.ReadLocalFacts(context.Background(), []domain.Identity{{Provider: "test", Ref: "track"}, {Provider: "other", Ref: "track"}, {Provider: "test", Ref: "track"}})
	if err != nil || len(rows) != 1 || rows[0].TrackID != id {
		t.Fatal("exact lookup conflated identities or duplicated rows", rows, err)
	}
}

func TestCandidateFactsUnboundAndOtherTrackAreNotOwnedBySimilarity(t *testing.T) {
	s, st, id, remote := factFixture(t)
	unbound, err := st.CreateTrack(&store.Track{Title: "Song", Credit: ptr("Artist"), Album: ptr("Album")})
	if err != nil {
		t.Fatal(err)
	}
	other := remote
	other.MatchRef, other.ReleaseRef = "second-track", ptr("second-release")
	pool, err := s.CandidateFacts(context.Background(), matching.NewInputProfile("Song", "Artist", "Album"), []provider.Candidate{remote, other})
	if err != nil || len(pool.Targets) != 2 || len(pool.Related) != 1 || pool.Related[0].TrackID != unbound {
		t.Fatal(pool, err)
	}
	for _, target := range pool.Targets {
		if target.Identity.Track.Ref == "track" && target.Ownership.TrackID != id {
			t.Fatal(target)
		}
		if target.Identity.Track.Ref == "second-track" && (target.Ownership.State != matching.OwnershipUnowned || target.Ownership.TrackID != 0) {
			t.Fatal("recording/ISRC/text became ownership", target)
		}
	}
}

func TestCandidateFactsConflictKeepsBothViewsAndReasons(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*provider.Candidate)
	}{
		{"title", func(c *provider.Candidate) { c.Title = ptr("Different Song") }},
		{"artist", func(c *provider.Candidate) { c.ArtistName = ptr("Different Artist") }},
		{"album", func(c *provider.Candidate) { c.AlbumTitle = ptr("Different Album") }},
		{"track", func(c *provider.Candidate) { c.TrackNumber = ptr(4) }},
		{"recording", func(c *provider.Candidate) { c.RecordingRef = ptr("other-recording") }},
		{"release", func(c *provider.Candidate) { c.ReleaseRef = ptr("other-release") }},
	} {
		t.Run(change.name, func(t *testing.T) {
			s, _, _, remote := factFixture(t)
			change.apply(&remote)
			before, _ := json.Marshal(remote)
			input := matching.NewInputProfile("Song", "Artist", "Album")
			pool, err := s.CandidateFacts(context.Background(), input, []provider.Candidate{remote})
			if err != nil || len(pool.Targets) != 1 {
				t.Fatal(pool, err)
			}
			target := pool.Targets[0]
			found := false
			for _, conflict := range target.Conflicts {
				if conflict.Field == change.name && conflict.LeftSource != "" && conflict.RightSource != "" && conflict.LeftValue != conflict.RightValue {
					found = true
				}
			}
			if !found || target.Check(input).Eligibility != matching.MetadataUnresolved {
				t.Fatal("conflict hidden", target)
			}
			if change.name == "release" && (target.Ownership.State != matching.OwnershipConflict || target.Ownership.TrackID != 0) {
				t.Fatal("release conflict owned", target)
			}
			after, _ := json.Marshal(remote)
			if string(before) != string(after) {
				t.Fatal("caller candidate changed")
			}
		})
	}
}

func TestCandidateFactsDoesNotBuildMatchFromComplementaryFragments(t *testing.T) {
	s, _, _, remote := factFixture(t)
	// Neither remote view has both reliable title and support. Even though
	// fields would form a perfect match if combined, they must stay unresolved.
	one, two := remote, remote
	one.ArtistName, one.AlbumTitle = nil, nil
	two.Title = nil
	sources, err := s.identitySources()
	if err != nil {
		t.Fatal(err)
	}
	pool := matching.BuildCandidatePool(matching.LocalRecallResult{Truncated: true}, []provider.Candidate{one, two}, sources, s.store)
	if len(pool.Targets) != 1 || !pool.Truncated || len(pool.Targets[0].Facts) != 2 {
		t.Fatal(pool)
	}
	input := matching.NewInputProfile("Song", "Artist", "Album")
	if result := pool.Targets[0].Check(input); result.Eligibility != matching.MetadataUnresolved {
		t.Fatal("spliced fragments authorized a match", result)
	}
	if one.AlbumTitle != nil || two.Title != nil {
		t.Fatal("source facts modified")
	}
}
