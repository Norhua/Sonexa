package library

import (
	"context"
	"errors"
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
	refs   []string
	totals map[string]int
}

func (p *summaryProvider) ID() provider.ProviderID { return "test" }
func (p *summaryProvider) SnapshotFromCandidate(c provider.Candidate) (*domain.Snapshot, error) {
	return provider.CandidateSnapshot(p.ID(), c)
}
func (p *summaryProvider) LoadReleaseSummary(ctx context.Context, id domain.Identity) (*domain.ReleaseSummary, error) {
	p.calls++
	p.refs = append(p.refs, id.Ref)
	total := domain.Missing[int]()
	if n := p.totals[id.Ref]; n > 0 {
		total = domain.Known(n)
	}
	return &domain.ReleaseSummary{Identity: id, ReleaseTrackTotal: total}, nil
}
func (p *summaryProvider) ListVersions(context.Context, domain.Identity, provider.Page) (*provider.VersionPage, error) {
	return nil, fmt.Errorf("must not enumerate group")
}
func selectionProfile() *matching.InputProfile {
	p := matching.NewInputProfile("Song", "Artist", "Album")
	return &p
}

func candidate(release, track string, total *int) provider.Candidate {
	title, album, artist, group, recording := "Song", "Album", "Artist", "group", "recording"
	return provider.Candidate{Provider: "test", MatchRef: track, ReleaseRef: &release, ReleaseGroupRef: &group, Title: &title, ArtistName: &artist, DiscNumber: ptr(1), TrackNumber: ptr(1), AlbumTitle: &album, AlbumCredit: &artist, RecordingRef: &recording, ReleaseTrackTotal: total}
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
	selection, err := s.SelectCandidate(context.Background(), high(a, b), 0, selectionProfile())
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "b1" || p.calls != 2 {
		t.Fatalf("total selection: %+v %v calls %d", selection, err, p.calls)
	}
	commit(t, s, prepare(t, s, seed("a", "owned", "group", "Album", "Artist")), false)
	p.calls = 0
	selection, err = s.SelectCandidate(context.Background(), high(a, b), 0, selectionProfile())
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "a1" || p.calls != 0 || selection.Plan.Baseline.RequiresConfirmation {
		t.Fatalf("owned subset: %+v %v calls %d", selection, err, p.calls)
	}
	selection, err = s.SelectCandidate(context.Background(), high(b), 0, selectionProfile())
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
	selected, err := s.SelectCandidate(context.Background(), high(a, b), 0, selectionProfile())
	if err != nil || selected.Candidate == nil || selected.Candidate.MatchRef != "b1" {
		t.Fatal(selected, err)
	}
	b.ReleaseTrackTotal = nil
	selected, err = s.SelectCandidate(context.Background(), high(a, b), 0, selectionProfile())
	if err != nil || selected.Reason != ReasonUnknownTotal || p.calls != 1 {
		t.Fatal(selected, err, p.calls)
	}
	b.ReleaseTrackTotal = &n
	b.ReleaseRef = a.ReleaseRef
	selected, err = s.SelectCandidate(context.Background(), high(a, b), 0, selectionProfile())
	if err != nil || selected.Reason != ReasonAmbiguous {
		t.Fatal("same release positions accepted", selected, err)
	}
	b = candidate("b", "b1", &n)
	other := "different recording"
	b.RecordingRef = &other
	selected, err = s.SelectCandidate(context.Background(), high(a, b), 0, selectionProfile())
	if err != nil || selected.Reason != ReasonAmbiguous {
		t.Fatal("different content accepted", selected, err)
	}
}

func TestSelectionCompatibleOwnedTrackPreference(t *testing.T) {
	for _, scenario := range []string{"owned_track", "explicit_deluxe", "multiple_owned", "unknown_owned_total", "repeated_position", "cross_group", "different_recording", "country_tie", "stable_tie"} {
		t.Run(scenario, func(t *testing.T) {
			s, st, _ := newTestService(t)
			p := &summaryProvider{totals: map[string]int{"a": 14, "b": 15}}
			s.registry.Register(p)
			a, b := candidate("a", "a-track", nil), candidate("b", "b-track", nil)
			profile := selectionProfile()
			if scenario == "explicit_deluxe" {
				b.AlbumTitle = ptr("Album (Deluxe Edition)")
				profile = func() *matching.InputProfile { p := matching.ProfileFromCandidate(b); return &p }()
			}
			if scenario == "cross_group" {
				b.AlbumTitle, b.ReleaseGroupRef = ptr("Different Album"), ptr("different-group")
				profile.Album = matching.ParseMetadataText("", matching.MetadataAlbum)
			}
			if scenario == "different_recording" {
				b.RecordingRef = ptr("different-recording")
			}
			if scenario == "repeated_position" {
				b.ReleaseRef, b.TrackNumber = a.ReleaseRef, ptr(2)
			}
			if scenario == "country_tie" || scenario == "stable_tie" {
				p.totals["a"], p.totals["b"] = 15, 15
				if scenario == "country_tie" {
					a.Country, b.Country = ptr("XW"), ptr("JP")
				}
			}
			owned := commit(t, s, mustCandidatePlan(t, s, a), false)
			multiple := scenario == "multiple_owned" || scenario == "unknown_owned_total" || scenario == "country_tie" || scenario == "stable_tie"
			if multiple {
				commit(t, s, mustCandidatePlan(t, s, b), true)
			} else if scenario == "owned_track" {
				// B's release exists and is larger, but its matching track does not.
				other := b
				other.MatchRef, other.Title = "other-song", ptr("Other Song")
				commit(t, s, mustCandidatePlan(t, s, other), true)
			}
			if scenario == "unknown_owned_total" {
				delete(p.totals, "b")
			}
			countBefore, err := st.CountTracks(store.TrackFilter{})
			if err != nil {
				t.Fatal(err)
			}
			// Unknown total on a third, unowned compatible release must not
			// trigger I/O or block the owned subset's default preference.
			unowned := candidate("unowned", "unowned-track", nil)
			rows := high(b, unowned, a)
			if scenario == "explicit_deluxe" {
				rows = high(a, b)
			}
			selected, err := s.SelectCandidate(context.Background(), rows, 0, profile)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "repeated_position", "different_recording", "cross_group", "unknown_owned_total":
				if selected.Candidate != nil || selected.Reason == "" {
					t.Fatal("preference erased ambiguity", selected)
				}
			case "explicit_deluxe":
				if selected.Candidate == nil || selected.Candidate.MatchRef != b.MatchRef || selected.Reason != ReasonNewRelease {
					t.Fatal("owned track overrode explicit edition", selected)
				}
			default:
				want := a.MatchRef
				if scenario == "multiple_owned" || scenario == "country_tie" {
					want = b.MatchRef
				}
				if selected.Candidate == nil || selected.Candidate.MatchRef != want || selected.Reason != "" {
					t.Fatal("wrong owned preference", selected, want)
				}
				result := commit(t, s, selected.Plan, false)
				if !result.Existing || (want == a.MatchRef && result.TrackID != owned.TrackID) {
					t.Fatal("owned preference created track", result)
				}
				found := false
				for _, reason := range selected.Reasons {
					found = found || reason.Code == "prefer_owned_compatible_track"
				}
				if !found {
					t.Fatal("default preference was not explained", selected.Reasons)
				}
			}
			wantCalls := 0
			if multiple {
				wantCalls = 2
			}
			if p.calls != wantCalls {
				t.Fatal("unnecessary/missing summary requests", p.calls, p.refs)
			}
			for _, ref := range p.refs {
				if ref == "unowned" {
					t.Fatal("loaded outside preferred subset", p.refs)
				}
			}
			countAfter, err := st.CountTracks(store.TrackFilter{})
			if err != nil || countAfter != countBefore {
				t.Fatal("selection created collection", countBefore, countAfter, err)
			}
		})
	}
}

func mustCandidatePlan(t *testing.T, s *Service, c provider.Candidate) *domain.AdmissionPlan {
	t.Helper()
	plan, err := s.PrepareCandidate(context.Background(), c, 0)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestAutomaticOwnedTargetUsesSkipPlanAndEvidenceRevision(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			s, st, _ := newTestService(t)
			c := candidate("release", "track", nil)
			owned := commit(t, s, mustCandidatePlan(t, s, c), false)
			batch, err := st.CreateBatch(&store.ImportBatch{Filename: "skip.csv", TotalRows: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: "[]", Profile: selectionProfile()}}); err != nil {
				t.Fatal(err)
			}
			item, err := st.ClaimNextItem(batch)
			if err != nil {
				t.Fatal(err)
			}
			selection, err := s.SelectCandidate(context.Background(), high(c), item.ID, selectionProfile())
			if err != nil || selection.Plan == nil || selection.Plan.Seed.Operation != domain.AdmissionSkipExisting || selection.Plan.Baseline.ExistingTrackID != owned.TrackID {
				t.Fatal("automatic owned target used admission instead of skip", selection, err)
			}
			if changed {
				if err := st.ApplyMetadataEdits(owned.TrackID, map[string]string{"title": "changed"}, nil, nil); err != nil {
					t.Fatal(err)
				}
				if _, err := s.CommitAdmission(context.Background(), selection.Plan.ID, selection.Plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
					t.Fatal("skip accepted changed evidence", err)
				}
				return
			}
			result := commit(t, s, selection.Plan, false)
			got, err := st.GetItem(item.ID)
			if err != nil || !result.Existing || got.Status != store.StatusSkipped || got.TrackID == nil || *got.TrackID != owned.TrackID {
				t.Fatal(result, got, err)
			}
		})
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
		selected, err := s.SelectCandidate(context.Background(), candidates, 0, selectionProfile())
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
