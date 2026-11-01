package musicbrainz

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

const identityRelease = "11111111-1111-4111-8111-111111111111"
const identityRecording = "2283ceda-78b1-4d20-8f52-bdf73bd53686"
const identityTrack = "f0a33d1a-628a-4a53-bc4e-6af077505956"
const identityDiscTwoTrack = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"

func identityFixture(t *testing.T) *releaseDetail {
	t.Helper()
	body, err := os.ReadFile("testdata/identity_repeated_recording.json")
	if err != nil {
		t.Fatal(err)
	}
	var rel releaseDetail
	if err := json.Unmarshal(body, &rel); err != nil {
		t.Fatal(err)
	}
	return &rel
}
func identitySeed() provider.Candidate {
	return provider.Candidate{Provider: provider.MusicBrainz, MatchRef: identityRecording + "@" + identityRelease,
		RecordingRef: strPtr(identityRecording), ReleaseRef: strPtr(identityRelease), Title: strPtr("input must not become facts")}
}
func identityPosition(disc, track int64) matching.PositionRequirement {
	p := matching.PositionRequirement{Disc: matching.MissingNumber("number", "disc"), Track: matching.MissingNumber("number", "track")}
	if disc > 0 {
		p.Disc.State, p.Disc.Value = matching.ValueKnown, disc
	}
	if track > 0 {
		p.Track.State, p.Track.Value = matching.ValueKnown, track
	}
	return p
}

func TestMusicBrainzInspectCandidateIdentity(t *testing.T) {
	p := &Provider{}
	seed := identitySeed()
	if got := p.InspectCandidate(seed); got.State != matching.IdentityUnresolved {
		t.Fatal(got)
	}
	seed.MatchRef = identityTrack
	if got := p.InspectCandidate(seed); got.State != matching.IdentityExact {
		t.Fatal(got)
	}
	for _, mutate := range []func(*provider.Candidate){
		func(c *provider.Candidate) { c.MatchRef = "not-a-track" },
		func(c *provider.Candidate) { c.Provider = provider.ITunes },
		func(c *provider.Candidate) { c.ReleaseRef = nil },
		func(c *provider.Candidate) { c.MatchRef = identityRecording + "@22222222-2222-4222-8222-222222222222" },
	} {
		c := seed
		mutate(&c)
		if got := p.InspectCandidate(c); got.State != matching.IdentityInvalid {
			t.Fatal(got)
		}
	}
}

func TestMusicBrainzResolvesPositionsWithoutChoosingFirst(t *testing.T) {
	p := &Provider{}
	for _, tt := range []struct {
		name     string
		position matching.PositionRequirement
		change   func(*releaseDetail)
		want     matching.TargetState
		count    int
		track    string
	}{
		{"repeated recording", identityPosition(0, 0), nil, matching.TargetAmbiguous, 2, ""},
		{"track number without disc", identityPosition(0, 1), nil, matching.TargetAmbiguous, 2, ""},
		{"second disc", identityPosition(2, 1), nil, matching.TargetExact, 1, identityDiscTwoTrack},
		{"unique first disc", identityPosition(1, 1), nil, matching.TargetExact, 1, identityTrack},
		{"not found", identityPosition(1, 2), nil, matching.TargetNotFound, 0, ""},
		{"missing disc", identityPosition(0, 0), func(r *releaseDetail) { r.Media[1].Position = 0 }, matching.TargetInsufficient, 1, ""},
		{"partial list", identityPosition(0, 0), func(r *releaseDetail) { r.Media = r.Media[:1] }, matching.TargetInsufficient, 1, ""},
		{"missing track id", identityPosition(0, 0), func(r *releaseDetail) { r.Media[1].Tracks[0].ID = "" }, matching.TargetInsufficient, 1, ""},
		{"missing other recording", identityPosition(0, 0), func(r *releaseDetail) { r.Media[0].Tracks[1].Recording = nil }, matching.TargetInsufficient, 2, ""},
		{"numeric number fallback", identityPosition(2, 1), func(r *releaseDetail) { r.Media[1].Tracks[0].Position = 0 }, matching.TargetExact, 1, identityDiscTwoTrack},
		{"nonnumeric number", identityPosition(2, 1), func(r *releaseDetail) { r.Media[1].Tracks[0].Position = 0; r.Media[1].Tracks[0].Number = "A1" }, matching.TargetInsufficient, 0, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rel := identityFixture(t)
			if tt.change != nil {
				tt.change(rel)
			}
			got := p.resolveCandidate(identitySeed(), tt.position, rel)
			if got.State != tt.want || len(got.Candidates) != tt.count {
				t.Fatalf("got %+v", got)
			}
			if tt.track != "" && got.Candidates[0].MatchRef != tt.track {
				t.Fatal("selected wrong position", got)
			}
			if tt.want == matching.TargetInsufficient && got.Completeness == domain.Complete {
				t.Fatal("partial claimed complete")
			}
			for _, c := range got.Candidates {
				if c.Title == nil || *c.Title != "you" || *c.ReleaseRef != identityRelease {
					t.Fatalf("query leaked or release changed: %+v", c)
				}
			}
		})
	}
}

func TestMusicBrainzSameRecordingDifferentReleaseRemainsSeparate(t *testing.T) {
	p := &Provider{}
	a, b := identityFixture(t), identityFixture(t)
	b.ID = "22222222-2222-4222-8222-222222222222"
	b.Media[0].Tracks[0].ID = "f4458acc-aa40-40b0-a0b7-aaa27df24b04"
	seedA, seedB := identitySeed(), identitySeed()
	seedB.MatchRef, seedB.ReleaseRef = identityRecording+"@"+b.ID, &b.ID
	ra := p.resolveCandidate(seedA, identityPosition(1, 1), a)
	rb := p.resolveCandidate(seedB, identityPosition(1, 1), b)
	if ra.State != matching.TargetExact || rb.State != matching.TargetExact || ra.Candidates[0].MatchRef == rb.Candidates[0].MatchRef || *ra.Candidates[0].ReleaseRef == *rb.Candidates[0].ReleaseRef {
		t.Fatal(ra, rb)
	}
}
