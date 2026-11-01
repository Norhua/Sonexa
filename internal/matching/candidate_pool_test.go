package matching

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

func poolFixture(t *testing.T) (*CandidateSources, *fakeLookup, LocalRecord, provider.Candidate) {
	t.Helper()
	sources, err := NewCandidateSources(identityFake{id: "one", state: IdentityExact}, identityFake{id: "two", state: IdentityExact}, identityFake{id: "unresolved", state: IdentityUnresolved})
	if err != nil {
		t.Fatal(err)
	}
	disc, position, duration := 2, 3, 620000
	c := provider.Candidate{Provider: "one", MatchRef: "track", ReleaseRef: sptr("release"), Title: sptr("Song"), ArtistName: sptr("Artist"), AlbumTitle: sptr("Album"), DiscNumber: &disc, TrackNumber: &position, DurationMS: &duration}
	track, release := domain.Identity{Provider: "one", Ref: "track"}, domain.Identity{Provider: "one", Ref: "release"}
	binding := LocalBinding{TrackID: 42, Track: track, Release: release, Revision: 3, ReleaseRevision: 5}
	lookup := &fakeLookup{bindings: map[domain.Identity]LocalBinding{track: binding}}
	r := LocalRecord{TrackID: 42, TrackIdentity: &track, ReleaseIdentity: &release, TrackRevision: 3, ReleaseRevision: 5, TrackSnapshotRevision: 7, ReleaseSnapshotRevision: 9,
		Current:         LocalCurrentFacts{Profile: ProfileFromCandidate(c)},
		TrackSnapshot:   &domain.TrackSnapshot{Identity: track, Release: &release, Title: domain.Known("Song"), Credit: domain.Known(domain.Credit{Text: "Artist"}), DiscNumber: domain.Known(2), TrackNumber: domain.Known(3), DurationMS: domain.Known(620000)},
		ReleaseSnapshot: &domain.ReleaseSnapshot{Identity: release, Title: domain.Known("Album"), TracklistCompleteness: domain.Complete, Tracklist: domain.Known([]domain.TracklistEntry{{Track: domain.Known(track), DiscNumber: 2, TrackNumber: 3, Title: domain.Known("Song"), Credit: domain.Known(domain.Credit{Text: "Artist"}), DurationMS: domain.Known(620000)}})},
	}
	r.Current.Profile.Duration = MissingNumber("ms", "duration")
	return sources, lookup, r, c
}

func TestCandidatePoolSourceIdentityAndUnresolvedSeeds(t *testing.T) {
	sources, lookup, _, c := poolFixture(t)
	other := c
	other.Provider = "two"
	seed := c
	seed.Provider = "unresolved"
	pool := BuildCandidatePool(LocalRecallResult{Truncated: true}, []provider.Candidate{c, c, other, seed, seed}, sources, lookup)
	if len(pool.Targets) != 4 || !pool.Truncated || len(pool.Targets[0].Facts) != 2 || lookup.calls != 2 {
		t.Fatal(pool, lookup.calls)
	}
	if pool.Targets[0].Ownership.State != OwnershipOwned || pool.Targets[1].Ownership.State != OwnershipUnowned {
		t.Fatal(pool)
	}
	for _, target := range pool.Targets[2:] {
		if target.Identity.State != IdentityUnresolved || target.Check(NewInputProfile("Song", "Artist", "Album")).Eligibility != MetadataUnresolved {
			t.Fatal(target)
		}
	}
}

func TestCandidatePoolRechecksStaleLocalFactsAndErrors(t *testing.T) {
	for _, scenario := range []string{"deleted", "revision", "release", "query_error", "stale_track", "stale_release"} {
		t.Run(scenario, func(t *testing.T) {
			sources, lookup, record, c := poolFixture(t)
			switch scenario {
			case "deleted":
				lookup.bindings = nil
			case "revision":
				binding := lookup.bindings[*record.TrackIdentity]
				binding.Revision++
				lookup.bindings[*record.TrackIdentity] = binding
			case "release":
				binding := lookup.bindings[*record.TrackIdentity]
				binding.Release.Ref = "changed"
				lookup.bindings[*record.TrackIdentity] = binding
			case "query_error":
				lookup.err = errors.New("database failed")
			case "stale_track":
				record.TrackSnapshot.Identity.Ref = "old-track"
			case "stale_release":
				record.ReleaseSnapshot.Identity.Ref = "old-release"
			}
			pool := BuildCandidatePool(LocalRecallResult{Records: []LocalRecord{record}}, []provider.Candidate{c}, sources, lookup)
			if len(pool.Targets) != 1 {
				t.Fatal(pool)
			}
			target := pool.Targets[0]
			if target.Check(NewInputProfile("Song", "Artist", "Album")).Eligibility != MetadataUnresolved || len(target.Issues) == 0 {
				t.Fatal("stale evidence accepted", target)
			}
			if (scenario == "deleted" || scenario == "release" || scenario == "query_error") && target.Ownership.TrackID != 0 {
				t.Fatal("stale local id retained", target)
			}
			if scenario == "query_error" && target.Ownership.State != OwnershipError {
				t.Fatal(target)
			}
		})
	}
}

func TestCandidatePoolRetainsVersionRoleAndPositionConflicts(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*provider.Candidate)
	}{
		{"remix", func(c *provider.Candidate) { c.Title = sptr("Song (B Remix)") }},
		{"live", func(c *provider.Candidate) { c.Title = sptr("Song (Live at Osaka)") }},
		{"deluxe", func(c *provider.Candidate) { c.AlbumTitle = sptr("Album (Deluxe Edition)") }},
		{"character", func(c *provider.Candidate) { c.ArtistName = sptr("角色 B(CV.X)") }},
		{"ensemble", func(c *provider.Candidate) { c.ArtistName = sptr("Artist feat. B") }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			sources, lookup, _, a := poolFixture(t)
			switch scenario.name {
			case "remix":
				a.Title = sptr("Song (A Remix)")
			case "live":
				a.Title = sptr("Song (Live at Tokyo)")
			case "character":
				a.ArtistName = sptr("角色 A(CV.X)")
			case "ensemble":
				a.ArtistName = sptr("Artist feat. A")
			}
			b := a
			scenario.change(&b)
			for _, rows := range [][]provider.Candidate{{a, b}, {b, a}} {
				pool := BuildCandidatePool(LocalRecallResult{}, rows, sources, lookup)
				if len(pool.Targets) != 1 || len(pool.Targets[0].Conflicts) == 0 || pool.Targets[0].Check(ProfileFromCandidate(a)).Eligibility != MetadataUnresolved {
					t.Fatal(pool)
				}
				conflict := pool.Targets[0].Conflicts[0]
				if conflict.LeftValue == conflict.RightValue || conflict.LeftSource == conflict.RightSource {
					t.Fatal(conflict)
				}
			}
		})
	}
}

func TestCandidatePoolCurrentNamesDoNotEraseSourceVersion(t *testing.T) {
	sources, lookup, record, _ := poolFixture(t)
	record.TrackSnapshot.Title = domain.Known("Song (Off Vocal)")
	record.ReleaseSnapshot.Tracklist.Value[0].Title = domain.Known("Song (Off Vocal)")
	pool := BuildCandidatePool(LocalRecallResult{Records: []LocalRecord{record}}, nil, sources, lookup)
	result := pool.Targets[0].Check(NewInputProfile("Song", "Artist", "Album"))
	if result.Eligibility != MetadataUnresolved {
		t.Fatal("user rename erased source version", result)
	}
	found := false
	for _, e := range result.Facets {
		for _, reason := range e.Reasons {
			if reason.Code == "current_cannot_override_source_constraint" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing constraint explanation", result)
	}
}

func TestCandidatePoolReliableAliasesAndImmutableInputs(t *testing.T) {
	sources, lookup, record, c := poolFixture(t)
	c.Title = sptr("Song（译名：歌曲）")
	c.ArtistName = sptr("Artist（原名：歌手）")
	c.AlbumTitle = sptr("Album（译名：专辑）")
	before, err := json.Marshal([]any{record, c})
	if err != nil {
		t.Fatal(err)
	}
	pool := BuildCandidatePool(LocalRecallResult{Records: []LocalRecord{record}}, []provider.Candidate{c}, sources, lookup)
	result := pool.Targets[0].Check(NewInputProfile("Song", "Artist", "Album"))
	if len(pool.Targets) != 1 || len(pool.Targets[0].Conflicts) != 0 || result.Eligibility != MetadataEligible {
		t.Fatal(pool, result)
	}
	after, err := json.Marshal([]any{record, c})
	if err != nil || string(before) != string(after) {
		t.Fatal("input objects modified", err)
	}
}

func TestCandidatePoolExplicitEmptyIsNotMissing(t *testing.T) {
	sources, lookup, record, c := poolFixture(t)
	record.TrackSnapshot.Title = domain.Cleared[string]()
	pool := BuildCandidatePool(LocalRecallResult{Records: []LocalRecord{record}}, []provider.Candidate{c}, sources, lookup)
	if len(pool.Targets[0].Conflicts) == 0 || pool.Targets[0].Check(NewInputProfile("Song", "Artist", "Album")).Eligibility != MetadataUnresolved {
		t.Fatal("explicit source empty was silently replaced", pool)
	}
}

func TestCandidatePoolCurrentPositionIsNotBorrowedFromSnapshot(t *testing.T) {
	sources, lookup, record, _ := poolFixture(t)
	record.Current.Profile = NewInputProfile("用户名", "Artist", "Album")
	pool := BuildCandidatePool(LocalRecallResult{Records: []LocalRecord{record}}, nil, sources, lookup)
	result := pool.Targets[0].Check(NewInputProfile("用户名", "Artist", "Album"))
	if result.Eligibility != MetadataUnresolved || result.Facets[0].Fields["title"].Level != EvidenceExact {
		t.Fatal("current name was combined with missing current position", result)
	}
}

func TestCandidatePoolTracklistIsIndependentEvidenceAndPartialIsUnknown(t *testing.T) {
	sources, lookup, record, _ := poolFixture(t)
	record.TrackSnapshot = nil
	record.Current.Profile = NewInputProfile("用户显示", "用户署名", "用户专辑")
	input := NewInputProfile("Song", "Artist", "Album")
	for _, complete := range []bool{true, false} {
		if !complete {
			record.ReleaseSnapshot.TracklistCompleteness = domain.Partial
		}
		pool := BuildCandidatePool(LocalRecallResult{Records: []LocalRecord{record}}, nil, sources, lookup)
		result := pool.Targets[0].Check(input)
		want := MetadataEligible
		if !complete {
			want = MetadataUnresolved
		}
		if result.Eligibility != want || len(pool.Targets[0].Facts) != 2 || pool.Targets[0].Facts[1].Kind != "tracklist" {
			t.Fatal(pool, result)
		}
	}
}
