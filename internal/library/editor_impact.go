package library

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
	"reflect"
	"strings"
)

var editableFields = map[string]string{
	"track.title": "text", "track.credit": "text", "track.year": "number", "track.discNumber": "number", "track.trackNumber": "number", "track.genre": "text", "track.lyrics": "text",
	"track.roles.vocal": "roles", "track.roles.composer": "roles", "track.roles.lyricist": "roles",
	"release.title": "text", "release.credit": "text", "release.primaryType": "type", "release.secondaryTypes": "strings", "release.discTotal": "number", "release.media": "media",
}

func validateEditFields(fields domain.EditFields) error {
	for key, f := range fields {
		kind, ok := editableFields[key]
		if !ok {
			return fmt.Errorf("字段 %s 只读或不存在", key)
		}
		if f.State == domain.Empty {
			continue
		}
		if f.State != domain.Value {
			return fmt.Errorf("字段 %s 必须是明确值或清空", key)
		}
		switch kind {
		case "text", "type":
			var value string
			if err := json.Unmarshal([]byte(f.ValueJSON), &value); err != nil {
				return fmt.Errorf("%s 必须是文本", key)
			}
			if kind == "type" && value != "" && store.NormalizeReleaseType(value) != strings.ToLower(value) {
				return fmt.Errorf("专辑类型无效")
			}
		case "number":
			var value int
			if json.Unmarshal([]byte(f.ValueJSON), &value) != nil || value <= 0 {
				return fmt.Errorf("%s 必须是正整数或留空", key)
			}
		case "strings":
			var values []string
			if json.Unmarshal([]byte(f.ValueJSON), &values) != nil {
				return fmt.Errorf("%s 必须是文本列表", key)
			}
		case "roles":
			var roles []domain.Role
			if json.Unmarshal([]byte(f.ValueJSON), &roles) != nil {
				return fmt.Errorf("角色列表无效")
			}
			for _, role := range roles {
				if strings.TrimSpace(role.Artist.Name) == "" {
					return fmt.Errorf("角色成员名称不能为空")
				}
				if role.Role != "" && "track.roles."+role.Role != key {
					return fmt.Errorf("角色与字段不一致")
				}
			}
		case "media":
			var media []domain.Medium
			if json.Unmarshal([]byte(f.ValueJSON), &media) != nil {
				return fmt.Errorf("每碟曲数必须是列表")
			}
			seen := map[int]bool{}
			for _, m := range media {
				if m.Number <= 0 || seen[m.Number] || m.TrackTotal.State == domain.Value && m.TrackTotal.Value <= 0 {
					return fmt.Errorf("碟号与每碟曲数必须是正整数，碟号不能重复")
				}
				seen[m.Number] = true
			}
		}
	}
	return nil
}
func sameEditValue(a, b domain.EditValue) bool {
	if a.State != b.State {
		return false
	}
	if a.State == domain.Empty {
		return true
	}
	var x, y any
	_ = json.Unmarshal([]byte(a.ValueJSON), &x)
	_ = json.Unmarshal([]byte(b.ValueJSON), &y)
	return reflect.DeepEqual(x, y)
}
func (s *Service) PreviewEditSave(ctx context.Context, draft domain.EditDraft) (*domain.EditImpact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateEditFields(draft.Fields); err != nil {
		return nil, err
	}
	source, err := s.store.ReadEditorSession(draft.TrackID)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(source.Revisions, draft.Revisions) {
		return nil, store.ErrRevisionConflict
	}
	switch draft.CoverAction {
	case "", "keep", "clear":
	case "replace":
		if draft.CoverToken == "" {
			return nil, fmt.Errorf("请先选择封面")
		}
	default:
		return nil, fmt.Errorf("封面动作无效")
	}
	impact := &domain.EditImpact{Source: source, TargetReleaseID: source.ReleaseID, SourceTrackIDs: []int64{draft.TrackID}, TargetTrackIDs: []int64{}}
	shared := draft.CoverAction == "replace"
	if draft.CoverAction == "clear" {
		var cover struct {
			Cleared bool `json:"cleared"`
		}
		_ = json.Unmarshal([]byte(source.Current["release.cover"].ValueJSON), &cover)
		shared = source.ReleaseID != 0 && !cover.Cleared
	}
	for key, value := range draft.Fields {
		if strings.HasPrefix(key, "release.") && !sameEditValue(value, source.Current[key]) {
			shared = true
		}
	}
	switch draft.BindingAction {
	case "", "keep":
	case "clear_track":
		impact.DetachesTrack = source.TrackIdentity != nil
	case "clear_release":
		shared = shared || source.ReleaseIdentity != nil
		impact.DetachesTrack = true
	case "set":
		snapshot, err := s.editPreview(draft.PreviewID)
		if err != nil {
			return nil, err
		}
		if snapshot.Track == nil || snapshot.Track.Identity.Ref == "" {
			return nil, fmt.Errorf("请选择具体歌曲身份")
		}
		if snapshot.Track.Release != nil && (snapshot.Release == nil || *snapshot.Track.Release != snapshot.Release.Identity) {
			return nil, store.ErrSnapshotTarget
		}
		target := snapshot.Release
		if target != nil {
			if source.ReleaseIdentity == nil || *source.ReleaseIdentity != target.Identity {
				shared = true
				impact.LinksNewUpstream = true
				impact.TargetReleaseID = 0
				existing, err := s.store.GetReleaseByRef(string(target.Identity.Provider), target.Identity.Ref)
				if err != nil {
					return nil, err
				}
				if existing != nil {
					impact.TargetReleaseID = existing.ID
				}
			}
		} else if source.ReleaseID != 0 {
			return nil, fmt.Errorf("新的歌曲身份缺少可验证发行，不能保留冲突的原发行绑定")
		}
	default:
		return nil, fmt.Errorf("绑定动作无效")
	}
	impact.SharedChange = shared
	impact.NeedsAlbumConfirmation = shared
	if shared {
		if source.ReleaseID != 0 {
			impact.SourceTrackIDs = append([]int64{}, source.ReleaseTrackIDs...)
		}
		if !impact.LinksNewUpstream {
			impact.TargetReleaseID = 0
			impact.DetachesTrack = true
		}
		impact.CreatesRelease = impact.TargetReleaseID == 0
	}
	if impact.TargetReleaseID != 0 && impact.TargetReleaseID != source.ReleaseID {
		tracks, err := s.store.ListTracks(store.TrackFilter{ReleaseID: impact.TargetReleaseID})
		if err != nil {
			return nil, err
		}
		impact.TargetPositions = map[string]int64{}
		for _, track := range tracks {
			impact.TargetTrackIDs = append(impact.TargetTrackIDs, track.ID)
			current, err := s.store.ReadEditorSession(track.ID)
			if err != nil {
				return nil, err
			}
			var disc, position int
			_ = json.Unmarshal([]byte(current.Current["track.discNumber"].ValueJSON), &disc)
			_ = json.Unmarshal([]byte(current.Current["track.trackNumber"].ValueJSON), &position)
			if disc > 0 && position > 0 {
				impact.TargetPositions[fmt.Sprintf("%d:%d", disc, position)] = track.ID
			}
		}
		impact.PreservesTargetValues = true
		if len(impact.TargetTrackIDs) > 0 {
			existing, err := s.store.ReadEditorSession(impact.TargetTrackIDs[0])
			if err != nil {
				return nil, err
			}
			impact.TargetCurrent = existing.Current
			impact.TargetRevisions = existing.Revisions
		} else {
			current, revisions, err := s.store.ReadEditorReleaseCurrent(impact.TargetReleaseID)
			if err != nil {
				return nil, err
			}
			impact.TargetCurrent = current
			impact.TargetRevisions = revisions
		}
	}
	merged := domain.EditFields{}
	for k, v := range source.Current {
		merged[k] = v
	}
	for k, v := range draft.Fields {
		merged[k] = v
	}
	var discs int
	var media []domain.Medium
	if f := merged["release.discTotal"]; f.State == domain.Value {
		_ = json.Unmarshal([]byte(f.ValueJSON), &discs)
	}
	if f := merged["release.media"]; f.State == domain.Value {
		if err := json.Unmarshal([]byte(f.ValueJSON), &media); err != nil {
			return nil, err
		}
	}
	if discs > 0 && len(media) > 0 && discs != len(media) {
		return nil, fmt.Errorf("每碟曲数列表长度必须与碟数一致，可将未知曲数留空")
	}
	if impact.CreatesRelease || impact.TargetReleaseID != source.ReleaseID {
		snapshot, err := s.editTargetSnapshot(draft, impact)
		if err != nil {
			return nil, err
		}
		membership := snapshot
		membership.Track = &domain.TrackSnapshot{}
		baseline, err := s.store.PreviewAdmission(editAdmissionSeed(draft, impact, membership))
		if err != nil {
			return nil, err
		}
		impact.Admission = &baseline
	}
	return impact, nil
}

// editTargetSnapshot 不把原发行身份复制到纯本地新建发行。
func (s *Service) editTargetSnapshot(draft domain.EditDraft, impact *domain.EditImpact) (domain.Snapshot, error) {
	if draft.BindingAction == "set" && !impact.DetachesTrack {
		return s.editPreview(draft.PreviewID)
	}
	if !impact.CreatesRelease {
		var snapshot domain.Snapshot
		err := json.Unmarshal([]byte(impact.Source.SnapshotJSON), &snapshot)
		return snapshot, err
	}
	fields := domain.EditFields{}
	for k, v := range impact.Source.Current {
		fields[k] = v
	}
	for k, v := range draft.Fields {
		fields[k] = v
	}
	raw := map[string]any{}
	for _, key := range []string{"title", "credit", "primaryType", "secondaryTypes", "discTotal", "media"} {
		field, ok := fields["release."+key]
		if !ok {
			continue
		}
		var value any
		if field.State == domain.Value {
			if err := json.Unmarshal([]byte(field.ValueJSON), &value); err != nil {
				return domain.Snapshot{}, err
			}
		}
		if key == "credit" && field.State == domain.Value {
			value = map[string]any{"text": value}
		}
		entry := map[string]any{"state": field.State}
		if field.State == domain.Value {
			entry["value"] = value
		}
		raw[key] = entry
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return domain.Snapshot{}, err
	}
	var release domain.ReleaseSnapshot
	if err := json.Unmarshal(body, &release); err != nil {
		return domain.Snapshot{}, err
	}
	return domain.Snapshot{Release: &release, Track: &domain.TrackSnapshot{}}, nil
}

func editAdmissionSeed(draft domain.EditDraft, impact *domain.EditImpact, snapshot domain.Snapshot) domain.AdmissionSeed {
	fields := domain.EditFields{}
	for k, v := range impact.Source.Current {
		fields[k] = v
	}
	applyAlbumFields(fields, snapshot, impact, draft.Fields)
	current := &domain.ReleaseSnapshot{}
	if f := fields["release.title"]; f.State == domain.Value {
		current.Title.State = domain.Value
		_ = json.Unmarshal([]byte(f.ValueJSON), &current.Title.Value)
	}
	if f := fields["release.credit"]; f.State == domain.Value {
		current.Credit.State = domain.Value
		_ = json.Unmarshal([]byte(f.ValueJSON), &current.Credit.Value.Text)
	}
	return domain.AdmissionSeed{Snapshot: snapshot, CurrentRelease: current, MergeTargetID: draft.MergeTargetID}
}
