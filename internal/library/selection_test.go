package library

import (
	"context"
	"fmt"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type summaryProvider struct {
	provider.MetadataProvider
	calls  int
	totals map[string]int
	// details 按发行标识返回发行详情（含清单）。
	details     map[string]*domain.Snapshot
	detailCalls int
}

func (p *summaryProvider) Complete(_ context.Context, target domain.DetailTarget) (*domain.Snapshot, error) {
	p.detailCalls++
	if snapshot, ok := p.details[target.Release.Ref]; ok {
		return snapshot, nil
	}
	return nil, provider.ErrUnsupported
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

// releaseDetail 构造一份带完整清单的发行详情。entries 依次是 曲目标识、录音标识。
func releaseDetail(release string, entries ...[2]string) *domain.Snapshot {
	identity := domain.Identity{Provider: "test", Ref: release}
	group := domain.Identity{Provider: "test", Ref: "group"}
	r := &domain.ReleaseSnapshot{Identity: identity, Group: &group, Title: domain.Known("Album"), Credit: domain.Known(domain.Credit{Text: "Artist"}), TracklistCompleteness: domain.Complete}
	list := []domain.TracklistEntry{}
	for i, e := range entries {
		list = append(list, domain.TracklistEntry{DiscNumber: 1, TrackNumber: i + 1, Title: domain.Known("Song"),
			Track: domain.Known(domain.Identity{Provider: "test", Ref: e[0]}), Recording: domain.Known(domain.Identity{Provider: "test", Ref: e[1]})})
	}
	r.Tracklist = domain.Known(list)
	return &domain.Snapshot{Release: r}
}
func candidate(release, track string, total *int) provider.Candidate {
	title, album, artist, group, recording := "Song", "Album", "Artist", "group", "recording"
	return provider.Candidate{Provider: "test", MatchRef: track, ReleaseRef: &release, ReleaseGroupRef: &group, Title: &title, AlbumTitle: &album, AlbumCredit: &artist, RecordingRef: &recording, ReleaseTrackTotal: total}
}
func high(c ...provider.Candidate) []matching.ScoredCandidate {
	out := []matching.ScoredCandidate{}
	for _, v := range c {
		out = append(out, matching.ScoredCandidate{Candidate: v, Score: matching.Score{HighConf: true, TitleMatch: matching.MatchExact}})
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
	// 已有发行的清单取不到：无法确认它是否含这首歌，不自动新建发行，也不报成新增发行。
	selection, err = s.SelectCandidate(context.Background(), high(b), 0)
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "b1" || selection.Reason != ReasonSongUnresolved || p.calls != 0 ||
		selection.Plan.Baseline.Coverage != domain.CoverageUnknown || !selection.Plan.Baseline.NeedsTypedConfirm {
		t.Fatalf("unique new version must review: %+v %v", selection, err)
	}
	// 清单取到后：已有发行按录音含这首歌，改为准入它的那条清单，不新建发行。
	p.details = map[string]*domain.Snapshot{"a": releaseDetail("a", [2]string{"owned", "other"}, [2]string{"a1", "recording"})}
	selection, err = s.SelectCandidate(context.Background(), high(b), 0)
	if err != nil || selection.Reason != "" || selection.Substitute == nil || selection.Substitute.TrackRef != "a1" ||
		selection.Plan.Seed.Snapshot.Track.Identity.Ref != "a1" || selection.Plan.Baseline.RequiresConfirmation || selection.Plan.Baseline.NeedsTypedConfirm || p.detailCalls != 2 {
		t.Fatalf("song not resolved to the existing release: %+v %v details %d", selection, err, p.detailCalls)
	}
	// 清单已保存，再次解析不再发请求。
	if selection, err = s.SelectCandidate(context.Background(), high(b), 0); err != nil || selection.Substitute == nil || p.detailCalls != 2 {
		t.Fatalf("tracklist refetched: %+v %v details %d", selection, err, p.detailCalls)
	}
}
func TestAutomaticSelectionPrefersSyncOnlyRelease(t *testing.T) {
	s, st, _ := newTestService(t)
	p := &summaryProvider{totals: map[string]int{"a": 10, "b": 20}}
	s.registry.Register(p)
	source, release, group := "test", "a", "group"
	if _, err := st.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "Saved", Album: ptrField(domain.Known("Album")), AlbumCredit: ptrField(domain.Known("Artist")), MatchSource: &source, MatchRef: ptrField(domain.Known("saved")), Release: &store.ReleaseInput{Provider: &source, ProviderRef: &release, ReleaseGroupRef: &group}}); err != nil {
		t.Fatal(err)
	}
	selection, err := s.SelectCandidate(context.Background(), high(candidate("a", "a1", nil), candidate("b", "b1", nil)), 0)
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "a1" || selection.Reason != "" || p.calls != 0 || selection.Plan.Baseline.RequiresConfirmation {
		t.Fatal("sync-only release not preferred or unnecessarily loaded summaries", selection, err, p.calls)
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
