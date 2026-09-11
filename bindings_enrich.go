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

// applyDetailArtists 由详情的参与者写艺术家、关系、参与者关系与可点击区间。
//
// 角色↔声优的拆分在这里真正落地：角色艺术家与人物艺术家各一条记录，
// 中间是一条 voice_of 关系；署名原串保真，并生成渲染用的可点击区间。
func (a *App) applyDetailArtists(ctx context.Context, trackID int64, providerID string, d *provider.Detail) error {
	credit := deref(d.TrackCredit)
	if credit == "" {
		credit = deref(d.AlbumCredit)
	}

	// 有署名原串时先做 CV 解析：拆出角色与声优，并生成区间
	artistIDs := map[string]int64{}
	var relations []store.TrackArtist

	if credit != "" {
		rules := matching.DefaultRules()
		parsed := rules.ParseCredit(credit)

		if parsed.Uncertain {
			// 拆分不确定时不拆：保留原串、标记该条目进复核，
			// 而不是猜测性地建立艺术家记录。
			return a.store.MarkTrackForReview(trackID, "署名拆分不确定，需人工确认")
		}

		for _, pa := range parsed.Artists {
			id, err := a.store.UpsertArtist(&store.Artist{
				Name: pa.Name, Type: strPtrOrNil(pa.Type),
			})
			if err != nil {
				return err
			}
			artistIDs[pa.Name] = id
			relations = append(relations, store.TrackArtist{
				ArtistID:     id,
				Role:         store.ArtistRole(pa.Role),
				CreditedName: &credit,
				Source:       &providerID,
			})
		}
		// 配音关系：仅角色存在时才发起补充请求
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

	// 详情给出的其余参与者（作曲、作词、器乐演奏等）
	seen := map[string]bool{}
	for _, da := range d.Artists {
		if da.Name == "" {
			continue
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
			ArtistID:     id,
			Role:         store.ArtistRole(da.Role),
			CreditedName: da.CreditedName,
			Attribute:    da.Attribute,
			Position:     &position,
			Source:       &providerID,
		})
	}

	if len(relations) == 0 {
		return nil
	}
	if err := a.store.SetTrackArtists(trackID, relations); err != nil {
		return err
	}

	// 可点击区间：解析时一并生成，供渲染使用
	if credit != "" {
		rules := matching.DefaultRules()
		parsed := rules.ParseCredit(credit)
		spans := matching.BuildSpans(parsed, artistIDs)
		if err := a.store.WriteCreditSpans(trackID, spans); err != nil {
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

// RefreshTrackDetail 为已入库曲目拉取并落库详情。
//
// 供复核队列的「接受」与编辑页的「更换」使用：
// 这两处拿到的是 track 粒度候选，补一次详情才能得到参与者与丰富信息。
func (a *App) RefreshTrackDetail(trackID int64, providerID, releaseRef string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return fmt.Errorf("%s", gate.Reason)
	}
	if releaseRef == "" {
		return fmt.Errorf("缺少发行标识，无法拉取详情")
	}

	p, err := a.registry.Get(provider.ProviderID(providerID))
	if err != nil {
		return err
	}
	d, err := p.FetchDetail(context.Background(), provider.Ref{
		ID: releaseRef, Country: countryOf(releaseRef),
	})
	if err != nil {
		return fmt.Errorf("拉取详情失败: %w", err)
	}
	if err := a.ApplyDetailToTrack(context.Background(), trackID, providerID, d); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
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
