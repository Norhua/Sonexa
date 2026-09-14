package main

import (
	"context"
	"encoding/json"
	"fmt"

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
	// 1. 丰富信息（不含参与者）
	if err := a.applyDetailMeta(trackID, providerID, d); err != nil {
		return err
	}
	// 2. 参与者关系 + 可点击区间
	if err := a.applyDetailArtists(ctx, trackID, providerID, d); err != nil {
		return err
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

// applyDetailArtists 由详情写艺术家、配音关系、署名分解与参与角色。
//
// 「署名」与「参与角色」是两件不同的事，落在两张表上：
//
//	track_credit  谁出现在署名串里，以什么写法、在哪个区间
//	track_artist  谁在这首歌上担任什么角色（演唱、作曲、作词……）
//
// 此前只有后者，而署名串解析出的艺术家被一律写成 role='vocal'——
// 于是只作词作曲的署名艺术家（如 DECO*27）在库里被标成演唱者，
// 而 MusicBrainz 关系给出的未署名作词作曲与署名艺术家长得一模一样，
// 艺术家列表因此把所有人都列了出来。
//
// 角色只来自上游的**关系**，不从署名串猜：署名串只说明「这条歌署了谁的名」，
// 说明不了这个人做了什么。
func (a *App) applyDetailArtists(ctx context.Context, trackID int64, providerID string, d *provider.Detail) error {
	credit := deref(d.TrackCredit)
	if credit == "" {
		credit = deref(d.AlbumCredit)
	}

	artistIDs := map[string]int64{}
	var credits []store.CreditEntry
	var relations []store.TrackArtist

	if credit != "" {
		rules := matching.DefaultRules()
		parsed := rules.ParseCredit(credit)

		if parsed.Uncertain {
			// 拆分不确定时不拆：保留原串、标记该条目待人工确认。
			// 但**不提前返回**——上游关系给出的作曲作词与这次拆分无关，
			// 丢掉它们等于因为署名串难解析而连带丢掉整首歌的参与者。
			if err := a.store.MarkTrackForReview(trackID, "署名拆分不确定，需人工确认"); err != nil {
				return err
			}
		} else {
			for i, pa := range parsed.Artists {
				id, err := a.store.UpsertArtist(&store.Artist{
					Name: pa.Name, Type: strPtrOrNil(pa.Type),
				})
				if err != nil {
					return err
				}
				artistIDs[pa.Name] = id

				// 署名：记这个人出现在原串的哪一段。credited_name 是**他自己**
				// 那一段的写法，不是整条署名串——此前这里写的是整串，
				// 于是参与者区把整条署名当成了某一位艺术家的名字。
				start, end := pa.Start, pa.End
				name := pa.Name
				credits = append(credits, store.CreditEntry{
					ArtistID: id, Position: i, CreditedName: &name,
					SpanStart: &start, SpanEnd: &end,
				})

				// 角色署名里的角色艺术家（「涼宮ハルヒ(CV:平野綾)」的前半）
				// 仍需要一条 character 关系：它是这位角色与这首歌的唯一联系。
				if pa.Type == store.ArtistTypeCharacter {
					relations = append(relations, store.TrackArtist{
						ArtistID: id, Role: store.RoleCharacter, Source: &providerID,
					})
				}
			}
			// 配音关系
			for character, actor := range parsed.VoiceOf {
				charID, actorID := artistIDs[character], artistIDs[actor]
				if charID == 0 || actorID == 0 {
					continue
				}
				if err := a.store.LinkArtists(charID, actorID, store.LinkVoiceOf); err != nil {
					return err
				}
			}
		}
	}

	// 上游关系给出的参与者：角色以它为准
	seen := map[string]bool{}
	for _, da := range d.Artists {
		if da.Name == "" || da.Role == "" {
			continue
		}
		if !store.IsValidRole(store.ArtistRole(da.Role)) {
			continue // 取值域之外的关系不创建
		}

		// 角色类型且来源为 MB 时，可补一次配音关系请求
		if musicbrainzIsCharacter(da.Type) && da.MBID != nil && providerID == string(provider.MusicBrainz) {
			if actor, err := a.lookupVoiceActor(ctx, *da.MBID); err == nil && actor != nil {
				charID, err := a.store.UpsertArtist(&store.Artist{
					Name: da.Name, Type: da.Type, MBID: da.MBID,
				})
				if err != nil {
					return err
				}
				actorID, err := a.store.UpsertArtist(&store.Artist{
					Name: actor.Name, Type: actor.Type, MBID: actor.MBID,
				})
				if err != nil {
					return err
				}
				if err := a.store.LinkArtists(charID, actorID, store.LinkVoiceOf); err != nil {
					return err
				}
			}
		}

		key := da.Name + "\x00" + da.Role
		if seen[key] {
			continue
		}
		seen[key] = true

		id, err := a.store.UpsertArtist(&store.Artist{
			Name: da.Name, SortName: da.SortName, Type: da.Type, MBID: da.MBID,
		})
		if err != nil {
			return err
		}
		artistIDs[da.Name] = id

		position := da.Position
		relations = append(relations, store.TrackArtist{
			ArtistID:  id,
			Role:      store.ArtistRole(da.Role),
			Attribute: da.Attribute,
			Position:  &position,
			Source:    &providerID,
		})
	}

	// 署名分解里此前没对上 id 的名字，可能在关系里出现过（上游两处写法一致时）。
	// 两处都写完之后再整体落库，顺序无关。
	if len(credits) > 0 {
		if err := a.store.SetTrackCredits(trackID, credits); err != nil {
			return err
		}
	}
	if len(relations) > 0 {
		if err := a.store.SetTrackArtists(trackID, relations); err != nil {
			return err
		}
	}
	return nil
}

// lookupVoiceActor 取角色的配音关系。
//
// 仅当艺术家类型为角色时才调用，结果由 provider_cache 缓存，
// 同一角色二次出现时命中缓存不再发请求。
func (a *App) lookupVoiceActor(ctx context.Context, artistMBID string) (*provider.DetailArtist, error) {
	mb, err := a.registry.Get(provider.MusicBrainz)
	if err != nil {
		return nil, err
	}
	// 具体的补充请求是 MB 特有的能力，接口上没有它的位置
	type voiceLookup interface {
		VoiceActorOf(context.Context, string) (*provider.DetailArtist, error)
	}
	v, ok := mb.(voiceLookup)
	if !ok {
		return nil, fmt.Errorf("该上游不支持角色关系查询")
	}
	return v.VoiceActorOf(ctx, artistMBID)
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

	p, err := a.registry.Get(provider.ProviderID(target.Provider))
	if err != nil {
		return err
	}
	d, err := p.FetchDetail(ctx, provider.Ref{
		ID: target.ReleaseRef, Country: countryOf(target.ReleaseRef),
	})
	if err != nil {
		return fmt.Errorf("拉取详情失败: %w", err)
	}
	return a.ApplyDetailToTrack(ctx, target.TrackID, target.Provider, d)
}

func musicbrainzIsCharacter(t *string) bool {
	return t != nil && (*t == "character" || *t == "Character")
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
