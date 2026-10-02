package domain

import "encoding/json"

// EditValue 是稳定的跨语言字段信封。ValueJSON 保存原始标量、数组或对象，
// 避免 Wails v2 为泛型切片生成错误的构造转换；未知与明确空值独立于 JSON null。
type EditValue struct {
	State     FieldState `json:"state"`
	ValueJSON string     `json:"valueJson"`
	Mode      ValueMode  `json:"mode"`
	Source    string     `json:"source"`
}
type EditFields map[string]EditValue

// EditorSession 是同一读事务的当前值、有效快照和关系基线；不发出网络请求。
type EditorSession struct {
	TrackID            int64            `json:"trackId"`
	ReleaseID          int64            `json:"releaseId"`
	GroupID            int64            `json:"groupId"`
	TrackIdentity      *Identity        `json:"trackIdentity"`
	ReleaseIdentity    *Identity        `json:"releaseIdentity"`
	Current            EditFields       `json:"current"`
	Upstream           EditFields       `json:"upstream"`
	SnapshotJSON       string           `json:"snapshotJson"`
	Revisions          map[string]int64 `json:"revisions"`
	ReleaseTrackIDs    []int64          `json:"releaseTrackIds"`
	OtherReleaseTracks int              `json:"otherReleaseTracks"`
	OtherGroupTracks   int              `json:"otherGroupTracks"`
	CoverRevision      int64            `json:"coverRevision"`
}

type EditPreview struct {
	ID              string     `json:"id"`
	ExpiresAt       string     `json:"expiresAt"`
	SnapshotJSON    string     `json:"snapshotJson"`
	Fields          EditFields `json:"fields"`
	TrackIdentity   *Identity  `json:"trackIdentity"`
	ReleaseIdentity *Identity  `json:"releaseIdentity"`
}

func EditorFields(snapshot Snapshot) (EditFields, error) {
	out := EditFields{}
	for _, layer := range []struct {
		prefix string
		value  any
	}{{"track", snapshot.Track}, {"release", snapshot.Release}} {
		body, err := json.Marshal(layer.value)
		if err != nil {
			return nil, err
		}
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal(body, &fields); err != nil {
			return nil, err
		}
		for key, raw := range fields {
			if key == "roles" {
				roles := map[string]json.RawMessage{}
				if err := json.Unmarshal(raw, &roles); err != nil {
					return nil, err
				}
				for role, raw := range roles {
					var field struct {
						State FieldState      `json:"state"`
						Value json.RawMessage `json:"value"`
					}
					if err := json.Unmarshal(raw, &field); err != nil {
						return nil, err
					}
					out[layer.prefix+".roles."+role] = EditValue{State: field.State, ValueJSON: string(field.Value)}
				}
				continue
			}
			var field struct {
				State FieldState      `json:"state"`
				Value json.RawMessage `json:"value"`
			}
			if json.Unmarshal(raw, &field) != nil || field.State == "" {
				continue
			}
			if key == "credit" && field.State == Value {
				var credit Credit
				if err := json.Unmarshal(field.Value, &credit); err != nil {
					return nil, err
				}
				field.Value, _ = json.Marshal(credit.Text)
			}
			out[layer.prefix+"."+key] = EditValue{State: field.State, ValueJSON: string(field.Value)}
		}
	}
	return out, nil
}

// EditDraft 只携带触碰字段和明确的绑定/迁入意图；预览 token 指向服务端暂存事实。
type EditDraft struct {
	TrackID       int64            `json:"trackId"`
	Revisions     map[string]int64 `json:"revisions"`
	Fields        EditFields       `json:"fields"`
	BindingAction string           `json:"bindingAction"` // keep | set | clear_track | clear_release
	PreviewID     string           `json:"previewId"`
	CoverAction   string           `json:"coverAction"` // keep | clear | replace
	CoverToken    string           `json:"coverToken"`
	Mappings      []EditMapping    `json:"mappings"`
	MergeTargetID int64            `json:"mergeTargetId"`
}
type EditMapping struct {
	TrackID     int64  `json:"trackId"`
	TargetRef   string `json:"targetRef"`
	Selected    bool   `json:"selected"`
	DiscNumber  *int   `json:"discNumber"`
	TrackNumber *int   `json:"trackNumber"`
}
type EditImpact struct {
	Admission              *AdmissionBaseline `json:"admission"`
	Source                 *EditorSession     `json:"source"`
	NeedsAlbumConfirmation bool               `json:"needsAlbumConfirmation"`
	TargetCurrent          EditFields         `json:"targetCurrent"`
	LinksNewUpstream       bool               `json:"linksNewUpstream"`
	SharedChange           bool               `json:"sharedChange"`
	CreatesRelease         bool               `json:"createsRelease"`
	DetachesTrack          bool               `json:"detachesTrack"`
	TargetReleaseID        int64              `json:"targetReleaseId"`
	TargetTrackIDs         []int64            `json:"targetTrackIds"`
	PreservesTargetValues  bool               `json:"preservesTargetValues"`
	SourceTrackIDs         []int64            `json:"sourceTrackIds"`
}

type AlbumMappingTarget struct {
	Ref          string `json:"ref"`
	Title        string `json:"title"`
	DiscNumber   int    `json:"discNumber"`
	TrackNumber  int    `json:"trackNumber"`
	OwnedTrackID int64  `json:"ownedTrackId"`
}
type AlbumChangeRow struct {
	BeforeIdentity *Identity            `json:"beforeIdentity"`
	AfterIdentity  *Identity            `json:"afterIdentity"`
	Revisions      map[string]int64     `json:"revisions"`
	TrackID        int64                `json:"trackId"`
	Before         EditFields           `json:"before"`
	After          EditFields           `json:"after"`
	TargetRef      string               `json:"targetRef"`
	Evidence       string               `json:"evidence"`
	Stay           bool                 `json:"stay"`
	Conflict       string               `json:"conflict"`
	Choices        []AlbumMappingTarget `json:"choices"`
}
type AlbumChangePlan struct {
	Impact           *EditImpact          `json:"impact"`
	Rows             []AlbumChangeRow     `json:"rows"`
	Targets          []AlbumMappingTarget `json:"targets"`
	HasConflicts     bool                 `json:"hasConflicts"`
	SelectedTrackIDs []int64              `json:"selectedTrackIds"`
	CanSubmit        bool                 `json:"canSubmit"`
}
