package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// FieldRow 是编辑界面中的一行字段。
//
// 基本信息与丰富信息两组用同一套「填充 / 保持」控件。
type FieldRow struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group"` // basic | rich
	// Current 是当前值；nil 表示缺失，界面显示占位短横线。
	Current *string `json:"current"`
	// Upstream 是上游值；nil 表示上游未提供该字段。
	Upstream *string `json:"upstream"`
	// Source 是该字段的来源：provider id / user / import。
	Source string `json:"source"`
	// UserLocked 为真时显示「用户」徽标且默认不勾选填充。
	UserLocked bool `json:"userLocked"`
	// Same 为真时标注「= 相同」。
	Same bool `json:"same"`
	// FillByDefault 是填充勾选的默认状态。
	FillByDefault bool `json:"fillByDefault"`
	// ReleaseScoped 为真时该字段属于歌集，改一次对该歌集的全部曲目生效。
	// 界面据此加一行说明，并对「歌集名」走二次确认（见 PlanAlbumRename）。
	ReleaseScoped bool `json:"releaseScoped"`
}

// releaseScopedFields 是属于歌集而非曲目的字段。
//
// 它们只存在歌集上一份：同一张专辑的十二首曲目没有理由各存一份载体与国家，
// 而此前它们逐曲目复制，改其中一首另外十一首不动。
var releaseScopedFields = map[string]bool{
	"album": true, "label": true, "catalogNumber": true,
	"mediumFormat": true, "country": true, "primaryType": true, "year": true,
}

// EditSession 是编辑界面的内容。
type EditSession struct {
	TrackID int64 `json:"trackId"`
	// MatchLabel 是当前匹配来源的可读描述。
	MatchLabel  string     `json:"matchLabel"`
	MatchSource *string    `json:"matchSource"`
	MatchRef    *string    `json:"matchRef"`
	Fields      []FieldRow `json:"fields"`
}

// GetEditSession 打开编辑界面。
func (a *App) GetEditSession(trackID int64) (*EditSession, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.buildEditSession(trackID, nil)
}

// RematchTrack 从编辑界面发起重新匹配。
//
// 这是重新匹配的唯一入口——浏览专辑/艺术家视图与本地搜索都不触发重新匹配。
func (a *App) RematchTrack(trackID int64, providerID, title, artist, album, country string) (*EditSession, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil, fmt.Errorf("%s", gate.Reason)
	}

	p, err := a.registry.Get(provider.ProviderID(providerID))
	if err != nil {
		return nil, err
	}
	if err := p.IsConfigured(); err != nil {
		return nil, err
	}

	// 与导入、搜索页、复核改搜同一条检索路径；用户在等，走交互式优先级
	done := a.beginInteractive()
	defer done()
	ctx := provider.WithPriority(context.Background(), provider.PriorityInteractive)
	scored, _, err := a.matchUpstream(ctx, p, matching.Input{
		Title: title, Artist: artist, Album: album, Country: country,
	})
	if err != nil {
		return nil, err
	}
	if len(scored) == 0 {
		return a.buildEditSession(trackID, nil)
	}
	// 候选池已按置信度降序
	return a.buildEditSession(trackID, &scored[0].Candidate)
}

// buildEditSession 组装编辑界面的字段行。
func (a *App) buildEditSession(trackID int64, upstream *provider.Candidate) (*EditSession, error) {
	track, err := a.store.GetTrackView(trackID)
	if err != nil {
		return nil, err
	}
	if track == nil {
		return nil, fmt.Errorf("曲目不存在")
	}
	meta, err := a.store.GetMetadata(trackID)
	if err != nil {
		return nil, err
	}

	provenance := map[string]string{}
	locked := map[string]bool{}
	if meta != nil {
		if meta.ProvenanceJSON != nil {
			json.Unmarshal([]byte(*meta.ProvenanceJSON), &provenance)
		}
		if meta.LockedJSON != nil {
			json.Unmarshal([]byte(*meta.LockedJSON), &locked)
		}
	}

	session := &EditSession{TrackID: trackID}
	if meta != nil {
		session.MatchSource = meta.MatchSource
		session.MatchRef = meta.MatchRef
		session.MatchLabel = matchLabel(meta)
	}

	add := func(key, label, group string, current, up *string, lockedByUser bool, source string) {
		row := FieldRow{
			Key: key, Label: label, Group: group,
			Current: current, Upstream: up,
			Source: source, UserLocked: lockedByUser,
			// 发行级字段改一次对该歌集的全部曲目生效，界面要说明这一点
			ReleaseScoped: releaseScopedFields[key],
		}
		row.Same = current != nil && up != nil && *current == *up
		// source = user 的字段默认不勾选填充，需用户逐项点击；
		// 其余字段默认勾选。
		row.FillByDefault = !lockedByUser && up != nil && !row.Same
		session.Fields = append(session.Fields, row)
	}

	// 曲名、署名、歌集名
	var upTitle, upArtist, upAlbum *string
	if upstream != nil {
		upTitle, upArtist, upAlbum = upstream.Title, upstream.ArtistName, upstream.AlbumTitle
	}
	add("title", "曲名", "basic", &track.Title, upTitle, track.TitleLocked, sourceOrDefault(provenance["title"], track.TitleLocked))
	add("artist", "艺术家", "basic", track.Artist, upArtist, track.ArtistLocked, sourceOrDefault(provenance["credit"], track.ArtistLocked))
	add("album", "歌集", "basic", track.Album, upAlbum, track.AlbumLocked, sourceOrDefault(provenance["album"], track.AlbumLocked))

	// 丰富信息：逐字段来源与用户锁
	type richField struct {
		key, label string
		current    *string
		upstream   *string
	}
	var fields []richField
	if meta != nil {
		fields = []richField{
			{"year", "年份", intToStr(meta.Year), intToStr(candidateYear(upstream))},
			{"trackNumber", "音轨号", intToStr(meta.TrackNumber), intToStr(candidateTrackNo(upstream))},
			{"discNumber", "碟号", intToStr(meta.DiscNumber), intToStr(candidateDiscNo(upstream))},
			{"durationMs", "时长", intToStr(meta.DurationMS), intToStr(candidateDuration(upstream))},
			{"genre", "风格", meta.Genre, nil},
			{"comment", "注释", meta.Comment, nil},
			{"label", "厂牌", meta.Label, nil},
			{"catalogNumber", "目录号", meta.CatalogNumber, nil},
			{"mediumFormat", "载体", meta.MediumFormat, candidateMedium(upstream)},
			{"country", "国家", meta.Country, candidateCountry(upstream)},
			{"primaryType", "类型", meta.PrimaryType, candidatePrimaryType(upstream)},
		}
	} else {
		fields = []richField{
			{"year", "年份", nil, intToStr(candidateYear(upstream))},
			{"trackNumber", "音轨号", nil, intToStr(candidateTrackNo(upstream))},
			{"mediumFormat", "载体", nil, candidateMedium(upstream)},
			{"country", "国家", nil, candidateCountry(upstream)},
		}
	}
	for _, f := range fields {
		src := provenance[f.key]
		add(f.key, f.label, "rich", f.current, f.upstream, locked[f.key], src)
	}
	return session, nil
}

// sourceOrDefault 在无记录时按锁状态推断来源。
func sourceOrDefault(recorded string, lockedByUser bool) string {
	if recorded != "" {
		return recorded
	}
	if lockedByUser {
		return "user"
	}
	return ""
}

func matchLabel(m *store.TrackMetadata) string {
	if m.MatchSource == nil || m.MatchRef == nil {
		return "未匹配"
	}
	label := *m.MatchSource
	if m.Title != "" {
		label += " · " + m.Title
	}
	if m.Year != nil {
		label += fmt.Sprintf(" (%d", *m.Year)
		if m.MediumFormat != nil {
			label += ", " + *m.MediumFormat
		}
		if m.Country != nil {
			label += ", " + *m.Country
		}
		label += ")"
	}
	return label
}

// ApplyEdits 保存编辑结果。
//
// 填充勾选在用户确认保存后才写入；取消则丢弃全部未保存改动（前端不调用本方法）。
// 填充后该字段来源改为对应上游并清除用户锁；
// 用户手动修改后来源置为 user 并加锁。
// albumRenameMode 为空时，若 edits 含「歌集名」且该歌集下不止一首曲目，
// 本方法拒绝保存并要求界面先走 PlanAlbumRename——歌集是共享实体，
// 改名会波及同专辑的其他曲目，不该在用户没看见影响范围时发生。
func (a *App) ApplyEdits(trackID int64, edits map[string]string, sources map[string]string,
	matchSource, matchRef *string, albumRenameMode string) error {
	if err := a.ready(); err != nil {
		return err
	}

	meta, err := a.store.GetMetadata(trackID)
	if err != nil {
		return err
	}
	provenance := map[string]string{}
	locked := map[string]bool{}
	if meta != nil {
		if meta.ProvenanceJSON != nil {
			json.Unmarshal([]byte(*meta.ProvenanceJSON), &provenance)
		}
		if meta.LockedJSON != nil {
			json.Unmarshal([]byte(*meta.LockedJSON), &locked)
		}
	}

	// 曲名与署名
	var title, credit *string
	byUser := false
	for key, value := range edits {
		v := value
		switch key {
		case "title":
			title = &v
		case "artist", "credit":
			credit = &v
		}
		src := sources[key]
		if src == "" || src == "user" {
			byUser = true
			provenance[key] = "user"
			locked[key] = true
		} else {
			// 填充：来源改为对应上游并清除用户锁
			provenance[key] = src
			delete(locked, key)
		}
	}
	if title != nil || credit != nil {
		if err := a.store.UpdateTrackFields(trackID, title, credit, byUser); err != nil {
			return err
		}
	}

	// 曲目级字段逐字段写入
	if err := a.store.ApplyMetadataEdits(trackID, edits, provenance, locked); err != nil {
		return err
	}

	// 歌集名：共享实体，改名要先确认影响范围
	if newAlbum, ok := edits["album"]; ok {
		plan, err := a.PlanAlbumRename(trackID, newAlbum)
		if err != nil {
			return err
		}
		mode := albumRenameMode
		if mode == "" {
			if plan.NeedsConfirm {
				return fmt.Errorf("这张歌集下有 %d 首曲目，请先选择改动的影响范围", plan.TrackCount)
			}
			mode = AlbumRenameThisRelease
		}
		if err := a.ApplyAlbumRename(trackID, newAlbum, mode); err != nil {
			return err
		}
		// 归属可能已经变了，后面的发行级字段要写到新的那一条上
		if meta, err = a.store.GetMetadata(trackID); err != nil {
			return err
		}
	}

	// 其余发行级字段落在歌集上：一处改动对该歌集的全部曲目生效。
	if meta != nil && meta.ReleaseID != nil {
		releaseEdits := map[string]string{}
		for key, value := range edits {
			switch key {
			case "label", "catalogNumber", "mediumFormat", "country", "primaryType",
				"discTotal", "trackTotal":
				releaseEdits[key] = value
			}
		}
		if len(releaseEdits) > 0 {
			if err := a.store.UpdateReleaseFields(*meta.ReleaseID, releaseEdits,
				provenance, locked); err != nil {
				return err
			}
		}
	}

	// 匹配溯源
	if matchSource != nil && matchRef != nil {
		if err := a.validateMatchRef(provider.ProviderID(*matchSource), *matchRef); err != nil {
			return err
		}
		if err := a.store.SetProvenance(trackID, *matchSource, *matchRef); err != nil {
			return err
		}
	}

	a.notifyLibraryChanged()
	return nil
}

// ClearProvenance 清空匹配溯源。字段值本身不变。
func (a *App) ClearProvenance(trackID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.ClearProvenance(trackID); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// SetProvenanceManual 手动填入匹配标识，需通过格式校验。
func (a *App) SetProvenanceManual(trackID int64, providerID, ref string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.validateMatchRef(provider.ProviderID(providerID), ref); err != nil {
		return err
	}
	if err := a.store.SetProvenance(trackID, providerID, ref); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// --- 候选字段取值小工具 ---

func candidateYear(c *provider.Candidate) *int {
	if c == nil {
		return nil
	}
	return c.Year
}
func candidateTrackNo(c *provider.Candidate) *int {
	if c == nil {
		return nil
	}
	return c.TrackNumber
}
func candidateDiscNo(c *provider.Candidate) *int {
	if c == nil {
		return nil
	}
	return c.DiscNumber
}
func candidateDuration(c *provider.Candidate) *int {
	if c == nil {
		return nil
	}
	return c.DurationMS
}
func candidateMedium(c *provider.Candidate) *string {
	if c == nil {
		return nil
	}
	return c.MediumFormat
}
func candidateCountry(c *provider.Candidate) *string {
	if c == nil {
		return nil
	}
	return c.Country
}
func candidatePrimaryType(c *provider.Candidate) *string {
	if c == nil {
		return nil
	}
	return c.PrimaryType
}

func intToStr(v *int) *string {
	if v == nil {
		return nil
	}
	s := fmt.Sprintf("%d", *v)
	return &s
}

var _ = time.Now

func (a *App) validateMatchRef(source provider.ProviderID, ref string) error {
	p, err := a.registry.Get(source)
	if err != nil {
		return err
	}
	_, err = p.Refs().ParseTrack(ref, "")
	return err
}
