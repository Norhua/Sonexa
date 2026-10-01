package library

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
	"strconv"
	"strings"
)

func (s *Service) PlanAlbumChange(ctx context.Context, draft domain.EditDraft) (*domain.AlbumChangePlan, error) {
	impact, err := s.PreviewEditSave(ctx, draft)
	if err != nil {
		return nil, err
	}
	if impact.NeedsScope {
		return nil, fmt.Errorf("请先选择共享改动范围")
	}
	target, err := s.editTargetSnapshot(draft, impact)
	if err != nil {
		return nil, err
	}
	sessions := []*domain.EditorSession{}
	for _, id := range impact.SourceTrackIDs {
		session, err := s.store.ReadEditorSession(id)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	owned := map[string]int64{}
	if impact.TargetReleaseID != 0 {
		tracks, err := s.store.ListTracks(store.TrackFilter{ReleaseID: impact.TargetReleaseID})
		if err != nil {
			return nil, err
		}
		for _, track := range tracks {
			if track.MatchRef != nil {
				owned[*track.MatchRef] = track.ID
			}
		}
	}
	return planAlbumRows(draft, impact, sessions, target, owned)
}
func planAlbumRows(draft domain.EditDraft, impact *domain.EditImpact, sources []*domain.EditorSession, target domain.Snapshot, owned map[string]int64) (*domain.AlbumChangePlan, error) {
	result := &domain.AlbumChangePlan{Impact: impact, Rows: []domain.AlbumChangeRow{}, Targets: []domain.AlbumMappingTarget{}}
	if draft.BindingAction != "set" || impact.DetachesTrack {
		return nil, fmt.Errorf("当前方案不是上游换版，请使用逐曲位置预览")
	}
	if target.Release == nil || target.Release.TracklistCompleteness != domain.Complete {
		return nil, fmt.Errorf("请先获取目标发行完整清单，再预览逐曲对应")
	}
	entries := target.Release.Tracklist.Value
	for _, entry := range entries {
		if entry.Track.State != domain.Value {
			continue
		}
		result.Targets = append(result.Targets, mappingTarget(entry, owned))
	}
	choices := map[int64]domain.EditMapping{}
	for _, choice := range draft.Mappings {
		if _, ok := choices[choice.TrackID]; ok {
			return nil, fmt.Errorf("同一曲目只能提交一个对应方案")
		}
		choices[choice.TrackID] = choice
	}
	assigned := map[string][]int{}
	for _, source := range sources {
		var snapshot domain.Snapshot
		if err := json.Unmarshal([]byte(source.SnapshotJSON), &snapshot); err != nil {
			return nil, err
		}
		row := domain.AlbumChangeRow{TrackID: source.TrackID, Before: source.Current, After: source.Current, Stay: true, Choices: []domain.AlbumMappingTarget{}}
		indexes, reason := matchAlbumEntries(snapshot.Track, entries)
		for _, index := range indexes {
			row.Choices = append(row.Choices, mappingTarget(entries[index], owned))
		}
		selected := -1
		if len(indexes) == 1 && reason != "文本与时长建议" {
			selected = indexes[0]
			row.Evidence = reason
		}
		if len(indexes) > 1 {
			row.Conflict = "存在多个对应位置，请明确选择或留在原发行"
		}
		if len(indexes) == 0 {
			row.Evidence = "目标没有可验证对应，保留原发行"
		}
		if reason == "文本与时长建议" {
			row.Evidence = reason
			row.Conflict = "文本与时长只能作为建议，请明确选择或留在原发行"
		}
		if source.TrackID == draft.TrackID && target.Track != nil {
			for i, entry := range entries {
				if entry.Track.State == domain.Value && entry.Track.Value == target.Track.Identity {
					selected = i
					row.Conflict = ""
					row.Evidence = "当前明确选择的歌曲"
					break
				}
			}
		}
		if choice, ok := choices[source.TrackID]; ok {
			delete(choices, source.TrackID)
			selected = -1
			row.Conflict = ""
			row.Evidence = "用户选择保留原发行"
			if !choice.Stay {
				for i, entry := range entries {
					if entry.Track.State == domain.Value && entry.Track.Value.Ref == choice.TargetRef {
						selected = i
						break
					}
				}
				if selected < 0 {
					return nil, fmt.Errorf("对应目标不在当前发行清单中")
				}
				row.Evidence = "用户明确选择"
			}
		}
		if selected >= 0 {
			entry := entries[selected]
			row.TargetRef = entry.Track.Value.Ref
			row.Stay = false
			if id := owned[row.TargetRef]; id != 0 && id != source.TrackID {
				row.Conflict = "目标位置已收藏，请选择其他位置或留在原发行"
			}
			track := trackFromAlbumEntry(entry, *target.Release)
			if target.Track != nil && target.Track.Identity == track.Identity {
				track = *target.Track
			}
			fields, err := domain.EditorFields(domain.Snapshot{Track: &track})
			if err != nil {
				return nil, err
			}
			row.After = domain.EditFields{}
			for k, v := range source.Current {
				row.After[k] = v
			}
			for k, v := range fields {
				if v.State == domain.Unknown {
					continue
				}
				if source.Current[k].Mode != domain.User {
					row.After[k] = v
				}
			}
			// 换版保留用户字段，但位置属于明确选择的目标清单身份。
			row.After["track.discNumber"] = editNumber(entry.DiscNumber)
			row.After["track.trackNumber"] = editNumber(entry.TrackNumber)
			if source.TrackID == draft.TrackID {
				for k, v := range draft.Fields {
					if strings.HasPrefix(k, "track.") {
						row.After[k] = v
					}
				}
			}
			assigned[row.TargetRef] = append(assigned[row.TargetRef], len(result.Rows))
		}
		result.Rows = append(result.Rows, row)
	}
	if len(choices) > 0 {
		return nil, fmt.Errorf("方案含有不在当前影响范围内的曲目")
	}
	for _, indexes := range assigned {
		if len(indexes) > 1 {
			for _, i := range indexes {
				result.Rows[i].Conflict = "多首来源曲目对应同一个目标，必须调整"
			}
		}
	}
	for _, row := range result.Rows {
		if row.Conflict != "" {
			result.HasConflicts = true
		}
	}
	return result, nil
}
func mappingTarget(entry domain.TracklistEntry, owned map[string]int64) domain.AlbumMappingTarget {
	return domain.AlbumMappingTarget{Ref: entry.Track.Value.Ref, Title: entry.Title.Value, DiscNumber: entry.DiscNumber, TrackNumber: entry.TrackNumber, OwnedTrackID: owned[entry.Track.Value.Ref]}
}
func editNumber(value int) domain.EditValue {
	return domain.EditValue{State: domain.Value, ValueJSON: strconv.Itoa(value)}
}
func trackFromAlbumEntry(entry domain.TracklistEntry, release domain.ReleaseSnapshot) domain.TrackSnapshot {
	track := domain.TrackSnapshot{Identity: entry.Track.Value, Release: &release.Identity, Title: entry.Title, Credit: entry.Credit, DiscNumber: domain.Known(entry.DiscNumber), TrackNumber: domain.Known(entry.TrackNumber), Recording: entry.Recording, Identifiers: entry.Identifiers, DurationMS: entry.DurationMS}
	if release.Date.State == domain.Value && len(release.Date.Value) >= 4 {
		if year, err := strconv.Atoi(release.Date.Value[:4]); err == nil && year > 0 {
			track.Year = domain.Known(year)
		}
	}
	return track
}
func matchAlbumEntries(source *domain.TrackSnapshot, entries []domain.TracklistEntry) ([]int, string) {
	if source == nil {
		return nil, ""
	}
	tests := []struct {
		reason string
		match  func(domain.TracklistEntry) bool
	}{
		{"精确歌曲身份", func(e domain.TracklistEntry) bool {
			return e.Track.State == domain.Value && source.Identity.Ref != "" && e.Track.Value == source.Identity
		}},
		{"相同来源录音", func(e domain.TracklistEntry) bool {
			return source.Recording.State == domain.Value && e.Recording.State == domain.Value && source.Recording.Value == e.Recording.Value && source.Recording.Value.Ref != ""
		}},
		{"相同 ISRC", func(e domain.TracklistEntry) bool {
			for _, a := range source.Identifiers.Value {
				if !strings.EqualFold(a.Kind, "ISRC") || a.Value == "" {
					continue
				}
				for _, b := range e.Identifiers.Value {
					if strings.EqualFold(b.Kind, "ISRC") && strings.EqualFold(a.Value, b.Value) {
						return true
					}
				}
			}
			return false
		}},
		{"文本与时长建议", func(e domain.TracklistEntry) bool {
			return source.Title.State == domain.Value && e.Title.State == domain.Value && source.Title.Value != "" && matching.Normalize(source.Title.Value) == matching.Normalize(e.Title.Value) && source.DurationMS.State == domain.Value && e.DurationMS.State == domain.Value && matching.DurationWithinTolerance(source.DurationMS.Value, e.DurationMS.Value)
		}},
	}
	for _, test := range tests {
		found := []int{}
		for i, entry := range entries {
			if entry.Track.State == domain.Value && test.match(entry) {
				found = append(found, i)
			}
		}
		if len(found) > 0 {
			return found, test.reason
		}
	}
	return nil, ""
}
