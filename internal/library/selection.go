package library

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

const (
	ReasonNoMatch           = "no_match"
	ReasonAmbiguous         = "ambiguous"
	ReasonGroupInsufficient = "group_insufficient"
	ReasonGroupConflict     = "group_conflict"
	ReasonNewRelease        = "new_release"
	ReasonUnknownTotal      = "unknown_total"
)

type Selection struct {
	Reasons            []matching.DecisionReason  `json:"reasons,omitempty"`
	ResolvedCandidates []matching.ScoredCandidate `json:"resolvedCandidates,omitempty"`
	Candidate          *provider.Candidate        `json:"candidate"`
	Reason             string                     `json:"reason"`
	Plan               *domain.AdmissionPlan      `json:"plan,omitempty"`
}
type selectionCandidate struct {
	candidate provider.Candidate
	summary   domain.ReleaseSummary
	groups    map[int64]bool
	existing  bool // concrete release already in the library
	owned     bool // this exact track is currently collected
	ownership matching.LocalMatch
}

// selectionCompatibility runs before preferences or total-count I/O. Owning
// one candidate cannot resolve a different group, content or repeated position.
func selectionCompatibility(candidates []selectionCandidate) string {
	if len(candidates) == 0 {
		return ReasonNoMatch
	}
	connected := map[int]bool{0: true}
	for changed := true; changed; {
		changed = false
		for i, a := range candidates {
			if !connected[i] {
				continue
			}
			for j, b := range candidates {
				if connected[j] {
					continue
				}
				same := domain.SameGroup(a.summary, b.summary)
				for group := range a.groups {
					if b.groups[group] {
						same = true
					}
				}
				if same {
					connected[j] = true
					changed = true
				}
			}
		}
	}
	if len(connected) != len(candidates) {
		return ReasonGroupConflict
	}
	first := candidates[0]
	for i := 1; i < len(candidates); i++ {
		next := candidates[i]
		a, b := first.candidate, next.candidate
		if a.Provider != b.Provider || a.RecordingRef == nil || b.RecordingRef == nil || *a.RecordingRef == "" || *a.RecordingRef != *b.RecordingRef {
			return ReasonAmbiguous
		}
		if e := matching.CompareMetadataText(matching.ParseMetadataText(value(a.Title), matching.MetadataTitle), matching.ParseMetadataText(value(b.Title), matching.MetadataTitle), matching.MetadataTextContext{Field: matching.MetadataTitle, IndependentSupport: true}); !e.Reliable() {
			return ReasonAmbiguous
		}
		if value(a.ArtistName) != "" && value(b.ArtistName) != "" {
			e := matching.CompareMetadataCredit(matching.ParseMetadataCredit(*a.ArtistName), matching.ParseMetadataCredit(*b.ArtistName), matching.MetadataCreditContext{Text: matching.MetadataTextContext{IndependentSupport: true}})
			if !e.Reliable() {
				return ReasonAmbiguous
			}
		}
	}
	positions := map[domain.Identity]string{}
	trackReleases := map[domain.Identity]domain.Identity{}
	for _, c := range candidates {
		trackIdentity := domain.Identity{Provider: c.candidate.Provider, Ref: c.candidate.MatchRef}
		if prior, ok := trackReleases[trackIdentity]; ok && prior != c.summary.Identity {
			return ReasonAmbiguous
		}
		trackReleases[trackIdentity] = c.summary.Identity
		if ref, ok := positions[c.summary.Identity]; ok && ref != c.candidate.MatchRef {
			return ReasonAmbiguous
		}
		positions[c.summary.Identity] = c.candidate.MatchRef
	}
	return ""
}

func preferredCandidates(candidates []selectionCandidate) ([]selectionCandidate, string) {
	for _, preference := range []struct {
		code string
		keep func(selectionCandidate) bool
	}{
		{"prefer_owned_compatible_track", func(c selectionCandidate) bool { return c.owned }},
		{"prefer_existing_compatible_release", func(c selectionCandidate) bool { return c.existing }},
	} {
		subset := []selectionCandidate{}
		for _, c := range candidates {
			if preference.keep(c) {
				subset = append(subset, c)
			}
		}
		if len(subset) > 0 {
			return subset, preference.code
		}
	}
	return append([]selectionCandidate(nil), candidates...), ""
}

// selectCandidate is a pure default preference, not proof of identity.
func selectCandidate(candidates []selectionCandidate) Selection {
	if reason := selectionCompatibility(candidates); reason != "" {
		return Selection{Reason: reason}
	}
	candidates, preference := preferredCandidates(candidates)
	if len(candidates) > 1 {
		for _, c := range candidates {
			if c.summary.ReleaseTrackTotal.State != domain.Value || c.summary.ReleaseTrackTotal.Value <= 0 {
				return Selection{Reason: ReasonUnknownTotal}
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			if a.summary.ReleaseTrackTotal.Value != b.summary.ReleaseTrackTotal.Value {
				return a.summary.ReleaseTrackTotal.Value > b.summary.ReleaseTrackTotal.Value
			}
			nonWorld := func(r domain.ReleaseSummary) bool {
				return r.Country.State == domain.Value && strings.TrimSpace(r.Country.Value) != "" && strings.ToUpper(strings.TrimSpace(r.Country.Value)) != "XW"
			}
			if nonWorld(a.summary) != nonWorld(b.summary) {
				return nonWorld(a.summary)
			}
			return candidateKey(a.candidate) < candidateKey(b.candidate)
		})
	}
	selected := candidates[0].candidate
	selection := Selection{Candidate: &selected}
	if preference != "" {
		selection.Reasons = []matching.DecisionReason{{Code: preference, Detail: "相容候选内的默认选版偏好，不构成身份等价"}}
	}
	return selection
}
func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func candidateKey(c provider.Candidate) string {
	return string(c.Provider) + "\x00" + value(c.ReleaseRef) + "\x00" + c.MatchRef
}
func ptrField[T any](field domain.Field[T]) *T {
	if field.State != domain.Value {
		return nil
	}
	v := field.Value
	return &v
}

// SelectCandidate 只对当前候选中确实缺少决策事实的发行补摘要，不枚举原生组。
func (s *Service) SelectCandidate(ctx context.Context, scored []matching.ScoredCandidate, itemID int64, profile *matching.InputProfile) (Selection, error) {
	if profile == nil || profile.RuleVersion != matching.MetadataRuleVersion {
		return Selection{}, errors.New("缺少当前版本的完整输入证据")
	}
	var revision int64
	if itemID != 0 {
		item, err := s.store.GetItem(itemID)
		if err != nil {
			return Selection{}, err
		}
		if item == nil || item.Status.IsTerminal() {
			return Selection{}, store.ErrPlanStale
		}
		revision = item.Revision
	}
	constrained, err := s.constrainSelection(ctx, scored, *profile)
	if err != nil {
		return Selection{}, err
	}
	if constrained.pending {
		return Selection{Reason: ReasonAmbiguous, ResolvedCandidates: constrained.display, Reasons: constrained.reasons}, nil
	}
	selection, err := s.selectResolvedCandidates(ctx, constrained.eligible, itemID, revision, constrained.summaries, *profile)
	selection.ResolvedCandidates = constrained.display
	selection.Reasons = append(constrained.reasons, selection.Reasons...)
	return selection, err
}

func (s *Service) selectResolvedCandidates(ctx context.Context, scored []matching.ScoredCandidate, itemID, revision int64, summaries map[domain.Identity]domain.ReleaseSummary, input matching.InputProfile) (Selection, error) {
	prepared := []selectionCandidate{}
	sources, err := s.identitySources()
	if err != nil {
		return Selection{}, err
	}
	seen := map[string]bool{}
	for _, sc := range scored {
		if seen[candidateKey(sc.Candidate)] {
			continue
		}
		seen[candidateKey(sc.Candidate)] = true
		seed, err := s.CandidateSeed(sc.Candidate, itemID)
		if err != nil {
			return Selection{}, err
		}
		if seed.Snapshot.Release == nil {
			return Selection{Reason: ReasonGroupInsufficient}, nil
		}
		summary := seed.Snapshot.Release.Summary()
		if known, ok := summaries[summary.Identity]; ok {
			summary = known
		}
		existing := false
		if summary.Identity.Ref != "" {
			release, err := s.store.GetReleaseByRef(string(summary.Identity.Provider), summary.Identity.Ref)
			if err != nil {
				return Selection{}, err
			}
			existing = release != nil
		}
		ownership, err := matching.ResolveLocal(s.store, sources, sc.Candidate)
		if err != nil {
			return Selection{}, err
		}
		prepared = append(prepared, selectionCandidate{candidate: sc.Candidate, summary: summary, existing: existing, owned: ownership.State == matching.OwnershipOwned, ownership: ownership})
	}
	load := func(c *selectionCandidate) error {
		identity := c.summary.Identity
		if identity.Ref == "" {
			return nil
		}
		summary, ok := summaries[identity]
		if !ok {
			p, err := s.provider(identity.Provider)
			if err != nil {
				return err
			}
			loaded, err := p.LoadReleaseSummary(ctx, identity)
			if errors.Is(err, provider.ErrUnsupported) {
				summaries[identity] = c.summary
				return nil
			}
			if err != nil {
				return err
			}
			if loaded == nil || loaded.Identity != identity {
				return store.ErrSnapshotTarget
			}
			summary = *loaded
			summaries[identity] = summary
		}
		c.summary.Title = domain.MergeField(c.summary.Title, summary.Title)
		c.summary.Credit = domain.MergeField(c.summary.Credit, summary.Credit)
		c.summary.ReleaseTrackTotal = domain.MergeField(c.summary.ReleaseTrackTotal, summary.ReleaseTrackTotal)
		c.summary.Country = domain.MergeField(c.summary.Country, summary.Country)
		c.summary.Media = domain.MergeField(c.summary.Media, summary.Media)
		if summary.Group != nil {
			c.summary.Group = summary.Group
		}
		return nil
	}
	refresh := func(c *selectionCandidate) (Selection, error) {
		// A summary is a new source view, not permission to silently replace
		// the facts that passed input filtering. Recheck its candidate view.
		original := c.candidate
		c.candidate.AlbumTitle = ptrField(c.summary.Title)
		if c.summary.Credit.State == domain.Value {
			v := c.summary.Credit.Value.Text
			c.candidate.AlbumCredit = &v
		}
		c.candidate.ReleaseTrackTotal = ptrField(c.summary.ReleaseTrackTotal)
		c.candidate.Country = ptrField(c.summary.Country)
		if c.summary.Group != nil {
			v := c.summary.Group.Ref
			c.candidate.ReleaseGroupRef = &v
		}
		updated, err := s.CandidateFacts(ctx, input, []provider.Candidate{original, c.candidate})
		if err != nil {
			return Selection{}, err
		}
		for _, target := range updated.Targets {
			if target.Identity.Track != (domain.Identity{Provider: c.candidate.Provider, Ref: c.candidate.MatchRef}) {
				continue
			}
			evidence := target.Check(input)
			if evidence.Eligibility != matching.MetadataEligible {
				return Selection{Reason: ReasonAmbiguous, Reasons: evidence.Reasons}, nil
			}
		}
		resolution, err := s.store.ResolveGroup(domain.Snapshot{Release: &domain.ReleaseSnapshot{Identity: c.summary.Identity, Group: c.summary.Group, Title: c.summary.Title, Credit: c.summary.Credit}})
		if err != nil {
			return Selection{}, err
		}
		if len(resolution.Groups) > 1 {
			return Selection{Reason: ReasonGroupConflict}, nil
		}
		if resolution.Insufficient {
			return Selection{Reason: ReasonGroupInsufficient}, nil
		}
		c.groups = map[int64]bool{}
		for _, group := range resolution.Groups {
			c.groups[group.ID] = true
		}
		return Selection{}, nil
	}
	for i := range prepared {
		c := &prepared[i]
		title, _ := domain.NameEvidence(c.summary.Title, c.summary.Credit)
		if c.summary.Group == nil && title == "" {
			if err := load(c); err != nil {
				return Selection{}, err
			}
		}
		if check, err := refresh(c); err != nil || check.Reason != "" {
			return check, err
		}
	}
	if reason := selectionCompatibility(prepared); reason != "" {
		return Selection{Reason: reason}, nil
	}
	preferred, _ := preferredCandidates(prepared)
	needsTotal := map[string]bool{}
	if len(preferred) > 1 {
		for _, c := range preferred {
			needsTotal[candidateKey(c.candidate)] = true
		}
	}
	for i := range prepared {
		c := &prepared[i]
		if !needsTotal[candidateKey(c.candidate)] || (c.summary.ReleaseTrackTotal.State == domain.Value && c.summary.ReleaseTrackTotal.Value > 0) {
			continue
		}
		if err := load(c); err != nil {
			return Selection{}, err
		}
		if check, err := refresh(c); err != nil || check.Reason != "" {
			return check, err
		}
	}
	selection := selectCandidate(prepared)
	if selection.Candidate == nil {
		return selection, nil
	}
	for _, candidate := range prepared {
		if candidateKey(candidate.candidate) == candidateKey(*selection.Candidate) {
			return s.prepareAutomaticTarget(ctx, selection, itemID, revision, candidate.ownership)
		}
	}
	return Selection{}, store.ErrPlanStale
}

// Both local-only and upstream decisions reach this final ownership/admission
// boundary. It consumes evidence revisions, never refreshes a stale decision
// into permission to recreate a deleted/rebound collection.
func (s *Service) prepareAutomaticTarget(ctx context.Context, selection Selection, itemID, revision int64, expected matching.LocalMatch) (Selection, error) {
	var plan *domain.AdmissionPlan
	var err error
	if itemID > 0 && expected.State == matching.OwnershipOwned {
		plan, err = s.PrepareSkipExisting(ctx, *selection.Candidate, itemID, revision)
		if err == nil && (plan.Baseline.ExistingTrackID != expected.TrackID || plan.Baseline.ExistingTrackRevision != expected.Revision || plan.Baseline.ExistingReleaseRevision != expected.ReleaseRevision) {
			err = store.ErrPlanStale
		}
	} else {
		plan, err = s.PrepareCandidate(ctx, *selection.Candidate, itemID)
	}
	if err != nil {
		return Selection{}, err
	}
	if itemID > 0 && plan.Seed.ItemRevision != revision {
		return Selection{}, store.ErrPlanStale
	}
	selection.Plan = plan
	if plan.Baseline.Conflict {
		selection.Reason = ReasonGroupConflict
	} else if plan.Baseline.RequiresConfirmation {
		selection.Reason = ReasonNewRelease
	}
	return selection, nil
}
