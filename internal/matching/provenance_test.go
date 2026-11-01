package matching

import (
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

type fakeLookup struct {
	bindings map[domain.Identity]LocalBinding
	err      error
	calls    int
}

func (f *fakeLookup) FindExactBinding(track domain.Identity) (*LocalBinding, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	binding, ok := f.bindings[track]
	if !ok {
		return nil, nil
	}
	return &binding, nil
}

func TestResolveLocalExactOwnershipOnly(t *testing.T) {
	sources, err := NewCandidateSources(identityFake{id: "one", state: IdentityExact}, identityFake{id: "two", state: IdentityExact})
	if err != nil {
		t.Fatal(err)
	}
	track := domain.Identity{Provider: "one", Ref: "track-a"}
	binding := LocalBinding{TrackID: 42, Track: track, Release: domain.Identity{Provider: "one", Ref: "release-a"}, Revision: 3, ReleaseRevision: 5}
	lookup := &fakeLookup{bindings: map[domain.Identity]LocalBinding{track: binding}}
	for _, tt := range []struct {
		name           string
		source         provider.ProviderID
		track, release string
		want           OwnershipState
	}{
		{"exact existing", "one", "track-a", "release-a", OwnershipOwned},
		{"same recording/ISRC other track", "one", "track-b", "release-b", OwnershipUnowned},
		{"same reference other source", "two", "track-a", "release-a", OwnershipUnowned},
		{"unbound matching text", "one", "track-c", "release-c", OwnershipUnowned},
		{"conflicting release", "one", "track-a", "release-b", OwnershipConflict},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveLocal(lookup, sources, provider.Candidate{Provider: tt.source, MatchRef: tt.track, ReleaseRef: &tt.release,
				Title: sptr("you"), ArtistName: sptr("同一歌手"), ISRC: sptr("JPE301500361"), RecordingRef: sptr("same-recording")})
			if got.State != tt.want {
				t.Fatalf("ownership: %+v %v", got, err)
			}
			if tt.want == OwnershipConflict {
				if !errors.Is(err, ErrIdentityConflict) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got.State == OwnershipOwned {
				if got.TrackID != 42 || got.Revision != 3 || got.ReleaseRevision != 5 || got.Action() != CandidateSkip || len(got.Badges(tt.source)) != 2 {
					t.Fatal(got)
				}
			} else if got.TrackID != 0 || got.Revision != 0 || len(got.Badges(tt.source)) != 1 {
				t.Fatal("related evidence became owned", got)
			}
		})
	}
	if lookup.calls != 5 {
		t.Fatalf("unexpected lookup count %d", lookup.calls)
	}
}

func TestResolveLocalIdentityStatesAndLookupFailure(t *testing.T) {
	for _, tt := range []struct {
		identity CandidateIdentityState
		want     OwnershipState
		wantErr  error
	}{
		{IdentityUnresolved, OwnershipUnowned, nil},
		{IdentityInvalid, OwnershipConflict, ErrIdentityConflict},
		{IdentityUnsupported, OwnershipError, ErrIdentityUnavailable},
	} {
		sources, err := NewCandidateSources(identityFake{id: "one", state: tt.identity})
		if err != nil {
			t.Fatal(err)
		}
		lookup := &fakeLookup{}
		got, err := ResolveLocal(lookup, sources, provider.Candidate{Provider: "one", MatchRef: "synthetic", ReleaseRef: sptr("release")})
		if got.State != tt.want || got.Identity.State != tt.identity || !errors.Is(err, tt.wantErr) || lookup.calls != 0 || got.TrackID != 0 {
			t.Fatalf("identity state: %+v %v calls=%d", got, err, lookup.calls)
		}
	}
	sources, err := NewCandidateSources(identityFake{id: "one", state: IdentityExact})
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("database unavailable")
	got, err := ResolveLocal(&fakeLookup{err: failure}, sources, provider.Candidate{Provider: "one", MatchRef: "track", ReleaseRef: sptr("release")})
	if !errors.Is(err, failure) || got.State != OwnershipError || got.TrackID != 0 || got.Note() == "" || got.Action() != CandidateUnavailable {
		t.Fatalf("lookup failure hidden: %+v %v", got, err)
	}
}

func TestDurationTolerance(t *testing.T) {
	cases := []struct {
		a, b int
		want bool
	}{
		{220000, 220000, true}, {220000, 222000, true}, {220000, 223000, true},
		{220000, 223001, false}, {223000, 220000, true}, {220000, 240000, false},
	}
	for _, c := range cases {
		if got := DurationWithinTolerance(c.a, c.b); got != c.want {
			t.Errorf("DurationWithinTolerance(%d,%d) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
