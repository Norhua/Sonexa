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
	// ReasonNewRelease：需要新增发行且不满足自动条件。
	ReasonNewRelease = "new_release"
	// ReasonSongUnresolved：无法确认、或疑似组内已有发行提供这首歌。
	ReasonSongUnresolved = "song_unresolved"
	ReasonUnknownTotal   = "unknown_total"
)

type Selection struct {
	Candidate *provider.Candidate   `json:"candidate"`
	Reason    string                `json:"reason"`
	Plan      *domain.AdmissionPlan `json:"plan,omitempty"`
	// Substitute 非空表示计划已改为准入组内已有发行的这条清单，而不是候选所在的发行。
	// 之后的已拥有判重、补全、已解析身份与日志都要改用它的身份。
	Substitute *domain.SongProvider `json:"substitute,omitempty"`
}
type selectionCandidate struct {
	candidate provider.Candidate
	summary   domain.ReleaseSummary
	groups    map[int64]bool
	existing  bool
	// refs 是该发行已有的曲目引用数（含同步专属曲目）。
	refs int64
}

// selectCandidate 是无网络的纯决策；先检查曲目/组歧义，再优先本地已有
// 曲目引用的发行（包含同步专属）。发行存在不证明歌曲相同，总数未知不当零。
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
	existing := []selectionCandidate{}
	for _, c := range candidates {
		if c.existing {
			existing = append(existing, c)
		}
	}
	if len(existing) > 0 {
		candidates = existing
	}
	if len(candidates) > 1 {
		for _, c := range candidates {
			if c.summary.ReleaseTrackTotal.State != domain.Value || c.summary.ReleaseTrackTotal.Value <= 0 {
				return Selection{Reason: ReasonUnknownTotal}
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			// 曲目引用多的发行优先于总曲数多的：共同歌曲持续落在同一个发行上。
			if a.refs != b.refs {
				return a.refs > b.refs
			}
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
	keys := []store.CandidateLocalKey{}
	for _, sc := range matching.AutomaticCandidates(scored) {
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
		keys = append(keys, store.CandidateLocalKey{Provider: string(summary.Identity.Provider), ReleaseRef: summary.Identity.Ref})
		prepared = append(prepared, selectionCandidate{candidate: sc.Candidate, summary: summary})
	}
	// 与候选标记共用精确身份批量查询；不把仅缓存/预览的空发行作为偏好，
	// 也不因其曲目尚未加入音乐库而排除真实发行。
	states, err := s.store.CandidateLocalStates(keys)
	if err != nil {
		return Selection{}, err
	}
	for i, state := range states {
		prepared[i].existing = state.ReleaseHasTracks
		prepared[i].refs = state.ReleaseTracks
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
	existingCount := 0
	for _, c := range prepared {
		if c.existing {
			existingCount++
		}
	}
	for i := range prepared {
		c := &prepared[i]
		title, _ := domain.NameEvidence(c.summary.Title, c.summary.Credit)
		groupingMissing := c.summary.Group == nil && title == ""
		comparing := (existingCount == 0 && len(prepared) > 1) || (existingCount > 1 && c.existing)
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
	selectedSeed, err := s.CandidateSeed(*selection.Candidate, itemID)
	if err != nil {
		return Selection{}, err
	}
	selectedSeed.AutomaticImport = itemID != 0
	plan, err := s.PrepareAdmission(ctx, selectedSeed)
	if err != nil {
		return Selection{}, err
	}
	selection.Plan = plan
	if plan.Baseline.Conflict {
		selection.Reason = ReasonGroupConflict
		return selection, nil
	}
	selection, err = s.resolveGroupSong(ctx, selection, selectedSeed)
	if err != nil {
		return Selection{}, err
	}
	if selection.Reason == "" && selection.Plan.Baseline.RequiresConfirmation {
		selection.Reason = ReasonNewRelease
	}
	return selection, nil
}
