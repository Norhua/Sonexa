package library

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type decisionProvider struct {
	summaryProvider
	requests []matching.DecisionRequest
	load     func(matching.DecisionRequest) ([]matching.TargetResolution, error)
	summary  *domain.ReleaseSummary
}

func (p *decisionProvider) LoadDecisionFacts(_ context.Context, request matching.DecisionRequest) ([]matching.TargetResolution, error) {
	p.requests = append(p.requests, request)
	return p.load(request)
}
func (p *decisionProvider) LoadReleaseSummary(ctx context.Context, id domain.Identity) (*domain.ReleaseSummary, error) {
	if p.summary != nil {
		p.calls++
		r := *p.summary
		r.Identity = id
		return &r, nil
	}
	return p.summaryProvider.LoadReleaseSummary(ctx, id)
}

func resolvedFact(t *testing.T, c provider.Candidate) matching.TargetResolution {
	t.Helper()
	snapshot, err := provider.CandidateSnapshot(c.Provider, c)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Release.TracklistCompleteness = domain.Complete
	snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{{
		Track: domain.Known(snapshot.Track.Identity), Title: snapshot.Track.Title, Credit: snapshot.Track.Credit,
		Recording: snapshot.Track.Recording, DiscNumber: *c.DiscNumber, TrackNumber: *c.TrackNumber,
	}})
	return matching.TargetResolution{State: matching.TargetExact, Candidates: []provider.Candidate{c}, Release: snapshot.Release, Completeness: domain.Complete}
}

func TestSelectionEvidenceDoesNotUseDisplayConfidence(t *testing.T) {
	s, _, _ := newTestService(t)
	c := candidate("release", "track", nil)
	selection, err := s.SelectCandidate(context.Background(), []matching.ScoredCandidate{{Candidate: c}}, 0, selectionProfile())
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != c.MatchRef {
		t.Fatal(selection, err)
	}
}

func TestSelectionEvidenceBatchesOnlyUnresolvedCompetitors(t *testing.T) {
	s, st, _ := newTestService(t)
	p := &decisionProvider{}
	s.registry.Register(p)
	a, b := candidate("release", "one", nil), candidate("release", "two", nil)
	b.TrackNumber = ptr(2)
	a.DiscNumber, b.DiscNumber = nil, nil
	excluded := candidate("other", "other", nil)
	excluded.AlbumTitle, excluded.DiscNumber = ptr("Unrelated Album"), nil
	// Resolve callback uses whole source views, not missing search fields.
	p.load = func(request matching.DecisionRequest) ([]matching.TargetResolution, error) {
		if len(request.Candidates) != 2 || !request.NeedTracklist {
			t.Fatal("excluded candidate requested or request not batched", request)
		}
		x, y := a, b
		x.DiscNumber, y.DiscNumber = ptr(1), ptr(1)
		return []matching.TargetResolution{resolvedFact(t, x), resolvedFact(t, y)}, nil
	}
	selection, err := s.SelectCandidate(context.Background(), high(excluded, a, b), 0, selectionProfile())
	if err != nil || selection.Candidate != nil || selection.Reason != ReasonAmbiguous || len(p.requests) != 1 || len(selection.ResolvedCandidates) != 3 {
		t.Fatal(selection, err, p.requests)
	}
	if count, err := st.CountTracks(store.TrackFilter{}); err != nil || count != 0 {
		t.Fatal("decision preview wrote collection", count, err)
	}
}

func TestSelectionEvidenceUnknownOrFailedFactsNeverManufactureUnique(t *testing.T) {
	for _, scenario := range []string{"unsupported", "partial", "error", "cancelled", "complete_not_found"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, _ := newTestService(t)
			p := &decisionProvider{}
			s.registry.Register(p)
			known, unknown := candidate("known", "known", nil), candidate("unknown", "unknown", nil)
			unknown.AlbumTitle = nil
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := errors.New("necessary decision request failed")
			p.load = func(request matching.DecisionRequest) ([]matching.TargetResolution, error) {
				if len(request.Candidates) != 1 || request.Candidates[0].MatchRef != "unknown" {
					t.Fatal(request)
				}
				switch scenario {
				case "error":
					return nil, wantErr
				case "cancelled":
					cancel()
					return nil, context.Canceled
				case "partial":
					c := unknown
					c.AlbumTitle = ptr("Album")
					result := resolvedFact(t, c)
					result.State, result.Completeness = matching.TargetInsufficient, domain.Partial
					result.Release.TracklistCompleteness = domain.Partial
					return []matching.TargetResolution{result}, nil
				case "complete_not_found":
					return []matching.TargetResolution{{State: matching.TargetNotFound, Completeness: domain.Complete}}, nil
				default:
					return []matching.TargetResolution{{State: matching.TargetUnsupported, Completeness: domain.Unloaded}}, nil
				}
			}
			selection, err := s.SelectCandidate(ctx, high(known, unknown), 0, selectionProfile())
			if scenario == "error" || scenario == "cancelled" {
				if err == nil || selection.Candidate != nil {
					t.Fatal(selection, err)
				}
				return
			}
			if err != nil || len(selection.ResolvedCandidates) != 2 {
				t.Fatal("manual candidate lost", selection, err)
			}
			if scenario == "complete_not_found" {
				if selection.Candidate == nil || selection.Candidate.MatchRef != known.MatchRef {
					t.Fatal(selection)
				}
			} else if selection.Candidate != nil || selection.Reason != ReasonAmbiguous {
				t.Fatal("unknown competitor disappeared", selection)
			}
		})
	}
}

func TestSelectionEvidenceRetainsConflictingLoadedSourceViews(t *testing.T) {
	for _, scenario := range []string{"track_title", "summary_title", "summary_group", "repeated_position"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, _ := newTestService(t)
			p := &decisionProvider{}
			s.registry.Register(p)
			a, b := candidate("a", "a", nil), candidate("b", "b", ptr(15))
			if scenario == "summary_title" || scenario == "summary_group" {
				p.summary = &domain.ReleaseSummary{ReleaseTrackTotal: domain.Known(14)}
				if scenario == "summary_title" {
					p.summary.Title = domain.Known("Other Album")
				} else {
					p.summary.Group = &domain.Identity{Provider: "test", Ref: "other-group"}
				}
			} else {
				a.DiscNumber = nil
				p.load = func(matching.DecisionRequest) ([]matching.TargetResolution, error) {
					c := a
					c.DiscNumber = ptr(1)
					result := resolvedFact(t, c)
					if scenario == "track_title" {
						result.Candidates[0].Title = ptr("Different Song")
					} else {
						entry := result.Release.Tracklist.Value[0]
						entry.Track = domain.Known(domain.Identity{Provider: "test", Ref: "uncollected-position"})
						entry.TrackNumber = 2
						result.Release.Tracklist.Value = append(result.Release.Tracklist.Value, entry)
					}
					return []matching.TargetResolution{result}, nil
				}
			}
			selection, err := s.SelectCandidate(context.Background(), high(a, b), 0, selectionProfile())
			if err != nil || selection.Candidate != nil || selection.Reason != ReasonAmbiguous {
				t.Fatal("new facts erased conflicting/competing source facts", selection, err)
			}
		})
	}
}
