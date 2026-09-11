## Context

全新项目：仓库当前只有 OpenSpec 脚手架与 `prompt/` 下的需求文档，无任何实现代码。动机见 `proposal.md - Why`，行为契约见本 change 的 22 份 `specs/`。本文只解释**怎么做**与**为什么这样做**。

塑造整个架构的三条外部约束：

1. **MusicBrainz 平均 1 req/s（按 IP）**。这不是性能调优问题，而是架构约束——它决定了详情必须一次请求获取尽可能多的信息、查询变体必须渐进式、曲目清单必须懒加载。1000 行的导入在每行 1 次请求下约 17 分钟；每行 4 次变体则约 67 分钟。
2. **两个上游的字段能力差距极大**（MusicBrainz 提供演唱者/作曲/作词/厂牌/ISRC/载体/国家，iTunes 全部没有）。这决定了 Provider 接口不能是"最小公倍数"式的统一接口。
3. **Apple 条款要求 iTunes 数据只可短期缓存**，而 MusicBrainz 数据为 CC0 可长期留存。同一张表里的两类数据生命周期不同。

## Goals / Non-Goals

**Goals:**

- 一套数据模型同时承载「结构稳定的基本信息」与「可演进的丰富信息」，且后者的演进不需要改动来源/锁的存储结构。
- 上游可插拔：新增一个 provider 只需实现接口并注册，零页面代码改动。
- 出网路径单一且可管控：User-Agent、限流、缓存、TTL 全部只有一处实现。
- 匹配结果可复现：同样的输入必须产生同样的选择。
- 全部外部数据解析器可离线复现与回归测试。

**Non-Goals:**

- 不为功能 2（本地音频）与功能 3（流媒体同步）预留任何结构，唯一例外是丰富信息的**字段命名与取值范围**对齐常见音频标签规范。
- 不做跨上游的实体统一（同一首歌在 MB 与 iTunes 的记录是两条独立事实，只在 ISRC 存在时做一次桥接查询）。
- 不做分布式或多端同步，全部状态在本机单进程内。

## Decisions

### D1. 曲目拆为两张表，而不是一张宽表

`track`（基本信息，3 列 + 3 个布尔锁）与 `track_metadata`（丰富信息，约 40 列，一对一）。

**为什么**：两层的**变更频率与结构稳定性不同**。基本信息是列表页、歌单、搜索的主来源，字段永不增删，所以 3 个布尔锁就够，不需要 JSON provenance。丰富信息被明确设计为可演进（后期可能增删字段甚至拆表），且可整体为空。

**替代方案**：单张宽表 + 统一的 provenance JSON。被否决，因为这会让「每首曲目必有基本信息」这个不变量依赖于应用层检查，而不是表结构；且列表页的查询会被 40 列的行拖累。

```sql
CREATE TABLE track (
  id            INTEGER PRIMARY KEY,
  title         TEXT NOT NULL,
  artist        TEXT,
  album         TEXT,
  title_locked  INTEGER NOT NULL DEFAULT 0,
  artist_locked INTEGER NOT NULL DEFAULT 0,
  album_locked  INTEGER NOT NULL DEFAULT 0,
  created_at    DATETIME NOT NULL,
  updated_at    DATETIME NOT NULL
);
```

`track_metadata` 以 `track_id` 为主键并 `ON DELETE CASCADE`，列分组为：上游规范值（`title` / `album`）、署名原始字符串（`track_credit` / `album_credit` / `credit_spans_json`）、发行信息（`primary_type` / `secondary_types` / `release_status` / `medium_format` / `country` / `year` / `release_date` / `first_release_date`）、位置与时长（`track_number` / `track_number_text` / `track_total` / `disc_number` / `disc_total` / `duration_ms`）、描述（`genre` / `label` / `catalog_number` / `comment`）、封面（`cover_path` / `cover_source` / `cover_source_url` / `cover_width` / `cover_height` / `cover_fetched_at`）、上游标识（`recording_mbid` / `release_mbid` / `release_group_mbid` / `work_mbid` / `isrc` / `itunes_track_id` / `itunes_collection_id`）、溯源（`match_source` / `match_ref` / `matched_at`）、逐字段元信息（`provenance_json` / `locked_json`）。

`track_number_text` 单独存在，是因为黑胶的曲序是 `A1` 这类非数字编号，塞不进 `INTEGER`。

### D2. provenance 与 lock 用 JSON 列，不是每字段一列

`provenance_json` = `{"year":"musicbrainz","genre":"user",...}`，`locked_json` = `{"year":true,...}`。取值域为 provider id、`user`、`import`。

**为什么**：① 丰富信息的字段集合会演进，加字段时不必同时改两列的 schema；② 详情页需要的是「逐字段显示来源徽标」，天然是一次整体读取，没有按来源筛选曲目的查询需求。

**代价**：无法用 SQL 直接查「所有 year 来源为 user 的曲目」。这个查询目前没有需求；若将来需要，可加一张派生索引表而不必改主表。

### D3. `release` 与 `track` 之间不建外键

两者通过既有的上游标识做等值 join：`release_track.track_ref = track_metadata.match_ref AND release.provider = track_metadata.match_source`。

**为什么**：这个标识本来就存在（`match_ref` 是溯源功能自带的，不是为清单新加的）。再加一条 FK 会产生**两个可能不一致的事实来源**——FK 说这首歌属于这张发行，而 `match_ref` 说它匹配到另一条，两者漂移时无从判断谁对。

**附带收益**：删除曲目时清单不受影响，该位置UI自然回落为缺失曲目行（以下简称“灰色行”）——这正是期望行为，不需要任何额外逻辑。

### D4. `match_ref` 用 track MBID，不用 recording MBID

MB 的 track 是 (recording × release × medium × 位置) 的唯一交点，正是所需粒度。iTunes 用 `{country}:{trackId}`。

**为什么不用 recording MBID**：同一 recording 出现在几十个 release 上。用它会把 CD 版与数字版判为同一条，使 release 优选阶梯（D11）完全失去意义——既然都是同一条，选哪个版本就不再是个问题了，而它恰恰是个问题。

**为什么 iTunes 必须带 country**：同一首歌在不同地区 storefront 的 `trackId` 不同，不带 country 会让日本区与美国区的同一首歌互相覆盖。

唯一索引建为部分索引，让未匹配的曲目（`match_source IS NULL`）不受约束：

```sql
CREATE UNIQUE INDEX idx_match_ref ON track_metadata(match_source, match_ref)
  WHERE match_source IS NOT NULL;
```

「已有其他版本」这一中间状态靠 `recording_mbid` 判定——它已经存在于 `track_metadata`，判定是免费的。不做这一级，用户从 CD 版入库而专辑视图显示数字版清单时，明明有的歌会被标成缺失。

### D5. 署名用两个 TEXT 列，不建 `artist_credit` 实体表

`track_credit` / `album_credit` 存原始字符串，`credit_spans_json` 存渲染用的可点击区间。

**为什么**：可复用署名实体表（artist_credit + 有序部件表）的价值在于「多首曲目引用同一条署名」与「结构化编辑 join phrase」，两者都不是 v1 目标。更关键的是 join phrase（`feat.` / `&` / `×`）**无法从关系表无损重建**——从 `track_artist` 拼回去只能得到统一的分隔符，原始字符串的写法就丢了。而写音频标签时 `ARTIST` 取的正是原始字符串。

`credit_spans_json` 是纯渲染辅助、不参与查询，且**必须在解析时一并生成**，不能在渲染时反向猜测——渲染时已经没有解析上下文了。

同理不建 `artist_alias`：v1 中不同写法就是不同的 artist 行。

### D6. Provider 接口用可空字段 + `Capabilities()`，不做最小公倍数接口

```go
type Provider interface {
    ID() ProviderID
    DisplayName() string
    Capabilities() Capabilities
    RateLimit() (n int, per time.Duration)
    IsConfigured() error
    SearchTracks(ctx context.Context, q Query) ([]Candidate, error)
    FetchDetail(ctx context.Context, ref Ref) (*Detail, error)
    SearchCovers(ctx context.Context, q Query) ([]CoverGroup, error)
}
```

**为什么**：MB 与 iTunes 的字段交集只有曲名/艺术家/专辑/年份/曲序/碟序/时长/风格。若接口只暴露交集，MB 的演唱者、作曲、厂牌、ISRC、载体、国家全部无处安放，产品的核心价值（把角色与声优结构化）直接消失。

`Capabilities` 至少覆盖：`SecondaryTypes | Composer | Lyricist | PerformerRoles | Label | CatalogNumber | MediumFormat | Country | ISRC | CoverArt | MultiSizeCover`。

UI 的列策略以**当次结果涉及的 provider 集合**为准（聚合搜索时是被勾选的那几个，复核队列里是当前选中的那一个），而不是以注册表全集为准——否则只搜 iTunes 时也会显示一堆永远是 `—` 的列。

所有字段用 `*T` 或 `Field[T]{Value, Source, Confidence}`。缺失即缺失：用空串伪装会让「署名精确匹配」在两边都缺失时误判为匹配成功。

### D7. 每个 provider 一个进程级令牌桶，图片服务不在桶内

同一 provider 的搜索、导入匹配、详情拉取共用一个桶——它们打的是同一个受限 API，各自一个桶会让各功能互相导致其他功能触发限流。

**图片服务独立**：CAA（含它的 JSON 索引）与 `*.mzstatic.com` 都不是被限流的那个端点，改由封面工作池的并发上限约束。因此「MB 1 req/s」与「封面 8 并发」并不矛盾。唯一的交叉点：iTunes 的图片地址来自 search 响应，那一次 search 照常走 iTunes 的桶。

iTunes 的 search 与 lookup 两个端点共用一个桶。

### D8. 封面字节走 Wails AssetServer，不经 IPC 桥

```
Go 工作池取字节 -> 写入磁盘 LRU -> 经 assetserver.Options.Handler 暴露本地 URL
前端只写: <img src="/cover?key=...&size=250" loading="lazy">
```

**为什么不经 IPC 桥回传字节**：Wails 绑定按 JSON 序列化，`[]byte` 变成 base64（体积 +33%），25 行缩略图约 1MB JSON 穿过桥；且丢掉 `<img>` 自带的懒加载、滚出视口自动取消、解码不占主线程。

**为什么不让前端直接取**：① CAA 降级链与 iTunes 的 URL 构造/退档是 provider 特有逻辑，搬到前端要用 TS 再实现一遍，每加一个上游改两处；② webview 自身的 HTTP 缓存不透明，设不了上限、控不了 TTL、也无法被设置页的「清除缓存」清掉，而「封面缓存目录与上限」是明确的设置项；③ `<img>` 请求无法携带自定义 User-Agent；④ 详情抽屉确认采用的图需按内容哈希落盘并记录来源，前端写不了文件。

列表渲染时前端调**一次批量** `PrefetchCovers(refs []CoverRef)`（一批一次，不是每行一次），这样并发度由 Go 的池决定，不受 webview 每主机连接数的限制。`<img>` 请求到达时若仍在下载则等同一个结果（按缓存键做 singleflight），已在盘则直接命中。

### D9. 两个上游的封面"规格"语义不同，必须分开处理

| | 规格来源 | 处理 |
|---|---|---|
| MusicBrainz | CAA 的 JSON 索引**真实枚举**可用图片，带 `type`（Front / Back / Booklet / Medium / Tray） | 直接读索引 |
| iTunes | 只返回一个 `artworkUrl100`，大图靠**构造 URL**（未文档化行为） | 只提供 `3000 / 1200 / 600 / 100` 四档；选中时先发 HEAD 验证，失败自动退档 |

不区分这一点，会做出「iTunes 显示的规格点开是 404」的缺陷。CAA 返回 404 是常态（大量 release 没有图），降级链必须优雅落到占位图，且单张失败只影响该行。

### D10. 查询变体渐进式执行

括号语义先分四类（判定顺序写死：CV → 版本关键词 → 翻译提示词 → 文字系统差异 → 保守兜底），再生成最多 4 个变体组合，然后**先只查第一个**，产出高置信就停。

**为什么**：MB 1 req/s 下，总是查全部 4 个变体会把 1000 行导入从约 17 分钟拖到约 67 分钟，而渐进式让大多数行只消耗 1 次请求。设置中可切回全量。

**为什么判定顺序必须写死**：不加判断地移除括号内容会把 `That Girl (DJLIU Remix)` 搜成 `That Girl`，匹配到完全不相关的歌。版本关键词表必须在文字系统差异判定**之前**生效，否则日文曲名 + 拉丁文版本标识会被误判为翻译异写。兜底分支保原始字符串同时生成剥离变体，剥离变体扣 3 分作为惩罚——防止误剥离时压过正确结果。

`normalize()` 明确**不剥离括号**：标准化是无语义的机械变换（NFKC → 小写 → 罗马数字 → 假名统一 → 去空白标点），括号处理是有语义判定的独立步骤。混进去会让两者都无法单独测试。

### D11. 高置信是结构性条件，不是分数线

```
高置信 ≡ 曲名匹配 AND (署名精确匹配 OR 歌集精确匹配)
```

**为什么**：`55 + 12 + 12 = 79` 分但署名与歌集**都只是模糊匹配**，这种候选恰恰是最容易错的——两处都"差不多"往往意味着搜到了同名不同曲。而最低的高置信是 `55 + 20 = 75` 分（署名精确）。**分数高不等于高置信**，用分数线会把前者放进来、把后者挡在外面，结果恰好相反。

两个上游用不同算法（MB 用上游 relevance score 补最后 5 分，iTunes 无 score 改用 `trackTimeMillis` 的时长接近度），但**满分统一为 100、高置信的判定标准统一**——因为复核队列里「本地 + MB + iTunes」的候选要同列排序。

### D12. release 优选阶梯只有一套，两处共用

```
1. release_status = Official        2. medium_format: CD > SACD > Vinyl > Cassette > Digital Media > 其他
3. country 命中偏好列表             4. release-group.first-release-date 最早
5. CAA 有图                         6. MBID 字典序（纯为确定性）
```

第 6 级存在的唯一目的是**可复现**：前 5 级全同时必须有确定的裁决，否则同样的输入会随 map 遍历顺序产生不同结果。

**同一阶梯也用于选专辑视图的"代表 release"**（一个 release group 下多个 release 时选谁的清单）。不另写一套：两处对"哪个版本更有代表性"的判断没有理由不同，而分成两套必然漂移。

### D13. 曲目清单在首次打开专辑时懒加载

| 时机 | 行为 |
|---|---|
| 任何 `FetchDetail` | 顺带写入 `release` + `release_track`，`tracklist_state = complete`——清单在该响应里本来就有（`inc=recordings+media`），不额外花请求 |
| 导入自动入库（只做 search） | 只写 `release` 行，`tracklist_state = none` |
| 首次打开该专辑视图 | 若 `state != complete` 补一次请求，**不阻塞渲染**：已入库曲目立即显示，灰色行随后补入 |

**为什么不在匹配时就拉**：MB 1 req/s，1000 行导入若每行都补一次详情，导入时间翻倍；而用户实际打开的专辑只是其中一小部分。设置中可切换为「匹配时即拉取」。

iTunes 来源的 `release` 按 `fetched_at` 设 TTL（默认 30 天），过期视为 `none`；MB 来源为 CC0 不受限。**同一张表里两种生命周期**，靠 `provider` 列区分，由一个统一的 `isStale(release)` 判定函数集中处理。

### D14. 灰色行是表格组件的显式变体，不是专辑视图的分叉

列定义与普通行完全相同，数据来自 `release_track`。组件需要显式支持这个变体（不可选中、不打开抽屉、右键只有两项、不计入选中计数、不参与导出、不进 FTS）。

**为什么不让专辑视图自己做一个**：列是一样的，分叉会让两边逐渐漂移——加一列时只改了一边，就会出现"专辑视图少一列"的缺陷。

灰色行**不进 FTS 索引**：搜索的是用户的收藏，不是上游目录；否则搜索结果里会出现点不开的行。

灰色行的「加入曲库」**不需要再搜一次**——清单里已有 track MBID，可直接带完整溯源入库。

### D15. 匹配队列持久化到 SQLite，进度用事件推送

`import_batch` + `import_item`，`import_item.raw_row` 存原始整行 JSON 且**永不修改**（用户在复核队列改搜只影响查询，不动原值）。状态集合：`queued | matching | imported | review | failed | skipped | merged | discarded`，队列索引为 `(status, next_retry_at)`。

**为什么持久化**：1000 行 × 1 req/s ≈ 17 分钟，用户必然会中途关窗口。内存队列意味着重启即丢。

进度经 `runtime.EventsEmit` 推送，不用前端轮询——轮询在 17 分钟的任务上会产生数千次无谓的 IPC 调用，且进度更新的自然形态就是推送。

### D16. Schema 迁移：顺序编号脚本 + `schema_version` 表

每个迁移在单个事务内执行，失败整体回滚且版本号保持不变；数据库版本高于程序时拒绝写入并提示升级（而不是尝试降级）。

**为什么这是硬约束而非可选项**：`track_metadata` 被明确设计为可演进（D1、D2）。没有迁移机制，「后期可增删字段」这句话就是空话，整个两层模型的理由随之失效。

### D17. 模块划分

```
internal/store/        SQLite 访问、迁移、FTS 同步、provider_cache
internal/provider/     Provider 接口、registry、令牌桶
  ├─ musicbrainz/      端点、解析、track 粒度展开、CAA 索引
  └─ itunes/           search + lookup、country 优先级、URL 构造与 HEAD 退档
internal/matching/     normalize、括号分类、变体生成、CV 解析、置信度、release 阶梯、合并判定
internal/importer/     编码/分隔符/表头/列映射探测、XLSX、队列状态机、退避
internal/covers/       工作池、singleflight、磁盘 LRU、AssetServer handler
internal/export/       CSV / JSON 写出
app.go                 Wails 绑定层（薄，只做参数转换与事件发射）

frontend/src/
  components/          TrackTable（含 ghost 变体）、CandidateTable、DetailDrawer
  views/               Library / Artists / Albums / UpstreamSearch / Review / Settings
  stores/              按需 Pinia（任务进度、设置、当前筛选），不强制全局化
```

`internal/matching` 不依赖 `internal/provider` 的具体实现，只依赖接口与 `Candidate` 结构——这样置信度算法可以对着固定的基准测试样例（Golden Tests），不需要出网。

### D18. 上游响应先落盘再解析

`provider_cache(provider, endpoint, params_hash)` + `fetched_at` + TTL。原始响应先写入再交给解析器。

**为什么**：解析器（MB 的 artist-rels / work-rels 嵌套、iTunes 的 collection 首条特例）是本项目最容易出错的部分，而复现一次 bug 在 1 req/s 下代价高昂。落盘后可离线反复重放。

## Risks / Trade-offs

- **MB 1 req/s 使大批量导入非常慢（1000 行 ≈ 17 分钟）** → 渐进式变体使多数行只需 1 次请求；队列持久化并支持暂停和恢复，使长耗时任务可以在后台运行，无需用户持续等待；完成时提供统计信息与失败明细。
- **CAA 404 是常态，大量 release 没有封面** → 降级链逐级退到占位图；单张失败只影响该行，不重排整批、不阻塞其他列渲染。
- **iTunes 的大图尺寸靠构造 URL，属未文档化行为，可能随时失效** → 只提供 4 档；选中时先 HEAD 验证再加载，失败自动退档；缩略图始终用响应里给出的 `artworkUrl100` 构造的小尺寸，风险面最小。
- **括号语义分类会误判**（`That Girl (DJLIU Remix)` 被剥离、或 `Artist (US)` 被当成 CV） → 判定顺序写死并配基准测试样例（Golden Tests）；兜底分支同时查原始字符串与剥离变体且给剥离变体扣分；CV 拆分不确定时不拆，进复核队列交用户。
- **文本模糊去重会误判为同一首** → 作用域压到 `match_source IS NULL` 的小集合（没有标识可比的那些），且结果标为"疑似"必须用户确认，绝不自动合并。
- **FTS 索引与艺术家关系可能漂移**（改了关系但没刷索引，搜声优名搜不到） → 索引刷新与关系写入放在同一事务；用「搜索 `平野綾` 与 `涼宮ハルヒ` 都命中同一首」作为验收用例常驻回归。
- **SQLite 单写者：后台匹配队列与前台用户操作会争写锁** → 开启 WAL；匹配循环按条目提交短事务，不持有长事务；写操作集中在 store 层一处，便于统一加重试。
- **iTunes 的 TTL 合规容易被忘记**（尤其 `release` 表里 MB 与 iTunes 数据混存） → 由单一的 `isStale(release)` 判定函数集中处理，所有读取清单的路径都经过它，而不是各处自己比 `fetched_at`。
- **`provenance_json` 无法按来源查询** → 目前无此需求；若将来需要，加派生索引表，不改主表。
- **两个表格组件被复用在 6+ 处，改动的影响范围较大** → 正是因此才只做一个：分叉的维护成本更高。灰色行作为显式变体而非分支，变更时两种形态一起被测到。

## Migration Plan

全新项目，无存量数据与回滚兼容问题。部署即首次安装。

- 初始 schema 作为 `0001_init` 迁移交付，而不是在代码里 `CREATE TABLE IF NOT EXISTS`——从第一天起就走迁移路径，否则迁移机制会因为"暂时用不上"而迟迟不落地。
- 数据库位于 `os.UserConfigDir()` 下的应用目录；封面缓存目录同级且可在设置中更改。
- 首次启动引导要求填写 MB 联系方式，未填写前全局禁用出网。这是产品行为也是合规要求。
- 回滚策略：`schema_version` 高于程序支持版本时拒绝写入并提示升级，不尝试降级迁移。

## Resolved Questions

以下三项在规划时留待实现决定，现已落定：

- **SQLite 驱动**：选 `modernc.org/sqlite`。纯 Go 免 CGO，Wails 跨平台打包最省事；落地前已验证 FTS5 与 JSON1 均可用，两项都是本设计的硬依赖（D1、D2、4.9）。
- **XLSX 解析库**：选 `github.com/xuri/excelize/v2`。**字符编码检测未取现成库**：候选集只有 6 种编码，而通用检测库的判据（字节分布）无法表达本项目真正需要的领域信号——见下方 D20。编码转换本身用 `golang.org/x/text`。
- **Jaro-Winkler**：自实现（约 60 行）。金样例已固定期望行为，自实现省掉一个依赖，且便于在阈值调整时直接读懂判据。

## Decisions（实现期追加）

### D19. FTS5 用 contentless 表 + 影子表，不用外部内容表

`track_fts` 声明为 `content=''`，另建 `track_fts_shadow` 记录每个曲目**当初索引进去的那份值**；删除索引行时以影子表的值为准。

**为什么不用外部内容表（`content='track_fts_source'`）**：两个原因叠加使它不可行。① 按 `rowid` 查询外部内容表会**穿透到内容视图**，返回视图里的行而非索引里的行——因此无法用查询 `track_fts` 本身判断某行是否已被索引，而这是决定「该插入还是该先删除」的前提。② contentless 与外部内容表的 `'delete'` 命令都要求提供的列值与当初索引进去的**完全一致**，否则索引损坏（报 `database disk image is malformed`）；而刷新索引时数据往往已被改写，视图返回的是新值。

影子表同时解掉这两点：它既是「是否已索引」的权威记录，也保存着删除所需的那份旧值，使 `syncFTS` 的正确性**与调用时机无关**——无论在数据更新之前还是之后调用都成立。

**代价**：影子表与 FTS 表必须在同一事务内一起维护，多一张表要同步。相对于「索引静默损坏」这一失败模式，这个代价是划算的。

### D20. 编码探测用领域信号打分，而不是通用字节分布

打分器对解码结果统计三类信号，而非单纯的「CJK 字符占比」：

```
假名                    强正信号  ×3.0    误解码几乎不产出连续全角假名
常用汉字 / 谚文          弱正信号  ×0.6
罕用汉字                 负信号   ×1.5    CJK 扩展区、康熙部首等
替换字符 / 控制字符      强负信号  ×10
半角片假名               强负信号  ×10    误解中文字节的典型产物
西里尔 / 希腊 / IPA 混排  负信号   ×4.0    误解简繁的典型产物
```

**为什么不能只数 CJK 占比**：GB18030 会把 Shift_JIS 的日文字节解成一串**合法但荒谬**的汉字（`嬋柤傾乕僥`），其 CJK 占比反而**高于**正确解码的结果——正确结果里有大量假名，而假名不计入汉字。同理，把单字节文本按 UTF-16 解码几乎不产生替换字符（任意字节对都是合法码位），却会得到一堆罕用汉字与私用区字符。

这三条惩罚项分别由 Shift_JIS、GB18030、Big5 三个金样例逐个暴露出来，每一条都对应一次真实的误判。
