package library

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func savedReleaseTrack(t *testing.T, st *store.Store, source, release string, inLibrary bool) int64 {
	t.Helper()
	id, err := st.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: inLibrary,
		Title: "Other song", Album: ptrField(domain.Known("Album")), AlbumCredit: ptrField(domain.Known("Artist")),
		MatchSource: &source, MatchRef: ptrField(domain.Known("saved-" + release)),
		Release: &store.ReleaseInput{Provider: &source, ProviderRef: &release, ReleaseGroupRef: ptrField(domain.Known("group"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestExistingReleasePreferenceDoesNotBypassAutomaticEvidence(t *testing.T) {
	for _, mode := range []string{"different-recording", "same-release-positions", "cross-group", "version", "review-only", "low-confidence", "exact-subset"} {
		t.Run(mode, func(t *testing.T) {
			s, st, _ := newTestService(t)
			s.registry.Register(&summaryProvider{})
			savedReleaseTrack(t, st, "test", "a", false)
			total := 12
			a, b := candidate("a", "a-next", &total), candidate("b", "b-next", &total)
			input := high(a, b)
			want := ReasonAmbiguous
			switch mode {
			case "different-recording":
				input[1].Candidate.RecordingRef = ptrField(domain.Known("another-recording"))
			case "same-release-positions":
				input[1].Candidate.ReleaseRef = a.ReleaseRef
			case "cross-group":
				input[1].Candidate.ReleaseGroupRef = ptrField(domain.Known("another-group"))
				input[1].Candidate.AlbumTitle = ptrField(domain.Known("Another album"))
				want = ReasonGroupConflict
			case "version":
				input[1].Candidate.Title = ptrField(domain.Known("Song (Live)"))
			case "review-only":
				input[0].Score.ReviewOnly = true
				want = ReasonNewRelease
			case "low-confidence":
				input[0].Score.HighConf = false
				want = ReasonNewRelease
			case "exact-subset":
				input[1].Score.TitleMatch, input[1].Score.CreditMatch, input[1].Score.AlbumMatch = matching.MatchExact, matching.MatchExact, matching.MatchExact
				want = ReasonNewRelease
			}
			selection, err := s.SelectCandidate(context.Background(), input, 0)
			if err != nil || selection.Reason != want {
				t.Fatal(selection, err)
			}
			if want == ReasonNewRelease && (selection.Candidate == nil || selection.Candidate.MatchRef != "b-next" || !selection.Plan.Baseline.RequiresConfirmation) {
				t.Fatal("ineligible existing release overrode evidence", selection)
			}
			if want != ReasonNewRelease && selection.Candidate != nil {
				t.Fatal("existing release resolved a content ambiguity", selection)
			}
		})
	}
}

func TestMultipleExistingReleasesKeepTotalComparisonAndSourceIsolation(t *testing.T) {
	s, st, _ := newTestService(t)
	p := &summaryProvider{}
	s.registry.Register(p)
	savedReleaseTrack(t, st, "test", "a", true)
	savedReleaseTrack(t, st, "test", "b", false)
	short, long, largest := 10, 12, 20
	input := high(candidate("new", "new-next", &largest), candidate("a", "a-next", &short), candidate("b", "b-next", &long))
	selection, err := s.SelectCandidate(context.Background(), input, 0)
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "b-next" || selection.Reason != "" {
		t.Fatal("all local releases must compete equally, new releases are excluded", selection, err)
	}
	input[2].Candidate.ReleaseTrackTotal = nil
	selection, err = s.SelectCandidate(context.Background(), input, 0)
	if err != nil || selection.Reason != ReasonUnknownTotal || p.calls != 1 {
		t.Fatal("multiple existing versions still require known totals", selection, err, p.calls)
	}

	other, otherStore, _ := newTestService(t)
	other.registry.Register(&summaryProvider{})
	savedReleaseTrack(t, otherStore, "other-provider", "a", false)
	selection, err = other.SelectCandidate(context.Background(), high(candidate("a", "a-next", &short), candidate("b", "b-next", &long)), 0)
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "b-next" {
		t.Fatal("another source's release ref was used as preference", selection, err)
	}
}

func TestExistingReleaseRemovedBeforeCommitInvalidatesAutomaticPlan(t *testing.T) {
	s, st, _ := newTestService(t)
	s.registry.Register(&summaryProvider{})
	track := savedReleaseTrack(t, st, "test", "a", false)
	batch, err := st.CreateBatch(&store.ImportBatch{TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: `[]`, InTitle: ptrField(domain.Known("Song")), InArtist: ptrField(domain.Known("Artist")), InAlbum: ptrField(domain.Known("Album"))}}); err != nil {
		t.Fatal(err)
	}
	item, err := st.ClaimNextItem(batch)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	total := 12
	selection, err := s.SelectCandidate(context.Background(), high(candidate("a", "a-next", nil), candidate("b", "b-next", &total)), item.ID)
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != "a-next" || selection.Reason != "" {
		t.Fatal(selection, err)
	}
	releaseID := selection.Plan.Baseline.ExistingReleaseID
	if err := st.DeleteTrack(track); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteOrphanRelease(releaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), selection.Plan.ID, selection.Plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("deleted preferred release silently recreated", err)
	}
	if id, err := st.FindByMatchRef("test", "a-next"); err != nil || id != 0 {
		t.Fatal("stale plan wrote a track", id, err)
	}
	item, err = st.GetItem(item.ID)
	if err != nil || item.Status != store.StatusMatching || item.TrackID != nil {
		t.Fatal("failed commit changed the input terminal state", item, err)
	}
}
