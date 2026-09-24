# 规格差异：QQ 音乐升级为标准元数据上游

> 记录本次实现（见 `plan.md`）与现有 `openspec/specs` 的差异和冲突。按要求未直接修改 spec，待确认后同步。

## 1. 直接冲突（spec 明文与实现相反）

| spec 位置 | 现有表述 | 实现 | 建议修订 |
| --- | --- | --- | --- |
| `upstream/provider-registry` 「接口与注册表」 | 封面搜索另行提供 CAA、iTunes、QQ、163 专用来源列表……不要求封面专用来源实现歌曲搜索、导入匹配或详情补全 | QQ 音乐（id `qq`）进入元数据注册表，实现完整 Provider 接口；封面专用来源只剩 163 | 改为「163 为封面专用来源；QQ 同时是元数据上游与封面来源」 |
| `upstream/provider-registry` Scenario「封面专用来源」 | QQ 和 163 注册为封面搜索来源……歌曲搜索、复核改搜和导入匹配不因此增加这两个元数据上游 | 上游搜索、复核改搜、编辑重匹配可选 QQ 音乐；导入匹配仍只用 MusicBrainz | Scenario 只保留 163；新增「QQ 元数据上游」Scenario |
| `upstream/cover-search` 「来源适配」 | QQ 和 163 只作为封面来源，不进入歌曲导入/复核的元数据上游选项 | QQ 进入复核的元数据上游选项 | 删去 QQ，仅保留 163 |
| `upstream/cover-art` 「后端取图」 | 自动请求……不自动访问 QQ、163 或其他封面来源 | 歌集/候选来源为 `qq` 且有专辑 MID 时，自动按 QQ 图片地址取图（缩略图 300→1200、详情 1200→300 逐级回退） | 新增「QQ 专辑身份自动取图」规则；不跨源取图原则不变（MB→CAA、iTunes→iTunes、QQ→QQ）|
| `app` 测试 `TestCoverSourceRegistryDoesNotExtendMetadataProviders` | 断言元数据来源只有 2 个且无 qq | 已改为 `TestCoverSourceRegistryOnlyQQIsMetadataProvider`，断言 `musicbrainz,itunes,qq` 且无 163 | 随 spec 同步 |

## 2. 新增能力（spec 未覆盖）

1. **新 spec 建议 `upstream/qqmusic/spec.md`**，内容包括：
   - 标识：`match_ref` = 歌曲 MID（`^[0-9A-Za-z]{10,20}$`），`release_ref` = 专辑 MID；album ID=0 / MID 为空的搜索结果不作为发行身份、不给封面。
   - 请求：`musicu.fcg` POST JSON；顶层与每个请求块 `code` 必须为 0；详情合并 GetAlbumDetail + GetAlbumSongList（每页 100，最多 10 页，空页停止）+ get_song_detail。
   - 能力：Composer、Lyricist、PerformerRoles、Label、PrimaryType、CoverArt、MultiSizeCover、Lyrics。不声明目录号（`recordNum` 语义未核实）、ISRC、国家、载体、次要类型。
   - 角色映射：按 info 标题（不按 type）：演唱者→vocal、合声→vocal(attribute=合声)、作词→lyricist、作曲→composer、编曲→arranger、制作人→producer、乐器名→instrument(attribute=乐器)；合声编写、录音/混音工程等不写入；专辑 singerList→album_artist。不把 `singer[]` 推断为演唱者。
   - 碟/轨：清单按 `(index_cd, index_album)` 排序；碟号 = index_cd+1；碟内序号为派生排名，多碟时 `track_number_text` 保存全专辑序号原值。搜索候选只在第 1 碟时给音轨号。
   - 标题使用 `title`（保留译名/版本），类型只映射 录音室专辑/EP/单曲。
   - 歌词：详情内「歌词」条目优先，缺失时回退 `fcg_query_lyric_new.fcg?format=json&nobase64=1`；歌词失败不影响其他字段。
   - 速率：默认 30 次/分钟，搜索/详情/歌词共用令牌桶；受全局出网条件约束。
2. **`platform/data-store`**：`track` 新增 `lyrics TEXT`（迁移 v5）；详情写入用 COALESCE，不因后续无歌词的响应而清空。歌词为上游事实，不进编辑界面。
3. **`upstream/provider-registry` 能力位**：新增 `Lyrics`。
4. **`provider` 传输层**：`GetRequest` 支持 `Body`（POST JSON）与 `Headers`；带请求体时缓存键须包含请求体。原 spec 描述的缓存/限流/重试规则对 POST 同样适用。
5. **`platform/settings`**：新增「上游 → QQ 音乐」分组，`qqmusic.ratePerMin`；默认 `providerPriority` 变为 `musicbrainz, itunes, qq`。关于页 QQ 说明改为「部分元数据、歌词与封面来自 QQ 音乐（非公开接口）……歌词与封面图片不随导出分发」。
6. **`library/track-library`（详情抽屉）**：新增只读、默认折叠的「歌词」区，展示时去掉 LRC 标签行与时间标签；QQ 来源无角色时给出对应提示。
7. **`matching/provenance`**：`ValidateMatchRef` 新增 qq 分支。

## 3. 保持不变 / 待定

- 封面搜索页 QQ 来源仍走原 `WebSource`（独立 `cover-qq` 配额），与元数据 QQ 的令牌桶**不共用**。这与 provider-registry「封面搜索中调用 MusicBrainz/iTunes 搜索仍消费其既有共享配额」的精神不一致，是否合并配额待定。
- 导入阶段自动匹配仍只用 MusicBrainz，spec 无需改动。
- 导出是否包含歌词：当前导出未加入歌词列，spec 未定义，待定。
- `recordNum`、`index_cd` 零基规律来自少量样本，未来契约变化需回归测试（`internal/provider/qqmusic/testdata`）。
