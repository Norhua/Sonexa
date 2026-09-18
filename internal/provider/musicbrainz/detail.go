package musicbrainz

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// FetchDetail 以单个请求取回全部所需信息。
//
// 1 req/s 下必须一次拉满（见 detailInc）。响应中已包含该 release 的完整曲目清单，
// 因此 Detail.Tracklist 一并填好，调用方不需要再花一次配额去取。
func (p *Provider) FetchDetail(ctx context.Context, ref provider.Ref) (*provider.Detail, error) {
	if err := p.IsConfigured(); err != nil {
		return nil, err
	}
	if ref.ID == "" {
		return nil, fmt.Errorf("缺少 release 标识")
	}

	params := map[string]string{"fmt": "json", "inc": detailInc}
	body, err := p.client.Get(ctx, provider.GetRequest{
		Provider:   provider.MusicBrainz,
		Endpoint:   "/ws/2/release/" + ref.ID,
		BaseURL:    p.cfg.ServerURL,
		Params:     params,
		ParamsHash: p.hash(mergeParams(params, map[string]string{"id": ref.ID})),
		TTL:        0, // CC0，可长期留存
	})
	if err != nil {
		return nil, err
	}

	var rel releaseDetail
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("解析 MusicBrainz 详情响应失败: %w", err)
	}
	return parseDetailTarget(&rel, ref.TrackRef)
}

func parseDetailTarget(rel *releaseDetail, trackRef string) (*provider.Detail, error) {
	d := ParseDetail(rel, trackRef)
	if trackRef != "" && d.MatchRef == "" {
		return nil, fmt.Errorf("发行 %s 中未找到目标曲目 %s", rel.ID, trackRef)
	}
	return d, nil
}

// ParseDetail 把详情响应解析为 Detail。导出以便金样例测试离线重放。
//
// trackRef 为空只解析发行和清单；非空时必须精确匹配 track MBID，不回退首曲。
// MatchRef 仅在实际定位成功后赋值，调用方可区分“未找到”与“字段缺失”。
func ParseDetail(rel *releaseDetail, trackRef string) *provider.Detail {
	d := &provider.Detail{
		Provider:   provider.MusicBrainz,
		AlbumTitle: strPtr(rel.Title),
		ReleaseRef: strPtr(rel.ID),
		FetchedAt:  time.Now().UTC(),
	}

	if credit := joinArtistCredit(rel.ArtistCredit); credit != "" {
		d.AlbumCredit = &credit
	}
	if rel.Status != "" {
		d.ReleaseStatus = strPtr(rel.Status)
	}
	if rel.Country != "" {
		d.Country = strPtr(rel.Country)
	}
	if rel.Date != "" {
		d.ReleaseDate = strPtr(rel.Date)
		d.Year = parseYear(rel.Date)
	}
	if rel.Annotation != "" {
		d.Comment = strPtr(rel.Annotation)
	}
	if rel.ReleaseGroup != nil {
		d.ReleaseGroupRef = strPtr(rel.ReleaseGroup.ID)
		d.PrimaryType = strPtr(rel.ReleaseGroup.PrimaryType)
		d.SecondaryTypes = rel.ReleaseGroup.SecondaryTypes
		d.FirstReleaseDate = strPtr(rel.ReleaseGroup.FirstReleaseDate)
	}
	if len(rel.LabelInfo) > 0 {
		if rel.LabelInfo[0].Label != nil {
			d.Label = strPtr(rel.LabelInfo[0].Label.Name)
		}
		d.CatalogNumber = strPtr(rel.LabelInfo[0].CatalogNumber)
	}
	if len(rel.Genres) > 0 {
		d.Genre = strPtr(topGenre(rel.Genres))
	}
	if rel.MediumCount > 0 {
		total := rel.MediumCount
		d.DiscTotal = &total
	} else if len(rel.Media) > 0 {
		total := len(rel.Media)
		d.DiscTotal = &total
	}

	// 清单：响应中本就包含，顺带解析，不额外花请求
	d.Tracklist = parseTracklist(rel)
	// 歌集总曲数是全部碟的总和，不是目标曲目所在那张碟的曲数。
	total := 0
	for _, med := range rel.Media {
		if med.TrackCount > 0 {
			total += med.TrackCount
		} else {
			total += len(med.TrackList())
		}
		if d.MediumFormat == nil {
			d.MediumFormat = strPtr(med.Format)
		}
	}
	if total > 0 {
		d.TrackTotal = &total
	}

	// 定位目标曲目位置，取其曲目级字段；未指定/未命中绝不取首曲或借用署名。
	med, trk, ok := findTrack(rel, trackRef)
	if !ok {
		return d
	}
	d.MatchRef = trk.ID
	d.Title = strPtr(firstNonEmpty(trk.Title, recordingTitle(trk)))
	if credit := joinArtistCredit(trk.ArtistCredit); credit != "" {
		d.TrackCredit = &credit
	}
	if trk.Position > 0 {
		pos := trk.Position
		d.TrackNumber = &pos
	}
	if trk.Number != "" {
		d.TrackNumberText = strPtr(trk.Number)
	}
	if med.Position > 0 {
		disc := med.Position
		d.DiscNumber = &disc
	}
	d.MediumFormat = strPtr(med.Format)
	if trk.Length != nil {
		d.DurationMS = trk.Length
	}
	if trk.Recording != nil {
		d.RecordingRef = strPtr(trk.Recording.ID)
		if len(trk.Recording.ISRCs) > 0 {
			d.ISRC = strPtr(trk.Recording.ISRCs[0])
		}
		if d.TrackCredit == nil {
			if credit := joinArtistCredit(trk.Recording.ArtistCredit); credit != "" {
				d.TrackCredit = &credit
			}
		}
		d.Artists = parseRecordingArtists(trk.Recording)
		if workRef := firstWorkID(trk.Recording); workRef != "" {
			d.WorkRef = strPtr(workRef)
		}
	}

	// 结构化边界优先级与 TrackCredit 完全一致。
	credits := trk.ArtistCredit
	if joinArtistCredit(credits) == "" && trk.Recording != nil {
		credits = trk.Recording.ArtistCredit
	}
	if joinArtistCredit(credits) == "" {
		credits = rel.ArtistCredit
	}
	d.Credits = parseCreditArtists(credits)

	// 发行级署名兜底为曲目级署名
	if d.TrackCredit == nil && d.AlbumCredit != nil {
		d.TrackCredit = d.AlbumCredit
	}
	return d
}

// parseTracklist 解析该 release 的完整曲目清单。
//
// inc=recordings+media 使响应已含 media[].tracks[]，
// 含 track MBID / 曲序 / 标题 / 时长 / recording MBID——清单是免费的。
func parseTracklist(rel *releaseDetail) []provider.TracklistEntry {
	var out []provider.TracklistEntry
	for _, med := range rel.Media {
		disc := med.Position
		if disc == 0 {
			disc = 1
		}
		for _, trk := range med.TrackList() {
			entry := provider.TracklistEntry{
				DiscNumber:  disc,
				TrackNumber: trk.Position,
				TrackRef:    strPtr(trk.ID),
				Title:       firstNonEmpty(trk.Title, recordingTitle(trk)),
				DurationMS:  trk.Length,
			}
			if trk.Number != "" {
				entry.TrackNumberText = strPtr(trk.Number)
			}
			if credit := joinArtistCredit(trk.ArtistCredit); credit != "" {
				entry.TrackCredit = &credit
			}
			if trk.Recording != nil {
				entry.RecordingRef = strPtr(trk.Recording.ID)
				if entry.TrackCredit == nil {
					if credit := joinArtistCredit(trk.Recording.ArtistCredit); credit != "" {
						entry.TrackCredit = &credit
					}
				}
			}
			out = append(out, entry)
		}
	}
	return out
}

// parseCreditArtists 保存上游的身份边界（包括含 & 的乐队名），但不赋参与角色。
func parseCreditArtists(credits []artistCredit) []provider.CreditArtist {
	var out []provider.CreditArtist
	offset := 0
	for _, c := range credits {
		name := firstNonEmpty(c.Name, c.Artist.Name)
		if name != "" {
			out = append(out, provider.CreditArtist{
				Name: name, CanonicalName: c.Artist.Name,
				SortName: strPtr(c.Artist.SortName), Type: normalizeArtistType(c.Artist.Type),
				MBID: strPtr(c.Artist.ID), Start: offset, End: offset + len(name),
			})
		}
		offset += len(name) + len(c.JoinPhrase)
	}
	return out
}

// parseRecordingArtists 只解析实际角色关系，不从 artist-credit 推断演唱。
func parseRecordingArtists(rec *recording) []provider.DetailArtist {
	var out []provider.DetailArtist
	position := 0

	// recording-level-rels：演唱者与器乐演奏者
	for _, reln := range rec.Relations {
		if reln.Artist == nil {
			continue
		}
		role, attribute := roleFromRecordingRelation(reln)
		if role == "" {
			continue
		}
		out = append(out, provider.DetailArtist{
			Name:      reln.Artist.Name,
			SortName:  strPtr(reln.Artist.SortName),
			Type:      normalizeArtistType(reln.Artist.Type),
			MBID:      strPtr(reln.Artist.ID),
			Role:      role,
			Attribute: attribute,
			Position:  position,
		})
		position++
	}

	// work-rels → work-level-rels：作曲与作词挂在 Work 上，
	// 不能因为 recording 层级没有就判为缺失。
	for _, reln := range rec.Relations {
		if reln.Work == nil {
			continue
		}
		for _, workRel := range reln.Work.Relations {
			if workRel.Artist == nil {
				continue
			}
			role := roleFromWorkRelation(workRel)
			if role == "" {
				continue
			}
			out = append(out, provider.DetailArtist{
				Name:     workRel.Artist.Name,
				SortName: strPtr(workRel.Artist.SortName),
				Type:     normalizeArtistType(workRel.Artist.Type),
				MBID:     strPtr(workRel.Artist.ID),
				Role:     role,
				Position: position,
			})
			position++
		}
	}
	return dedupeArtists(out)
}

// roleFromRecordingRelation 把 recording 层级的关系映射到角色取值域。
//
// 取值域之外的关系不创建（返回空），不引入新的角色值。
func roleFromRecordingRelation(r relation) (role string, attribute *string) {
	switch strings.ToLower(r.Type) {
	case "vocal":
		return "vocal", nil
	case "instrument":
		// 乐器名存在 attributes 中
		if len(r.Attributes) > 0 {
			attr := strings.Join(r.Attributes, ", ")
			return "instrument", &attr
		}
		return "instrument", nil
	case "performer", "performing orchestra":
		return "performer", nil
	case "producer":
		return "producer", nil
	case "remixer":
		return "remixer", nil
	case "conductor":
		return "conductor", nil
	case "arranger", "instrument arranger", "vocal arranger":
		return "arranger", nil
	case "mix", "mix-dj":
		return "remixer", nil
	default:
		return "", nil
	}
}

// roleFromWorkRelation 把 Work 层级的关系映射到角色取值域。
func roleFromWorkRelation(r relation) string {
	switch strings.ToLower(r.Type) {
	case "composer":
		return "composer"
	case "lyricist":
		return "lyricist"
	case "writer":
		return "writer"
	case "arranger", "orchestrator":
		return "arranger"
	default:
		return ""
	}
}

// normalizeArtistType 把 MB 的艺术家类型归一到取值域。
func normalizeArtistType(t string) *string {
	if t == "" {
		return nil
	}
	normalized := strings.ToLower(t)
	switch normalized {
	case "person", "group", "character", "orchestra", "choir":
		return &normalized
	default:
		other := "other"
		return &other
	}
}

// dedupeArtists 按 (名称, 角色) 去重，保留首次出现的顺序。
func dedupeArtists(in []provider.DetailArtist) []provider.DetailArtist {
	seen := map[string]bool{}
	out := make([]provider.DetailArtist, 0, len(in))
	for _, a := range in {
		key := a.Name + "\x00" + a.Role
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

// findTrack 只接受精确的曲目标识；空标识表示仅取发行清单。
func findTrack(rel *releaseDetail, trackRef string) (medium, track, bool) {
	if trackRef == "" {
		return medium{}, track{}, false
	}
	for _, med := range rel.Media {
		for _, trk := range med.TrackList() {
			if trk.ID == trackRef {
				return med, trk, true
			}
		}
	}
	return medium{}, track{}, false
}

func recordingTitle(t track) string {
	if t.Recording != nil {
		return t.Recording.Title
	}
	return ""
}

func firstWorkID(rec *recording) string {
	for _, r := range rec.Relations {
		if r.Work != nil && r.Work.ID != "" {
			return r.Work.ID
		}
	}
	return ""
}

// topGenre 取社区标签计数最高的风格。
func topGenre(genres []genre) string {
	best, bestCount := "", -1
	for _, g := range genres {
		if g.Count > bestCount {
			best, bestCount = g.Name, g.Count
		}
	}
	return best
}

func mergeParams(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// VoiceActorOf 取某角色艺术家的配音关系。
//
// 仅当 artist 的 type == "Character" 时才应调用——其他类型不触发该请求。
// 结果被缓存，同一角色二次出现时命中缓存。
func (p *Provider) VoiceActorOf(ctx context.Context, artistMBID string) (*provider.DetailArtist, error) {
	if err := p.IsConfigured(); err != nil {
		return nil, err
	}
	params := map[string]string{"fmt": "json", "inc": "artist-rels"}
	body, err := p.client.Get(ctx, provider.GetRequest{
		Provider:   provider.MusicBrainz,
		Endpoint:   "/ws/2/artist/" + artistMBID,
		BaseURL:    p.cfg.ServerURL,
		Params:     params,
		ParamsHash: p.hash(mergeParams(params, map[string]string{"id": artistMBID})),
		TTL:        0,
	})
	if err != nil {
		return nil, err
	}

	var a artistDetail
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("解析艺术家响应失败: %w", err)
	}
	return ParseVoiceActor(&a), nil
}

// ParseVoiceActor 从艺术家响应中取出配音该角色的人物。导出以便离线重放测试。
func ParseVoiceActor(a *artistDetail) *provider.DetailArtist {
	for _, r := range a.Relations {
		if !strings.EqualFold(r.Type, "voice") || r.Artist == nil {
			continue
		}
		return &provider.DetailArtist{
			Name:     r.Artist.Name,
			SortName: strPtr(r.Artist.SortName),
			Type:     normalizeArtistType(r.Artist.Type),
			MBID:     strPtr(r.Artist.ID),
			Role:     "vocal",
		}
	}
	return nil
}

// IsCharacter 报告某艺术家是否为角色类型——只有角色才触发补充请求。
func IsCharacter(artistType *string) bool {
	return artistType != nil && strings.EqualFold(*artistType, "character")
}

// TracklistTotal 返回清单的总曲目数。
func TracklistTotal(entries []provider.TracklistEntry) int { return len(entries) }

var _ = strconv.Itoa

// ParseDetailJSON 解析详情响应的原始字节。
//
// 供离线重放使用：直接喂 provider_cache 中落盘的原始响应，
// 走的是与在线路径完全相同的解析代码。
func ParseDetailJSON(body []byte, trackRef string) (*provider.Detail, error) {
	var rel releaseDetail
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("解析详情响应失败: %w", err)
	}
	return parseDetailTarget(&rel, trackRef)
}

// ParseVoiceActorJSON 解析艺术家响应的原始字节，供离线重放使用。
func ParseVoiceActorJSON(body []byte) (*provider.DetailArtist, error) {
	var a artistDetail
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("解析艺术家响应失败: %w", err)
	}
	return ParseVoiceActor(&a), nil
}
