package library

import (
	"context"
	"fmt"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

type summaryProvider struct {
	provider.Provider
	calls  int
	totals map[string]int
}

func (p *summaryProvider) ID() provider.ProviderID { return "test" }
func (p *summaryProvider) SnapshotFromCandidate(c provider.Candidate) (*domain.Snapshot, error) {
	return provider.CandidateSnapshot(p.ID(), c)
}
func (p *summaryProvider) LoadReleaseSummary(ctx context.Context, id domain.Identity) (*domain.ReleaseSummary, error) {
	p.calls++
	total := domain.Missing[int]()
	if n := p.totals[id.Ref]; n > 0 {
		total = domain.Known(n)
	}
	return &domain.ReleaseSummary{Identity: id, ReleaseTrackTotal: total}, nil
}
func (p *summaryProvider) ListVersions(context.Context, domain.Identity, provider.Page) (*provider.VersionPage, error) {
	return nil, fmt.Errorf("must not enumerate group")
}
func candidate(release, track string, total *int) provider.Candidate {
	title, album, artist, group, recording := "Song", "Album", "Artist", "group", "recording"
	return provider.Candidate{Provider: "test", MatchRef: track, ReleaseRef: &release, ReleaseGroupRef: &group, Title: &title, AlbumTitle: &album, AlbumCredit: &artist, RecordingRef: &recording, ReleaseTrackTotal: total}
}
func high(c ...provider.Candidate) []matching.ScoredCandidate {
	out := []matching.ScoredCandidate{}
	for _, v := range c {
		out = append(out, matching.ScoredCandidate{Candidate: v, Score: matching.Score{HighConf: true}})
	}
	return out
}
func TestAutomaticSelectionPrioritizesOwnedReleaseAndKnownTotal(t *testing.T) {
	s, _, _ := newTestService(t)
	p := &summaryProvider{totals: map[string]int{"a": 14, "b": 15}}
	s.registry.Register(p)
	a, b := candidate("a", "a1", nil), candidate("b", "b1", nil)
	selection, err := s.SelectCandidate(context.Background(), high(a, b), 0)
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "b1" || p.calls != 2 {
		t.Fatalf("total selection: %+v %v calls %d", selection, err, p.calls)
	}
	commit(t, s, prepare(t, s, seed("a", "owned", "group", "Album", "Artist")), false)
	p.calls = 0
	selection, err = s.SelectCandidate(context.Background(), high(a, b), 0)
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "a1" || p.calls != 0 || selection.Plan.Baseline.RequiresConfirmation {
		t.Fatalf("owned subset: %+v %v calls %d", selection, err, p.calls)
	}
	selection, err = s.SelectCandidate(context.Background(), high(b), 0)
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "b1" || selection.Reason != ReasonNewRelease || p.calls != 0 {
		t.Fatalf("unique new version must review: %+v %v", selection, err)
	}
}
func TestAutomaticSelectionAmbiguityUnknownTotalAndWorldTie(t *testing.T) {
	s, _, _ := newTestService(t)
	p := &summaryProvider{}
	s.registry.Register(p)
	n := 15
	a, b := candidate("a", "a1", &n), candidate("b", "b1", &n)
	world, jp := "XW", "JP"
	a.Country = &world
	b.Country = &jp
	selected, err := s.SelectCandidate(context.Background(), high(a, b), 0)
	if err != nil || selected.Candidate == nil || selected.Candidate.MatchRef != "b1" {
		t.Fatal(selected, err)
	}
	b.ReleaseTrackTotal = nil
	selected, err = s.SelectCandidate(context.Background(), high(a, b), 0)
	if err != nil || selected.Reason != ReasonUnknownTotal || p.calls != 1 {
		t.Fatal(selected, err, p.calls)
	}
	b.ReleaseTrackTotal = &n
	b.ReleaseRef = a.ReleaseRef
	selected, err = s.SelectCandidate(context.Background(), high(a, b), 0)
	if err != nil || selected.Reason != ReasonAmbiguous {
		t.Fatal("same release positions accepted", selected, err)
	}
	b = candidate("b", "b1", &n)
	other := "different recording"
	b.RecordingRef = &other
	selected, err = s.SelectCandidate(context.Background(), high(a, b), 0)
	if err != nil || selected.Reason != ReasonAmbiguous {
		t.Fatal("different content accepted", selected, err)
	}
}

func TestDisplayPreferencesDoNotChangeSelectionOrBindings(t *testing.T) {
	s, st, _ := newTestService(t)
	s.registry.Register(&summaryProvider{})
	makeVersion := func(ref, format, country string) domain.AdmissionResult {
		input := seed(ref, ref+"-owned", "group", "Album", "Artist")
		input.Snapshot.Release.Media = domain.Known([]domain.Medium{{Number: 1, Format: domain.Known(format)}})
		input.Snapshot.Release.Country = domain.Known(country)
		input.Snapshot.Release.Status = domain.Known("Official")
		return *commit(t, s, prepare(t, s, input), ref == "b")
	}
	a := makeVersion("a", "CD", "JP")
	b := makeVersion("b", "Digital Media", "US")
	short, long := 14, 15
	candidates := high(candidate("a", "a-next", &short), candidate("b", "b-next", &long))
	for i, order := range [][]string{{"CD", "Digital Media"}, {"Digital Media", "CD"}} {
		cfg := matching.DefaultLadderConfig()
		cfg.MediumPriority = order
		st.SetLadderConfig(cfg)
		rep, err := st.RepresentativeRelease(a.GroupID)
		if err != nil {
			t.Fatal(err)
		}
		want := a.ReleaseID
		if i == 1 {
			want = b.ReleaseID
		}
		if rep.ID != want {
			t.Fatalf("representative: %d want %d", rep.ID, want)
		}
		selected, err := s.SelectCandidate(context.Background(), candidates, 0)
		if err != nil || selected.Candidate == nil || selected.Candidate.MatchRef != "b-next" {
			t.Fatalf("preference affected admission: %+v %v", selected, err)
		}
		for _, saved := range []domain.AdmissionResult{a, b} {
			track, err := st.GetTrack(saved.TrackID)
			if err != nil || track.ReleaseID == nil || *track.ReleaseID != saved.ReleaseID {
				t.Fatal("preference rebound collection", err)
			}
		}
	}
}
