package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func problemInput(t *testing.T, st *store.Store, title string) *store.ImportItem {
	t.Helper()
	profile := matching.NewInputProfile(title, "Artist", "Album")
	batch, err := st.CreateBatch(&store.ImportBatch{Filename: "source.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: `["` + title + `","Artist","Album"]`, Profile: &profile}}); err != nil {
		t.Fatal(err)
	}
	item, err := st.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReview(item.ID, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	item, err = st.GetItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func problemScope(t *testing.T, st *store.Store, itemID int64) (*domain.InputScope, store.ProblemScope) {
	t.Helper()
	token, err := st.ItemProblemScope(itemID)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := st.ReadProblemInputScope(token)
	if err != nil {
		t.Fatal(err)
	}
	return scope, token
}

func TestInputScopeInvalidatesApprovalsWithoutExpandingRows(t *testing.T) {
	for _, single := range []bool{false, true} {
		for _, mutation := range []string{"join", "split-other", "query", "candidates", "member-revision", "input-revision", "proof", "profile", "rule"} {
			t.Run(map[bool]string{false: "problem", true: "single"}[single]+"/"+mutation, func(t *testing.T) {
				s, st, _ := newTestService(t)
				commit(t, s, prepare(t, s, seed("existing", "other", "group", "Album", "Artist")), false)
				a, b := problemInput(t, st, "Song"), problemInput(t, st, "Song（译名：歌曲）")
				scope, token := problemScope(t, st, a.ID)
				if len(scope.Members) != 2 {
					t.Fatal(scope)
				}
				if single {
					var err error
					scope, err = st.ReadSingleInputScope(a.ID, a.Revision)
					if err != nil {
						t.Fatal(err)
					}
				}
				target := seed("new", "new-track", "group", "Album", "Artist")
				target.InputScope = scope
				plan := prepare(t, s, target)
				if !plan.Baseline.RequiresConfirmation || !reflect.DeepEqual(plan.Seed.InputScope, plan.Baseline.InputScope) {
					t.Fatal(plan)
				}
				approval, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash)
				if err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "join":
					problemInput(t, st, "Song")
				case "split-other":
					_, err = st.SplitProblemItem(token.ProblemID, token.Revision, b.ID, b.Revision)
				case "query":
					_, err = st.BeginProblemQuery(token, store.ReviewQuery{Version: store.ReviewProtocolVersion, Title: "other"})
				case "candidates":
					err = st.NoneMatchedProblem(token)
				case "member-revision":
					_, err = st.DB().Exec("UPDATE review_problem_item SET revision=revision+1 WHERE item_id=?", b.ID)
				case "input-revision":
					_, err = st.DB().Exec("UPDATE import_item SET revision=revision+1 WHERE id=?", b.ID)
				// Digests bind the actual evidence too, not merely an integer
				// whose increment a future evidence rebuild might forget.
				case "proof":
					_, err = st.DB().Exec("UPDATE review_problem SET proof_json=json_set(proof_json,'$.names.title',json('[\"changed\"]')) WHERE id=?", token.ProblemID)
				case "profile":
					_, err = st.DB().Exec("UPDATE import_item SET input_profile=json_set(input_profile,'$.title.raw','Changed') WHERE id=?", b.ID)
				case "rule":
					_, err = st.DB().Exec("UPDATE review_problem SET rule_version=rule_version+1 WHERE id=?", token.ProblemID)
				}
				if err != nil {
					t.Fatal(err)
				}
				before := libraryTables(t, st)
				if _, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash); !errors.Is(err, store.ErrPlanStale) {
					t.Fatal("stale confirm", err)
				}
				if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, *approval); !errors.Is(err, store.ErrPlanStale) {
					t.Fatal("stale commit", err)
				}
				if _, err := st.ReadProblemInputScope(token); err == nil && mutation != "proof" {
					t.Fatal("stale displayed token upgraded")
				}
				if libraryTables(t, st) != before {
					t.Fatal("stale approval changed library")
				}
				for _, id := range []int64{a.ID, b.ID} {
					item, err := st.GetItem(id)
					if err != nil || item.Status != store.StatusReview {
						t.Fatal(item, err)
					}
				}
			})
		}
	}
}

// Scope resolution tests use a provider-owned unresolved seed. Business code
// must not know this provider's encoding.
type scopedResolutionProvider struct{ decisionProvider }

func (p *scopedResolutionProvider) InspectCandidate(c provider.Candidate) matching.CandidateIdentity {
	if c.MatchRef == "unresolved" {
		return matching.CandidateIdentity{State: matching.IdentityUnresolved}
	}
	return p.summaryProvider.InspectCandidate(c)
}

func TestScopedPreparationReusesResolutionAndRejectsMemberArrivalDuringIO(t *testing.T) {
	for _, join := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared resolution", true: "arrival during IO"}[join], func(t *testing.T) {
			s, st, _ := newTestService(t)
			a := problemInput(t, st, "Song")
			problemInput(t, st, "Song")
			scope, _ := problemScope(t, st, a.ID)
			p := &scopedResolutionProvider{}
			s.registry.Register(p)
			p.load = func(request matching.DecisionRequest) ([]matching.TargetResolution, error) {
				if join {
					problemInput(t, st, "Song")
				}
				return []matching.TargetResolution{resolvedFact(t, candidate("release", "real-track", nil))}, nil
			}
			plan, err := s.PrepareScopedCandidate(context.Background(), candidate("release", "unresolved", nil), *scope)
			if join {
				if !errors.Is(err, store.ErrPlanStale) {
					t.Fatal(plan, err)
				}
			} else if err != nil || plan.Seed.Snapshot.Track.Identity.Ref != "real-track" || len(plan.Seed.InputScope.Members) != 2 {
				t.Fatal(plan, err)
			}
			if len(p.requests) != 1 {
				t.Fatal("duplicate resolution requests", len(p.requests))
			}
			if count, err := st.CountTracks(store.TrackFilter{}); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestInputScopeHashAndStrictShape(t *testing.T) {
	s, st, _ := newTestService(t)
	a := problemInput(t, st, "Song")
	problemInput(t, st, "Song")
	scope, _ := problemScope(t, st, a.ID)
	target := seed("new", "new-track", "group", "Album", "Artist")
	target.InputScope = scope
	plan := prepare(t, s, target)
	mutations := map[string]func(*domain.InputScope){
		"kind":             func(v *domain.InputScope) { v.Kind = domain.InputScopeSingle },
		"version":          func(v *domain.InputScope) { v.Version++ },
		"problem":          func(v *domain.InputScope) { v.Problem.ID++ },
		"problem revision": func(v *domain.InputScope) { v.Problem.Revision++ },
		"query":            func(v *domain.InputScope) { v.Problem.QueryRevision++ },
		"candidates":       func(v *domain.InputScope) { v.Problem.CandidateRevision++ },
		"rule":             func(v *domain.InputScope) { v.Problem.RuleVersion++ },
		"proof version":    func(v *domain.InputScope) { v.Problem.ProofVersion++ },
		"proof hash":       func(v *domain.InputScope) { v.Problem.EvidenceHash = "changed" },
		"member":           func(v *domain.InputScope) { v.Members[0].ItemID++ },
		"member revision":  func(v *domain.InputScope) { v.Members[0].MemberRevision++ },
		"input revision":   func(v *domain.InputScope) { v.Members[0].InputRevision++ },
		"profile":          func(v *domain.InputScope) { v.Members[0].ProfileVersion++ },
		"input hash":       func(v *domain.InputScope) { v.Members[0].EvidenceHash = "changed" },
		"subset":           func(v *domain.InputScope) { v.Members = v.Members[:1] },
		"duplicate":        func(v *domain.InputScope) { v.Members[1] = v.Members[0] },
		"reordered":        func(v *domain.InputScope) { v.Members[0], v.Members[1] = v.Members[1], v.Members[0] },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed, err := clone(*plan)
			if err != nil {
				t.Fatal(err)
			}
			mutate(changed.Seed.InputScope)
			hash, err := store.AdmissionHash(changed)
			if err != nil || hash == plan.Hash {
				t.Fatal("scope omitted from hash", err)
			}
			if err := st.CheckInputScope(changed.Seed.InputScope); !errors.Is(err, store.ErrPlanStale) {
				t.Fatal(err)
			}
			changed.Hash = hash // recomputing hash does not grant a server seal
			if _, err := st.CommitAdmission(changed, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
				t.Fatal(err)
			}
		})
	}
	// Returned DTOs are detached from the stored seed/baseline and approval.
	plan.Seed.InputScope.Members[0].ItemID = 999
	stored, err := s.plan(plan.ID, plan.Hash)
	if err != nil || stored.Seed.InputScope.Members[0].ItemID != a.ID {
		t.Fatal(stored, err)
	}
	body, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip domain.AdmissionPlan
	if err := json.Unmarshal(body, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if hash, err := store.AdmissionHash(roundtrip); err != nil || hash != stored.Hash {
		t.Fatal(hash, err)
	}
}

func TestScopedSkipAndLegacySingleNeverAdoptOtherRows(t *testing.T) {
	s, st, _ := newTestService(t)
	target := seed("release", "track", "group", "Album", "Artist")
	existing := commit(t, s, prepare(t, s, target), false)
	a := problemInput(t, st, "Song")
	b := problemInput(t, st, "Song")
	scope, _ := problemScope(t, st, a.ID)
	plan, err := s.PrepareScopedSkipExisting(context.Background(), candidate("release", "track", nil), *scope)
	if err != nil {
		t.Fatal(err)
	}
	problemInput(t, st, "Song")
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
	// A newly prepared legacy operation remains exactly one row.
	single, err := s.PrepareSkipExisting(context.Background(), candidate("release", "track", nil), a.ID, a.Revision)
	if err != nil || len(single.Seed.InputScope.Members) != 1 {
		t.Fatal(single, err)
	}
	commit(t, s, single, false)
	assertExistingInput(t, st, a, existing.TrackID)
	other, err := st.GetItem(b.ID)
	if err != nil || !reflect.DeepEqual(b, other) {
		t.Fatal(other, err)
	}
}
