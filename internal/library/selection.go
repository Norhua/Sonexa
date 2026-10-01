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
	Candidate *provider.Candidate   `json:"candidate"`
	Reason    string                `json:"reason"`
	Plan      *domain.AdmissionPlan `json:"plan,omitempty"`
}
type selectionCandidate struct {
	candidate provider.Candidate
	summary   domain.ReleaseSummary
	groups    map[int64]bool
	existing  bool
}

// selectCandidate 是无网络的纯决策；已有发行优先，总数未知不会被当作零。
func selectCandidate(candidates []selectionCandidate) Selection {
	if len(candidates) == 0 {
		return Selection{Reason: ReasonNoMatch}
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
		return Selection{Reason: ReasonGroupConflict}
	}
	first := candidates[0]
	for i := 1; i < len(candidates); i++ {
		next := candidates[i]
		a, b := first.candidate, next.candidate
		if a.Provider != b.Provider || a.RecordingRef == nil || b.RecordingRef == nil || *a.RecordingRef == "" || *a.RecordingRef != *b.RecordingRef {
			return Selection{Reason: ReasonAmbiguous}
		}
		if matching.DefaultWeights().CompareTitle(value(a.Title), value(b.Title)) == matching.MatchNone {
			return Selection{Reason: ReasonAmbiguous}
		}
	}
	positions := map[domain.Identity]string{}
	trackReleases := map[domain.Identity]domain.Identity{}
	for _, c := range candidates {
		trackIdentity := domain.Identity{Provider: c.candidate.Provider, Ref: c.candidate.MatchRef}
		if prior, ok := trackReleases[trackIdentity]; ok && prior != c.summary.Identity {
			return Selection{Reason: ReasonAmbiguous}
		}
		trackReleases[trackIdentity] = c.summary.Identity
		if ref, ok := positions[c.summary.Identity]; ok && ref != c.candidate.MatchRef {
			return Selection{Reason: ReasonAmbiguous}
		}
		positions[c.summary.Identity] = c.candidate.MatchRef
	}
	owned := []selectionCandidate{}
	for _, c := range candidates {
		if c.existing {
			owned = append(owned, c)
		}
	}
	if len(owned) > 0 {
		candidates = owned
	}
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
				return r.Country.State == domain.Value && strings.TrimSpace(r.Country.Value) != "" && strings.ToUpper(r.Country.Value) != "XW"
			}
			if nonWorld(a.summary) != nonWorld(b.summary) {
				return nonWorld(a.summary)
			}
			return candidateKey(a.candidate) < candidateKey(b.candidate)
		})
	}
	selected := candidates[0].candidate
	return Selection{Candidate: &selected}
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
func (s *Service) SelectCandidate(ctx context.Context, scored []matching.ScoredCandidate, itemID int64) (Selection, error) {
	prepared := []selectionCandidate{}
	summaries := map[domain.Identity]domain.ReleaseSummary{}
	seen := map[string]bool{}
	for _, sc := range scored {
		if !sc.Score.HighConf || seen[candidateKey(sc.Candidate)] {
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
		existing := false
		if summary.Identity.Ref != "" {
			release, err := s.store.GetReleaseByRef(string(summary.Identity.Provider), summary.Identity.Ref)
			if err != nil {
				return Selection{}, err
			}
			existing = release != nil
		}
		prepared = append(prepared, selectionCandidate{candidate: sc.Candidate, summary: summary, existing: existing})
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
	ownedCount := 0
	for _, c := range prepared {
		if c.existing {
			ownedCount++
		}
	}
	for i := range prepared {
		c := &prepared[i]
		title, _ := domain.NameEvidence(c.summary.Title, c.summary.Credit)
		groupingMissing := c.summary.Group == nil && title == ""
		comparing := (ownedCount == 0 && len(prepared) > 1) || (ownedCount > 1 && c.existing)
		if groupingMissing || (comparing && c.summary.ReleaseTrackTotal.State != domain.Value) {
			if err := load(c); err != nil {
				return Selection{}, err
			}
		}
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
	}
	selection := selectCandidate(prepared)
	if selection.Candidate == nil {
		return selection, nil
	}
	plan, err := s.PrepareCandidate(ctx, *selection.Candidate, itemID)
	if err != nil {
		return Selection{}, err
	}
	selection.Plan = plan
	if plan.Baseline.Conflict {
		selection.Reason = ReasonGroupConflict
	} else if plan.Baseline.RequiresConfirmation {
		selection.Reason = ReasonNewRelease
	}
	return selection, nil
}
