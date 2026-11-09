# Design

## Context

动机与范围见 [proposal.md](proposal.md)，对外行为以本变更 `specs/` 为准。以下是已阅读代码的结论，不表示新能力已实现：

| 现有位置 | 现状及对本次设计的影响 |
| --- | --- |
| `internal/app/upstreams.go` | 集中构建、验证和发布元数据/封面/歌词能力，QQ 已复用同一个实例；新增歌单集合沿用这一入口，不增加业务侧来源分发。 |
| `internal/provider/qqmusic/{qqmusic.go,parse.go}`、`provider/client.go` | 已有 musicu 请求、块状态解析、缓存模式、统一门禁与平台配额；现有专辑清单不是用户歌单接口，其分页上限不能直接套用。 |
| `tmp/demo/qqmusic_playlist_export_bundle/qqmusic_playlist_export.py` | 展示了 qzone/v8/musicu 歌单读取路径与新旧字段差异；它按异常轮换端点、拒绝空列表，且完整性检查不一致，不能作为生产实现直接复制。 |
| `tmp/demo/spotify_playlist_cli/{spotify_playlist_export.py,diagnose_spotify_playlist.py}` | 使用 SpotifyScraper v3，`max_tracks=None` 避免默认 100 首截断；诊断明确展示普通 Track 过滤可能导致总数不同。诊断依赖私有 API，只提供风险线索。 |
| `internal/importer/runner.go` | 匹配前后调用 `ResolveImport`，自动准入提交后触发刷新；多个“已有曲目”提前返回路径目前没有同步目标投递。 |
| `internal/store/{import_identity.go,owned_import.go,queue.go}` | 输入不可变、跨批次主条目合并、确认基线与搜索修订已存在；主条目同时承载调度状态，不能级联删除它而误伤其他输入。判重读取全 track，目前无需新增 in_library 过滤。 |
| `internal/store/admission_plan.go`、`internal/domain/admission.go` | 存在服务端计划、签名、事务重验、幂等结果和输入确认；尚无导入目标或曲库归属。 |
| `internal/store/{library.go,browse.go,lookup.go,fts.go,track.go}` | track 被默认视为收藏；加入时间取 created_at；删除物理清理；多个名单、清单收藏与计数查询独立实现，不能只在列表加一处 WHERE。 |
| `frontend/src/{App.vue,components/TrackTable.vue,stores/trackColumns.ts}` | 列表使用统一表格、稳定行选择、普通/专辑两套列偏好；`ghost` 同时控制置灰和不可选，不能用于同步保留行。 |
| `frontend/src/views/{ReviewView.vue,ImportQueueView.vue}` | 用户指定的三个操作按钮实际在 ReviewView；ImportQueueView 展示批次，两处分别补目标提示。 |
| `build/`、`README.md` | 只有 Wails 构建和平台资源，没有 Python 包装或辅助程序交付；需新增可复现构建链，而不是在运行时安装。 |

现有主规格的“导入仅来自文件、不支持增量同步”“track 即收藏”“灰色行不可选”“无收藏发行删除”和“本地命中即已在库”均与新需求冲突，已通过对应 delta 显式修订。主规格 `platform/app-shell` 仍写复核“接受/合并”，但代码与 review 规格实际为“接受/跳过”，本次涉及候选归属时统一为后者。

## Goals / Non-Goals

**Goals:**
- 把上游快照、导入输入/需求、已导入本地实体、曲库成员分成四个可独立验证的状态层。
- 在事务中完成“队列终态 + 所有有效目标投递 + 曲目引用”，保证取消、删除、重复提交和进程退出不会留下半成品。
- 保留既有匹配证据、MusicBrainz 自动搜索、发行准入、用户字段保护和元数据补全，不将歌单抓取误当作元数据接受。
- 对网络、辅助进程和预览设置可测的边界；QQ 与 Spotify 的差异只存在于各自上游包。

**Non-Goals:**
- 不实现通用插件系统、常驻 Python 服务、用户可配置脚本/解释器或自动更新运行环境。
- 不改造所有历史队列/匹配算法，不增加新的宽松判重依据；跨歌单共享仍依赖已有可靠证据。
- 不借此修复 G07 全局大曲库分页、G09 所有旧入口错误提示等无关缺口；新同步视图及差异弹窗不得静默截断。
- 不把 Spotify 接入元数据搜索/歌词/封面，不用平台歌曲 ID 直接跳过导入，不自动写回用户当前元数据。

## Decisions

### 1. 歌单业务声明窄接口，装配层注入

新增 `internal/playlists/`，拥有 `Provider`、来源集合、完整快照、差异计划和服务端编排所需的存储接口。建议接口为：

```text
Provider: Upstream + ParsePlaylist(input) + FetchPlaylist(ctx, ref)
Snapshot: PlaylistRef, Name, FetchedAt, Entries, Completeness, Diagnostics
Entry: EntryKey, TrackRef, Position, Title?, Credit?, Album?, DurationMS?
```

`EntryKey` 是适配器声明的上游出现标识；`TrackRef` 是平台歌曲身份。它们不等于 Sonexa `match_source/match_ref`。数据的字段缺失不影响快照成员完整性，两者分别校验。业务只做统一结构/边界校验，不补字段、不解释原生错误。

`qqmusic.Provider` 直接实现该接口，新增 `internal/provider/qqmusic/playlist.go` 等文件，复用 QQ 实例、私有请求与状态解析。新增 `internal/provider/spotify/`，实现链接解析及对辅助程序的调用。`app/upstreams.go` 创建并验证歌单集合，扩展 `upstreamServices`，原子发布；Spotify 不进入 `provider.Registry`。能力运行环境缺失不阻断整个 App 或 QQ 装配，以来源可用性返回明确错误。

**替代方案**：扩展 `MetadataProvider` 大接口会迫使 Spotify 提供无意义的元数据实现；在 `playlists` 内 `switch source` 或调用 Python 会违反已有依赖方向，均不采用。

### 2. 独立歌单实体，不复用可写 collection_item

建议结构如下，实际命名遵循 store 现有风格：

| 实体 | 新增或关键字段 | 约束/用途 |
| --- | --- | --- |
| `track` | `in_library NOT NULL`、`library_added_at NULL` | 唯一元数据身份不变；所有创建路径显式指定归属，不依靠一个可能误入库的默认值。入库为真时必须有首次入库时间。 |
| `synced_playlist` | ID、provider、playlist_ref、upstream_name、alias、revision、last_synced_at、snapshot_digest | `(provider, playlist_ref)` 唯一；显示名为非空 alias，否则 upstream_name。不保存原始分享参数。 |
| `synced_playlist_entry` | ID、playlist_id、entry_key、track_ref、position、基础字段、upstream_present、local_state、track_id、revision | `(playlist_id, entry_key)` 唯一；`local_state=pending/imported/ignored`，只有 imported 可有 track_id 且必须有有效 FK。 |
| `import_destination` | input_id、kind、synced_entry_id、state、revision、目标显示摘要 | 每次有效输入保留自己的目标；kind 为 library 或 synced_playlist，state 为 active/delivered/cancelled。条目删除先撤销并断开 FK，保留最小目标摘要供历史日志。 |
| `import_batch` | 来源类型及目标摘要 | 文件目标固定音乐库，同步批次指向歌单；不保存临时 CSV/文件名。 |

`pending` 覆盖 queued/matching/review/failed，不复制完整队列状态；具体进度通过活动需求定位主条目。`ignored` 无活动需求；`imported` 不再依赖队列是否为 imported 或 skipped，而以成功投递关联为准。使用约束、唯一索引及事务断言禁止无引用 imported、带 track 的 pending/ignored、多重活动需求。

歌曲原始上游快照与 `track_current` 作用不同：前者用于差异与下一次新尝试的输入；已提交 `import_item.raw_row/in_*` 永不改写。仍在待处理的条目只更新最新同步快照，不静默替换其已经展示的输入/候选。忽略恢复时用最新确认快照新建尝试。

**替代方案**：给普通 collection 增加 source 再复用 Add/Remove 会带入可写成员、只按 track 唯一和级联删除语义，不满足上游重复项与双状态；独立表更清楚。

### 3. 成员差异以身份和出现次数匹配，不以标题或列表位置匹配

优先采用真实上游条目身份。上游没有稳定条目 ID 时，适配器按稳定歌曲 Ref + 同 Ref 的出现序号建立出现键。普通重排保持同一歌曲及其出现集合，新增/删除重复收录只改变出现数量，不将全歌单重新入队。该回退无法证明同曲两个相同副本中究竟删了哪一个，使用固定序号配对；这不构造跨歌曲同一性，也不按本地匹配 ID 合并上游行。将这一限制写入适配器测试和来源诊断。

已导入缺席项保留键和最后位置；重新出现优先恢复同键引用。默认展示先按新快照顺序显示在上游的已导入项，再以最后顺序与本地条目 ID 稳定显示缺席项。表头临时排序不写回顺序。同步行 key 使用 entry ID，不使用 track ID；批量入库按 track ID 去重。

**替代方案**：标题/专辑指纹会把平台改名当作新歌；全局位置在插入首项时导致全量重导；单纯 track ID 唯一会丢失重复收录，均不采用。

### 4. 完整快照、差异预览与提交分开

网络读取在事务和 App 全局锁之外。按绑定 ID（首次绑定按规范平台身份）合并/拒绝重复抓取。成功后创建有界服务端计划，保存快照、base revision、内容摘要和过期时间，前端只拿展示 DTO 与 token，不回传可伪造的原始快照。

建议 API：
- `ListPlaylistSources`、`ListSyncedPlaylists`、`GetSyncedPlaylistEntries`。
- `PreparePlaylistBinding` / `CommitPlaylistBinding`。
- `PreparePlaylistSync` / `CommitPlaylistSync(planID, hash, restoreEntryIDs)`。
- `CancelPlaylistOperation`、`DeleteSyncedPlaylist`。
- `PrepareLibraryAddition(trackIDs)` / `CommitLibraryAddition`。

首次获取后确认别名及绑定，提交才创建数据库歌单。刷新弹窗先展示新增/删除/恢复/排除项和默认折叠的忽略清单；选择恢复 ID 只能来自该计划的已忽略且仍存在上游集合，服务端按计划与选择重新校验。无变化也可提交恢复或更新名称/顺序。关闭/取消销毁计划，不修改数据库。

计划沿用现有准入的 15 分钟有效期和幂等结果模式，但不是创建发行的 AdmissionPlan。每歌单最多一个有效同步计划；每 App 最多 8 份、合计 64 MiB，超额明确拒绝而非驱逐未确认操作。提交先核对成功 operation_result，再核对绑定/条目 revision；目标导入成功、忽略、恢复或删除均递增歌单 revision，使旧差异不得误分类 pending/imported。已入库标记不参与上游增删分类，但入库提交另行读取最新状态。

确认事务执行差异、撤销/建立需求、忽略恢复、更新 revision 及 last_synced_at。网络出错、取消或不足以证明完整都不创建可确认删除计划。应用提交只证明本次已获取快照，不宣称远端在确认瞬间仍未变化；下次手动刷新继续比较。

**替代方案**：边拉分页边改数据库会把部分结果当删除；抓取后立即应用再展示会让“取消”无法撤回，所以采用准备/提交两阶段。

### 5. 明确同步条目状态机

| 当前状态 | 新快照/用户动作 | 事务结果 |
| --- | --- | --- |
| 不存在 | 新出现 | 保存 pending、创建有效输入及需求。 |
| present + pending | 仍存在 | 更新快照，不重复入队，不替换原输入。 |
| present + pending | 上游删除并确认 | 删除同步条目、撤销自身需求；没有其他需求才取消活动任务。 |
| present + pending | 用户移出队列 | 记 ignored、撤销需求，原尝试为终态。 |
| present + ignored | 仍存在 | 保持忽略；展开列表才能选择恢复。 |
| present + ignored | 选择恢复并确认 | 新建输入尝试，再做全 track 判重与必要匹配。 |
| present + ignored | 上游删除并确认 | 删除条目及忽略记录；以后重加是新输入。 |
| pending | 接受/无需匹配/可靠跳过成功 | 关联 track，置 imported；队列、确认关系和投递同事务。 |
| present + imported | 上游删除并确认 | 保留引用，upstream_present=false，页面置灰。 |
| absent + imported | 同键重新出现 | 复用原 track，恢复正常，不重新匹配。 |
| 任意 | 删除绑定 | 撤销自身需求和所有引用，仅回收无剩余同步引用的未入库 track。 |

上游“先删后加”只有在某次成功确认快照实际观察到缺席时才能识别；同一未观察区间发生删后重加无法推断，不凭时间猜测。

### 6. 输入合并保持多目标；结果集中投递

不为同步建立第二套匹配器，也不为了目标不同就取消既有跨批次合并。保留 `import_item` 及 `merged_item_id`，新增目标关系后把“活动输入集合”定义为主条目及其 absorbed 输入中仍有 active 需求的集合。

- `mergeTextInputTx` / 自动已解析身份合并仍使用现有严格证据；目标跟随输入，不覆盖为主条目的单一目标。
- 主输入目标被撤销但仍有其他有效需求时，保留主条目作为调度壳；实际 `matching.Input` 从仍有效输入的确定性代表构建，证据读取只用有效输入，不改任何 raw_row。已有手动搜索草稿按主 ID 保留；修订变化使旧提交计划失效。
- 未导入上游消失要求删除的是 `synced_playlist_entry` 和它的活动需求，不物理删除所有历史 `import_item`。独占任务退出复核并成为 discarded；被吸收输入保持 merged 终态。撤销需求记录“移出目标”日志，历史分母不变，merged 不重复计 discarded。
- 忽略恢复新建输入和需求，不重新激活历史 discarded/merged；可按同样证据并入当前其他有效任务，忽略中的其他歌单不会自动恢复。
- 主任务的移出动作针对 UI 明示的全部有效需求：音乐库需求取消，同步需求各自置 ignored；用户随后可在单个歌单恢复其中一项。

新增一个事务内结果投递原语，供 `finishAdmissionTx`、`skipOwnedImportTx`、`SkipReviewCandidate`、无需匹配和 `ReconcileReviewDuplicates` 共用。它先重验当前需求，再记录队列终态、为全部有效输入保存确认、向每个同步条目关联同一 track，并返回受影响歌单 ID 和归属变化。不能只在 Runner 的“新建成功”分支补一次 AddToPlaylist，否则已有曲目的提前返回路径会丢成员。

新建曲目：有任何有效 library 目标则 `in_library=true`，仅同步需求则 false。命中现有 track：无论目标是什么，按用户明确要求 skipped，不改 in_library。前端日志提示“已存在于本地，未自动入库”；需要入库必须显式操作。这也适用于普通文件导入命中同步专属 track。

**替代方案**：在曲目写入之后另起事务追加歌单会造成 imported 但页面无成员；按目标禁用合并会改变现有规则并增加重复匹配，不采用。

### 7. 曲库归属和引用生命周期

保留元数据唯一索引及全 track 判重。为查询建立明确用途，而不是给 `track_read` 全局添加过滤：

| 用途 | 范围 |
| --- | --- |
| ListTracks / CountTracks 默认、资料库搜索、最近添加、普通集合、艺术家/专辑名单与分面 | 仅 in_library=true。 |
| 同步列表 | 从当前歌单 imported entries join track_read，无 in_library 限制，包含 absent 行。 |
| GetTrackDetail、精确绑定徽章、导入判重、快照、补全、结构准入、编辑影响检查、物理引用清理 | 所有 track。 |
| 专辑 owned 行/位置、OwnedCount、音乐库展示代表 | 仅已入库曲目；结构成员集仍包含全部真实引用。 |
| 自动选版的“已在库发行优先” | 保持以已入库发行作为偏好证据；组成员准入仍检查全部真实发行，避免隐藏同步发行绕过确认。 |

`owned_import.go`/`FindByMatchRef` 维持全 track；不要复用已过滤的 ListTracks 做判重。`ResolveTracklist` 与 `ownedRowsFor` 单独改成已入库语义，同一未入库 track 在曲库专辑中可对应 missing 位置，明确加入时复用现有 ID。

FTS 保留所有已保存 track，通过 SQL join 限定查询域，再排序/限制返回，不先全局取 2000 个 ID 再过滤。同步页面查询支持当前歌单全部可见项；不将此次作用域修复宣传为解决所有大曲库性能缺口。

`library_added_at` 表示首次入库；从未入库为 NULL，第一次入库设置，仍保留 track 的取消/再入库不改首次时间，物理删除后再次创建是新实体。created_at 保留导入/创建时间，`TrackView`、详情和 AlbumRow 分别投影，前端加入时间不再错误取 created_at。

显式入库为 store 的批量幂等事务：预览按 ID 去重，返回 willAdd/alreadyInLibrary；提交重查，如果期间被入库则归入 already，若 ID 已被物理删除则返回 stale 并整批不提交。入库既不重跑元数据准入也不新增发行成员，原导入准入已完成。搜索/专辑灰色行明确加入现有未入库身份使用同一归属原语。

`DeleteTracks` 对已入库项执行 RemoveFromLibrary：清理 collection_item，存在任意同步 imported 引用（包括 absent）则只改 in_library；否则调用物理清理路径。后端对本来未入库的同步专属 ID 不执行物理删除。绑定删除先收集候选 track、撤销自身需求和条目，再逐一检查 `!in_library && !exists(other synced imported ref)`，安全回收；发行 orphan 判断始终按全部 track。歌词/封面文件清理沿用 fileMu、事务引用及提交后回收。

**替代方案**：按歌单复制 track 会破坏唯一绑定和共享标记；全局过滤 track_read 会让判重、补全和引用清理失真；从库删除直接 DELETE 会破坏只读同步成员，均不采用。

### 8. QQ 歌单适配

在已有 QQ 包实现官方歌单 URL/数字 ID 的离线解析，接受 demo 中官方详情页形态，但只抽取校验后的 ID 并由代码构造端点；不直接请求用户 URL，不增加任意跳转或 Cookie。

以 demo 给出的 musicu `music.srfDissInfo.DissInfo / CgiGetDiss` 为首选需验证协议，按其 total 与原始页长推进；如真实样本必须使用 qzone，则在 QQ 包中选择并固定经验证方案。不能复制“任何异常都尝试三个接口”的链路。顶层/块 code、限流、鉴权使用 QQ 现有解析规则，明确停止错误不能回退。采用 `NoStore`，共用 `qq` 长期桶，不误用专辑 10 页上限或现有详情 partial 容忍逻辑。

完整性同时检查所有页的总数/身份、页覆盖、零总数与重复页；基础字段缺失如实保留。无法稳定定位歌曲或含无法解释缺失时整次不可提交。绑定和刷新不把歌单歌曲直接构造成已接受 QQ 元数据候选，仍送原始输入经过 MB 导入与人工确认。

**替代方案**：复制 Python QQ demo 或为 QQ 也引入辅助运行环境无必要；借元数据专辑接口获取歌单会混淆实体和完整性，均不采用。

### 9. Spotify 使用随包冻结辅助程序，网络经 Go 代理执行

选用 Python SpotifyScraper v3 系列作为读取/解析依赖，使用 PyInstaller one-folder 模式冻结为私有 helper（包含解释器、包及必要 native 库），不让用户安装 Python，不在首次使用运行 pip/uv。helper 源码放在 `internal/provider/spotify/sidecar/`，冻结产物与依赖目录是构建产物，不提交二进制。

具体依赖补丁版本、Python 小版本、PyInstaller 及传递依赖在实现第一阶段离线/构建验证后固定精确版本与哈希；demo 的 `>=3,<4` 不作为交付锁。第三方包发布接口本次被网络地址安全检查阻止，未获取包源码，不能声称公开 transport 注入点或冻结已验通。实现先验证三个明确问题：完整分页/原始条目覆盖、所有请求可通过受控传输、目标平台冻结自检。若不可行，停下报告并更新方案，不退回直连、外部 Python 或复制 demo。

选择 Go 执行所有远程 HTTP，而不是只在 helper 启动前检查一次门禁：

```text
playlists.Service
  → provider/spotify.Provider
    → helper runner（受控子进程）
      → SpotifyScraper 读取/解析
      ↔ framed stdio: http.request / http.response
    → spotify transport bridge
      → provider 通用门禁/预算/共享令牌桶/HTTP
      → 已批准 Spotify HTTPS 端点
```

Python 提供局部 transport 适配，必要的私有 API 调用/小范围兼容补丁只在该包中，随锁定版本测试；不做进程级 requests 猴子补丁、不构建第二套业务解析器。库内部网络直连路径必须被禁用并以 socket 拦截替身证明。Go 上游包校验目标主机、方法、路径和请求头，匿名会话令牌只用于已批准 Spotify 主机；通用 provider 设施不反向导入 playlists 或 spotify。是否需扩展通用低层 transport 返回状态/有限响应头，由现有 `Client.Get` 能力差异决定，但平台特有配置仍留在 Spotify 包。

原始页面覆盖和普通歌曲数量分开：使用库的原始数据/解析扩展点核对 raw_seen、declared_total、excluded_count 和 track_count。只有总数稳定且所有位置已核对时，可把明确的非歌曲项列为不支持并提交歌曲集合；来源隐藏项造成已有歌曲身份去向不明时失败，不能悄悄丢弃后生成删除。详情补取仅针对同一稳定 track 的缺失基础字段，受同一预算控制；仍缺字段可以 unknown，不等同于成员不完整。

**替代方案**：外部解释器违反无安装要求；常驻服务扩大生命周期与资源管理；helper 内自建 HTTP 会漏掉内部请求的动态门禁/配额；在 Go 重写 SpotifyScraper 不满足指定依赖，均不采用。

### 10. 辅助进程协议、资源限制与交付

使用固定路径与参数数组启动 helper，不执行 shell；工作目录为只读应用资源目录，过滤 PYTHONPATH/PYTHONHOME 等用户 Python 注入变量。helper 不加载用户脚本、浏览器 Cookie、自动更新代码。stdio 使用带长度的 UTF-8 JSON 帧，版本、request ID、消息 kind 必须匹配；结果、http 交换、进度和结构化错误分型，stderr 仅保留脱敏限长诊断。

初始工程保护值（不是平台承诺，后续可在测试依据下调整并同步文档）：
- 每平台最多 1 个歌单抓取，App 合计最多 2 个；同绑定重复点击不新建进程。
- Spotify 平台令牌桶默认每秒 1 次、突发 1；QQ 使用既有 1–60/分钟设置，默认 45。
- 一次读取最多 20,000 个原始条目、200 个分页、500 次实际发送（包含会话、分页、补取、重试和跳转），总期限 15 分钟；单次 HTTP 30 秒，传输暂时失败至多 2 次额外尝试，限流/鉴权直接停止。
- HTTP 响应默认上限 8 MiB，最终快照/单帧上限 32 MiB，stderr 保留上限 64 KiB，原始响应累计处理量上限 256 MiB；超限报不完整/资源错误，绝不截断为成功。
- 关闭取消通信及请求，最多等 3 秒后强制终止进程树；平台封装负责 macOS/Unix 进程组和 Windows Job Object，退出先等待歌单服务再关闭 Store。

运行环境随目标平台分发：macOS 放在 `.app/Contents/Resources/spotify-helper/`，按 bundle 签名顺序签 helper/native 库再签主应用；Windows 安装器带上 `resources/spotify-helper/` 并正确卸载；Linux 如发布支持包则同样相对应用资源目录带齐依赖。不能让只含 Wails 主二进制的产物宣称完整 Spotify 支持。构建脚本按目标原生环境产出，不假设跨平台冻结可用；开发运行也先准备同版本 helper 到约定 build 目录，不走用户解释器回退。

清单记录 OS/arch、helper protocol、Python/依赖版本及校验摘要；启动按可信资源路径、版本与摘要校验，离线 `--self-test` 验证无需网络的导入、协议和解析 fixture。实际发行矩阵由现有目标构建逐项落地，未验证的系统/架构不能标为支持。SHA 校验不替代平台签名，只用于检测资源缺失/错配。

**替代方案**：one-file 冻结每次自解压会增加启动、签名和临时清理复杂度；构建后随手复制脚本不能证明依赖齐全，采用 one-folder 及显式打包验证。

### 11. 前端与事件

新增 `SyncedPlaylistView.vue`、绑定/差异/入库三个弹窗以及 `stores/syncedPlaylists.ts`。`library.ViewKind` 增加独立 `syncedPlaylist`，`AppSidebar.vue` 在普通歌单下渲染新组；名称后刷新按钮有 busy/disabled 和统一禁网原因。同步 API 返回 `entryId, trackId, upstreamPresent, inLibrary, TrackView`，不让普通列表接口通过任意前端 scope 读取全部 track。

扩展 TrackTable 的上下文策略，而不是塞入普通 collectionId：`canRemoveFromCollection/canDelete/canOrganize/canExport` 和入库动作由上下文决定；同步页面关掉成员写操作与普通导出。行新增 `upstreamMissing` 展示属性，不设置 ghost；默认选择/详情/复制照常。列上下文新增 `syncedPlaylist`，已入库列默认隐藏、48px、表头文本为空，菜单名“已入库”、绿色圆勾及 aria-label，普通与专辑偏好不变。

ReviewView 按 `importDestinations` 在按钮行最右侧展示所有有效目标，音乐库普通色，同步橙色；长名单换行/省略并可查看完整目标。ImportQueueView 增加批次目标。移出共享任务作用于所显示全部目标，前后端同一契约。

候选 DTO 分开 `localTrackId` 和 `inLibrary`；复核按精确 localTrackId 显示跳过，SearchView 按 inLibrary 显示已在库中/添加。不能以未入库徽章让用户跳转到一个曲库中不存在的选中行；可打开真实详情或在明确入库后定位曲库。

提交后统一发布 `synced-playlists:changed`（IDs/revision）、队列变化及既有本地曲目变化事件。保留 `library:changed` 作为既有通用本地实体失效通知，不把事件名当作入库统计；新增同步 track/重复投递也要让待复核离线判重和候选徽章重新读取。Pinia 使用现有 `refreshQueue` 串行合并、请求序号和按上下文失效，启动先订阅再读快照，不轮询。关闭弹窗/切歌单取消对应预览，删除后退出失效导航。

### 12. 默认细节与验收边界

为未指定的小细节采用以下默认，不扩大首期范围：
- 绑定时设置别名；本期不新增独立别名编辑器。无别名跟随上游名称，非空别名不自动变化。
- 首次绑定自动形成一次全量导入需求，后续只手动刷新，不开定时任务。
- 同步列表保留重复出现；侧栏数量为已导入出现项数，入库确认按不同 track 数计数，两者文案明确。
- 已入库列默认隐藏；同步页不提供普通歌单/标签入口或新的独立导出功能，沿用既有“导出是音乐库内容”的边界。
- 上游缺失已导入项保留且可入库，不提供本地删歌隐藏功能；忽略只作用于未导入需求。
- 新增 UI、字段、请求链与构建必须有新证据，旧 demo 或浏览器替身通过不等于真实来源/打包原生验收。

## Risks / Trade-offs

- [非公开接口变动或限制] → 单独适配器、锁定依赖、完整性失败保护旧快照；在线验收遇限制立即停止，不以不断回退求成功。
- [SpotifyScraper 私有 API/传输扩展点尚未核验] → 实施首先验证可控传输和完整页覆盖；失败作为阻塞报告，不绕过 Go 门禁或改变用户无安装要求。
- [辅助包体积与跨架构签名成本] → one-folder、精确锁定、按平台构建和无 Python 环境自检，发布清单逐项记录，不能用本机成功证明全部平台。
- [同步提交与复核并发] → 服务端计划/revision、有效需求集合和原子投递；任何删除/忽略竞态都以事务重验为准，网络取消只是减少浪费而不是唯一保护。
- [in_library 过滤遗漏] → 按用途清单覆盖列表、计数、FTS、艺术家、专辑、灰色行、候选、导出、编辑与 orphan；测试同时放置已入库和未入库相似数据。
- [共享输入误伤其他目标] → 不物理级联删除主任务，分离输入需求并覆盖“删除主输入所属歌单、另一个歌单继续”用例。
- [重复出现无原生条目身份] → 固定 Ref + occurrence 配对并公开限制，不能承诺识别上游未观察区间内的具体副本变化。
- [共享 track 的编辑影响多个歌单] → 按现有共享实体语义展示相同当前值；同步刷新不覆盖当前值，发行编辑仍检查全部真实引用而非仅音乐库可见部分。
- [完整快照较大] → 明确分页/条目/字节/发送/时间上限、差异弹窗内部滚动，达到上限报告失败不删歌；不把该保护值当作“任意长度均可同步”。

## Migration Plan

1. 首先完成 Spotify 依赖/冻结/传输前置验证和 fixture，再实现数据及业务路径；阶段尚未完整时不把新 UI 标为可用。
2. 按项目早期开发约定以清空历史应用数据验收：更新新模型初始化、SQL 视图、约束及相应模型代际检查，不编写历史 in_library 推导或数据搬迁。应用检测不兼容结构时只报告，不主动删库。
3. 同步表和曲库标记必须与导入终态投递、查询范围一起交付，不能只先发前端隐藏来保护数据。
4. 按目标平台构建 helper → 校验清单/许可证 → Wails 构建与绑定生成 → 安装包资源装配/签名 → 无外部 Python 冒烟测试；生成绑定、dist、helper 二进制均不提交。
5. 回滚使用旧应用与旧备份/用户主动清空后的测试数据；不让旧版本误读新模型，不声称支持无损向下兼容。辅助运行环境与应用整体回滚，禁止单独在线升级。
6. 真实 QQ/Spotify 验证按显式开启的有界入口执行，记录样本数量、完整性及限制；原生验证另列，未完成保留任务未勾选，不以规划校验替代实施测试。
