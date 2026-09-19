package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// ApplyDetailToTrack 把一次详情拉取的结果落到某首曲目上。
//
// 这是**唯一**把上游详情转为本地数据的地方：丰富信息、参与者关系、
// 可点击区间、清单都在这里写入。此前这些写入散落且从未被调用——
// 结果是艺术家子系统在生产路径上完全没跑起来，而单元测试各自都通过。
func (a *App) ApplyDetailToTrack(
	ctx context.Context,
	trackID int64,
	providerID string,
	d *provider.Detail,
) error {
	if d == nil {
		return fmt.Errorf("详情为空，无法补全曲目")
	}
	// 1. 丰富信息（不含参与者）
	if err := a.applyDetailMeta(trackID, providerID, d); err != nil {
		return err
	}
	// 2. 参与者关系 + 可点击区间
	if err := a.applyDetailArtists(trackID, providerID, d); err != nil {
		return err
	}
	// 3. 同一次响应已带完整清单，不再等专辑页另取一次。
	if d.ReleaseRef != nil && len(d.Tracklist) > 0 {
		rel, err := a.store.GetReleaseByRef(providerID, *d.ReleaseRef)
		if err != nil {
			return err
		}
		if rel != nil {
			return a.replaceTracklistFromDetail(rel.ID, d)
		}
	}
	return nil
}

// applyDetailMeta 写入丰富信息字段。
func (a *App) applyDetailMeta(trackID int64, providerID string, d *provider.Detail) error {
	edits := map[string]string{}
	sources := map[string]string{}

	put := func(key, value string) {
		if value != "" {
			edits[key] = value
			sources[key] = providerID
		}
	}
	if d.Year != nil {
		put("year", fmt.Sprintf("%d", *d.Year))
	}
	if d.TrackNumber != nil {
		put("trackNumber", fmt.Sprintf("%d", *d.TrackNumber))
	}
	if d.DiscNumber != nil {
		put("discNumber", fmt.Sprintf("%d", *d.DiscNumber))
	}
	if d.DurationMS != nil {
		put("durationMs", fmt.Sprintf("%d", *d.DurationMS))
	}
	put("genre", deref(d.Genre))
	put("label", deref(d.Label))
	put("catalogNumber", deref(d.CatalogNumber))
	put("mediumFormat", deref(d.MediumFormat))
	put("country", deref(d.Country))
	put("primaryType", deref(d.PrimaryType))
	put("comment", deref(d.Comment))

	// 逐字段来源与锁：读取既有值，来源为 user 的字段不被覆盖
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
	for key := range edits {
		// 用户手动改过并加锁的字段不被上游自动覆盖
		if locked[key] && provenance[key] == "user" {
			delete(edits, key)
			continue
		}
		provenance[key] = sources[key]
	}

	// 上游标识与署名原串：直接写列，不走编辑界面那套
	if err := a.store.WriteDetailColumns(trackID, d); err != nil {
		return err
	}
	return a.store.ApplyMetadataEdits(trackID, edits, provenance, locked)
}

// applyDetailArtists 分别写入署名成员与真实角色；CV 署名为一个完整身份。
func (a *App) applyDetailArtists(trackID int64, providerID string, d *provider.Detail) error {
	credit := deref(d.TrackCredit)
	if credit == "" {
		credit = deref(d.AlbumCredit)
	}
	rules := matching.DefaultRules()
	parsed := rules.ParseCredit(credit)
	var upstreamCredits [][]provider.CreditArtist
	// MB 已给出边界，不能再按标点拆乐队名。跨多个部件的 CV 署名仍合为一个身份。
	if len(d.Credits) > 0 {
		parsed = matching.CreditParse{Raw: credit}
		for _, c := range d.Credits {
			if c.Start < 0 || c.End > len(credit) || c.Start >= c.End || credit[c.Start:c.End] != c.Name {
				parsed.Uncertain = true
				break
			}
		}
		segments := matching.FindBracketSegments(credit)
		for i := 0; i < len(d.Credits) && !parsed.Uncertain; {
			c := d.Credits[i]
			end := c.End
			for _, seg := range segments {
				if seg.Start >= c.Start && seg.End > end &&
					(seg.Start <= end || strings.TrimSpace(credit[end:seg.Start]) == "") && rules.Classify(seg).Kind == matching.KindCV {
					end = seg.End
				}
			}
			j := i + 1
			for j < len(d.Credits) && d.Credits[j].Start < end {
				if d.Credits[j].End > end {
					parsed.Uncertain = true
				}
				j++
			}
			part := rules.ParseCreditName(credit[c.Start:end])
			if part.Uncertain || len(part.Artists) != 1 {
				parsed.Uncertain = true
				break
			}
			pa := part.Artists[0]
			pa.Start += c.Start
			pa.End += c.Start
			parsed.Artists = append(parsed.Artists, pa)
			upstreamCredits = append(upstreamCredits, d.Credits[i:j])
			i = j
		}
	}

	var credits []store.CreditEntry
	var relations []store.TrackArtist
	// 同一声优有多个角色署名时不猜是哪一个；普通署名可按明确上游身份复用。
	type creditIdentity struct {
		id int64
		cv bool
	}
	identities := map[string][]creditIdentity{}
	remember := func(key string, id int64, cv bool) {
		for _, existing := range identities[key] {
			if existing.id == id {
				return
			}
		}
		identities[key] = append(identities[key], creditIdentity{id, cv})
	}
	if parsed.Uncertain {
		if err := a.store.MarkTrackForReview(trackID, "署名拆分不确定，需人工确认"); err != nil {
			return err
		}
	} else {
		for i, pa := range parsed.Artists {
			artist := &store.Artist{Name: pa.Name, Type: strPtrOrNil(pa.Type)}
			cv := pa.VoiceActor != ""
			var upstream *provider.CreditArtist
			if len(upstreamCredits) > i {
				upstream = &upstreamCredits[i][0]
				if !cv {
					artist.SortName, artist.Type = upstream.SortName, upstream.Type
					// 保留实际署名写法，不把不同写法跨曲目自动合并。
					// 别名不抢占规范名的唯一 MBID；当前曲目内仍可按明确身份对应角色。
					if upstream.CanonicalName == "" || upstream.CanonicalName == pa.Name {
						artist.MBID = upstream.MBID
					}
				}
			}
			id, err := a.store.UpsertArtist(artist)
			if err != nil {
				return err
			}
			start, end, name := pa.Start, pa.End, pa.Name
			credits = append(credits, store.CreditEntry{
				ArtistID: id, Position: i, CreditedName: &name, SpanStart: &start, SpanEnd: &end,
			})
			remember("name:"+pa.Name, id, cv)
			if cv {
				remember("name:"+pa.VoiceActor, id, true)
			}
			if upstream != nil {
				for _, part := range upstreamCredits[i] {
					remember("name:"+part.Name, id, cv)
					if part.CanonicalName != "" {
						remember("name:"+part.CanonicalName, id, cv)
					}
					if part.MBID != nil {
						remember("mbid:"+*part.MBID, id, cv)
					}
				}
			}
		}
	}

	seen := map[string]bool{}
	for _, da := range d.Artists {
		if da.Name == "" || !store.IsValidRole(store.ArtistRole(da.Role)) {
			continue
		}
		matches := identities["name:"+da.Name]
		if da.MBID != nil && len(identities["mbid:"+*da.MBID]) > 0 {
			matches = identities["mbid:"+*da.MBID]
		}
		var id int64
		if len(matches) == 1 && (!matches[0].cv || da.Role == "vocal") {
			id = matches[0].id
		} else {
			var err error
			id, err = a.store.UpsertArtist(&store.Artist{
				Name: da.Name, SortName: da.SortName, Type: da.Type, MBID: da.MBID,
			})
			if err != nil {
				return err
			}
		}
		key := fmt.Sprintf("%d\x00%s", id, da.Role)
		if seen[key] {
			continue
		}
		seen[key] = true
		position := da.Position
		relations = append(relations, store.TrackArtist{
			ArtistID: id, Role: store.ArtistRole(da.Role), Attribute: da.Attribute,
			Position: &position, Source: &providerID,
		})
	}
	// 空结果也替换，避免刷新后残留旧的署名成员或误推断的演唱关系。
	if err := a.store.SetTrackCredits(trackID, credits); err != nil {
		return err
	}
	return a.store.SetTrackArtists(trackID, relations)
}

// RefreshTrackDetail 为已入库曲目立即拉取并落库详情。
//
// 供编辑页的「更换」与用户显式的「重新获取」使用：**用户在等**，
// 因此走交互式优先级，插到后台补全的队列之前。
//
// 复核队列的「接受」不再走这里：那一次请求的产出与「这条曲目能不能进库」
// 无关，而在 1 req/s 之下它就是 1–2 秒的卡顿。接受只入库，
// 详情交给后台的补全信息匹配（见 enrichWorker）。
func (a *App) RefreshTrackDetail(trackID int64, providerID, releaseRef string) error {
	if err := a.ready(); err != nil {
		return err
	}
	done := a.beginInteractive()
	defer done()

	ctx := provider.WithPriority(context.Background(), provider.PriorityInteractive)
	if err := a.fetchAndApplyDetail(ctx, store.EnrichTarget{
		TrackID: trackID, Provider: providerID, ReleaseRef: releaseRef,
	}); err != nil {
		return err
	}
	if err := a.store.SetEnrichState(trackID, store.EnrichFetched); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// enrichOne 是后台补全任务处理一条曲目的入口。
//
// 与 RefreshTrackDetail 的差别只有两处：优先级为后台，
// 且不逐条通知界面重载（那由任务在一轮结束时做一次）。
// 状态由任务写，因为只有它知道失败该记第几次。
func (a *App) enrichOne(ctx context.Context, target store.EnrichTarget) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.fetchAndApplyDetail(provider.WithPriority(ctx, provider.PriorityBackground), target)
}

// fetchAndApplyDetail 取一次详情并落到某条曲目上。
//
// 两条路径（用户显式刷新、后台补全）共用它：出网前置检查、缺标识的判断、
// 错误包装都只写一份，否则两边的行为会慢慢漂移。
func (a *App) fetchAndApplyDetail(ctx context.Context, target store.EnrichTarget) error {
	if gate := a.CheckOutbound(); !gate.Allowed {
		return fmt.Errorf("%s", gate.Reason)
	}
	if target.ReleaseRef == "" {
		return fmt.Errorf("缺少发行标识，无法拉取详情")
	}

	// 在共用入口从当前记录读取身份，而不是只依赖排队时的发行快照。
	// 后台、手动刷新与导入时立即补全均经过这里，不能漏接某一条路径。
	meta, err := a.store.GetMetadata(target.TrackID)
	if err != nil {
		return err
	}
	if meta == nil || deref(meta.MatchRef) == "" {
		return fmt.Errorf("缺少曲目标识，无法定位详情")
	}
	if deref(meta.MatchSource) != target.Provider || deref(meta.ReleaseRef) != target.ReleaseRef {
		return fmt.Errorf("曲目的上游来源或所属发行已改变，取消本次补全")
	}
	trackRef := *meta.MatchRef

	p, err := a.registry.Get(provider.ProviderID(target.Provider))
	if err != nil {
		return err
	}
	d, err := p.FetchDetail(ctx, provider.Ref{
		Provider: provider.ProviderID(target.Provider),
		ID:       target.ReleaseRef, TrackRef: trackRef, Country: countryOf(target.ReleaseRef),
	})
	if err != nil {
		return fmt.Errorf("拉取详情失败: %w", err)
	}
	if d == nil || string(d.Provider) != target.Provider || deref(d.ReleaseRef) != target.ReleaseRef || d.MatchRef != trackRef {
		return fmt.Errorf("详情未匹配目标曲目，拒绝写入")
	}
	// 请求在途时可能发生重新匹配/移动歌集；迟到的旧详情不能覆盖新身份。
	current, err := a.store.GetMetadata(target.TrackID)
	if err != nil {
		return err
	}
	if current == nil || deref(current.MatchSource) != target.Provider || deref(current.MatchRef) != trackRef || deref(current.ReleaseRef) != target.ReleaseRef {
		return fmt.Errorf("曲目身份在拉取期间已改变，取消本次补全")
	}
	return a.ApplyDetailToTrack(ctx, target.TrackID, target.Provider, d)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
