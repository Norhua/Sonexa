package library

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"strconv"
	"strings"
)

func (s *Service) PlanAlbumChange(ctx context.Context, draft domain.EditDraft) (*domain.AlbumChangePlan, error) {
	impact, err := s.PreviewEditSave(ctx, draft)
	if err != nil {
		return nil, err
	}
	if !impact.SharedChange || impact.UpdatesRelease {
		return nil, fmt.Errorf("没有需要迁入确认的专辑改动")
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
	if target.Release != nil {
		for _, entry := range target.Release.Tracklist.Value {
			if entry.Track.State != domain.Value {
				continue
			}
			id, err := s.store.FindByMatchRef(string(entry.Track.Value.Provider), entry.Track.Value.Ref)
			if err != nil {
				return nil, err
			}
			if id != 0 {
				owned[entry.Track.Value.Ref] = id
			}
		}
	}
	return planAlbumRows(draft, impact, sessions, target, owned)
}
func planAlbumRows(draft domain.EditDraft, impact *domain.EditImpact, sources []*domain.EditorSession, target domain.Snapshot, owned map[string]int64) (*domain.AlbumChangePlan, error) {
	result := &domain.AlbumChangePlan{Impact: impact, Rows: []domain.AlbumChangeRow{}, Targets: []domain.AlbumMappingTarget{}}
	if draft.BindingAction != "set" || impact.DetachesTrack {
		return planLocalAlbumRows(draft, impact, sources, target)
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
		row := domain.AlbumChangeRow{TrackID: source.TrackID, BeforeIdentity: source.TrackIdentity, AfterIdentity: source.TrackIdentity, Revisions: source.Revisions, Before: source.Current, After: source.Current, Stay: true, Choices: []domain.AlbumMappingTarget{}}
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
			if choice.Selected {
				for i, entry := range entries {
					if entry.Track.State == domain.Value && entry.Track.Value.Ref == choice.TargetRef {
						selected = i
						break
					}
				}
				row.Evidence = "用户明确选择"
			}
		}
		choice, confirmed := mappingChoice(draft.Mappings, source.TrackID)
		if !confirmed || !choice.Selected {
			selected = -1
			row.Conflict = ""
		}
		if confirmed && choice.Selected && selected < 0 {
			row.Stay = false
			row.Conflict = "请明确选择目标清单对应"
		}
		if selected >= 0 {
			entry := entries[selected]
			row.TargetRef = entry.Track.Value.Ref
			row.AfterIdentity = &entry.Track.Value
			row.Stay = false
			if id := owned[row.TargetRef]; id != 0 && id != source.TrackID {
				row.Conflict = "目标位置已收藏，请选择其他位置或留在原发行"
			}
			track := trackFromAlbumEntry(entry, *target.Release)
			if target.Track != nil && target.Track.Identity == track.Identity {
				track = *target.Track
			}
			body, err := json.Marshal(track)
			if err != nil {
				return nil, err
			}
			row.TargetSnapshotJSON = string(body)
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
			row.After["track.discNumber"] = editNumber(entry.DiscNumber)
			row.After["track.trackNumber"] = editNumber(entry.TrackNumber)
			applyAlbumFields(row.After, target, impact, draft.Fields)
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
	validateAlbumPositions(result)
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

func mappingChoice(choices []domain.EditMapping, id int64) (domain.EditMapping, bool) {
	for _, choice := range choices {
		if choice.TrackID == id {
			return choice, true
		}
	}
	return domain.EditMapping{}, false
}

func applyAlbumFields(fields domain.EditFields, target domain.Snapshot, impact *domain.EditImpact, edits domain.EditFields) {
	values, _ := domain.EditorFields(target)
	if impact.PreservesTargetValues {
		values = impact.TargetCurrent
	}
	for key, value := range values {
		if strings.HasPrefix(key, "release.") && value.State != domain.Unknown {
			fields[key] = value
		}
	}
	if !impact.PreservesTargetValues {
		for key, value := range edits {
			if strings.HasPrefix(key, "release.") {
				fields[key] = value
			}
		}
	}
}

func planLocalAlbumRows(draft domain.EditDraft, impact *domain.EditImpact, sources []*domain.EditorSession, target domain.Snapshot) (*domain.AlbumChangePlan, error) {
	result := &domain.AlbumChangePlan{Impact: impact, Rows: []domain.AlbumChangeRow{}, Targets: []domain.AlbumMappingTarget{}}
	choices := map[int64]domain.EditMapping{}
	for _, choice := range draft.Mappings {
		if _, found := choices[choice.TrackID]; found {
			return nil, fmt.Errorf("同一曲目只能提交一个迁入方案")
		}
		choices[choice.TrackID] = choice
	}
	for _, source := range sources {
		row := domain.AlbumChangeRow{TrackID: source.TrackID, BeforeIdentity: source.TrackIdentity, AfterIdentity: source.TrackIdentity, Revisions: source.Revisions, Before: source.Current, After: source.Current, Stay: true, Choices: []domain.AlbumMappingTarget{}, Evidence: "未确认迁入，保留原发行"}
		choice, exists := choices[source.TrackID]
		delete(choices, source.TrackID)
		if exists && choice.Selected {
			row.Stay = false
			row.AfterIdentity = nil
			row.Evidence = "明确迁入新建本地专辑，解除不再一致的歌曲绑定"
			row.After = domain.EditFields{}
			for k, v := range source.Current {
				row.After[k] = v
			}
			if source.TrackID == draft.TrackID {
				for k, v := range draft.Fields {
					if strings.HasPrefix(k, "track.") {
						row.After[k] = v
					}
				}
			}
			applyAlbumFields(row.After, target, impact, draft.Fields)
			// A selected local mapping is a complete position proposal; nil explicitly means unknown.
			for key, value := range map[string]*int{"track.discNumber": choice.DiscNumber, "track.trackNumber": choice.TrackNumber} {
				row.After[key] = domain.EditValue{State: domain.Empty}
				if value != nil {
					row.After[key] = editNumber(*value)
				}
			}
		}
		result.Rows = append(result.Rows, row)
	}
	if len(choices) > 0 {
		return nil, fmt.Errorf("方案含有不在原发行中的曲目")
	}
	validateAlbumPositions(result)
	return result, nil
}

func validateAlbumPositions(plan *domain.AlbumChangePlan) {
	positions := map[string][]int{}
	plan.SelectedTrackIDs = []int64{}
	for i := range plan.Rows {
		row := &plan.Rows[i]
		if row.Stay {
			continue
		}
		plan.SelectedTrackIDs = append(plan.SelectedTrackIDs, row.TrackID)
		disc, track := 0, 0
		for key, value := range map[string]*int{"track.discNumber": &disc, "track.trackNumber": &track} {
			field := row.After[key]
			if field.State == domain.Value {
				if json.Unmarshal([]byte(field.ValueJSON), value) != nil || *value <= 0 {
					row.Conflict = "碟号与音轨号必须是正整数或未知"
				}
			}
		}
		var discTotal int
		var media []domain.Medium
		if f := row.After["release.discTotal"]; f.State == domain.Value {
			_ = json.Unmarshal([]byte(f.ValueJSON), &discTotal)
		}
		if f := row.After["release.media"]; f.State == domain.Value {
			_ = json.Unmarshal([]byte(f.ValueJSON), &media)
		}
		if discTotal > 0 && disc > discTotal {
			row.Conflict = "碟号超出目标专辑结构"
		}
		for _, medium := range media {
			if medium.Number == disc && medium.TrackTotal.State == domain.Value && track > medium.TrackTotal.Value {
				row.Conflict = "音轨号超出目标碟曲数"
			}
		}
		if disc > 0 && track > 0 {
			key := fmt.Sprintf("%d:%d", disc, track)
			if plan.Impact != nil {
				if id := plan.Impact.TargetPositions[key]; id != 0 && id != row.TrackID {
					row.Conflict = "目标位置已收藏"
				}
			}
			positions[key] = append(positions[key], i)
		}
	}
	for _, indexes := range positions {
		if len(indexes) > 1 {
			for _, index := range indexes {
				plan.Rows[index].Conflict = "所选歌曲的碟号与音轨号重复，请调整位置或取消迁入"
			}
		}
	}
	for _, row := range plan.Rows {
		if !row.Stay && row.Conflict != "" {
			plan.HasConflicts = true
		}
	}
	plan.CanSubmit = len(plan.SelectedTrackIDs) > 0 && !plan.HasConflicts
}
