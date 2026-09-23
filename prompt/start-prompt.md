# Sonexa — 音乐库应用 v1 实现提示词

本文件是技术与设计的主文档。功能维度的简明清单见 `./features.md`。

---

## 1. 项目概述与技术栈

Sonexa 是一个桌面音乐库应用，为用户管理**音乐收藏本身**——收藏的存在不依赖本地音频文件是否存在。

**技术栈**

| 层 | 选型 |
|---|---|
| 壳 | Wails v2 |
| 后端 | Go |
| 前端 | Vue 3 + TypeScript |
| 样式 | Tailwind CSS v4 |
| 组件 | shadcn-vue |
| 图标 | Lucide Icons |
| 状态 | Pinia（按需，不强制全局化） |
| 存储 | SQLite（含 FTS5） |

---

## 2. v1 范围与非目标

### 2.1 产品的三大功能与本期取舍

| | 功能 | v1 |
|---|---|---|
| 1 | **管理音乐收藏**：标签、歌单、导入、导出、搜索 | **本期唯一焦点** |
| 2 | 本地音频管理：扫描、关联、播放、写入音频文件内部的元数据标签 | 不实现 |
| 3 | 流媒体账户关联与歌单同步 | 不实现 |

功能 2 / 3 不在本期实现。唯一的例外是字段设计：丰富信息的字段需以「未来能写入音频文件内部的元数据标签」为目标，字段名与取值范围应对齐常见音频标签规范。除此之外不为它们留任何结构。

### 2.2 功能清单概览

详细描述见 `./features.md`，此处仅为定位索引：

| 编号 | 功能 | 一句话 |
|---|---|---|
| F1 | 曲目库管理 | 基本信息（稳定）+ 丰富信息（可演进），逐字段来源与用户锁 |
| F2 | 歌单与标签 | 后台同一套模型，前端两种呈现 |
| F3 | 艺术家与角色关系 | 关系表存储；`角色(CV:声优)` 拆为两个艺术家 |
| F4 | 本地搜索与浏览视图 | 在当前列表内筛选 + 艺术家视图 + 专辑视图（未入库曲目显示为灰色行） |
| F5 | 上游 Provider 抽象 | 统一接口 + registry + 能力声明 + 独立限流 |
| F6 | 上游搜索 | 结果粒度是 track，可直接加入曲库 |
| F7 | 封面获取 | 三场景 × 尺寸阶梯 × 来源跟随 |
| F8 | 表格导入 | CSV/TSV/XLSX，自动探测优先 |
| F9 | 匹配流程与置信度 | 每个上游各用自己的算法，但满分与「高置信」的判定标准统一 |
| F10 | 复核队列 | 单一界面处置全部人工判断 |
| F11 | 编辑与重新匹配 | 重新匹配的唯一入口，逐字段确认 |
| F12 | 导出 | CSV/JSON，不打包封面 |
| F13 | 设置 | MB 联系方式是全局出网前置条件 |
| F14 | 匹配溯源与去重 | 用记录而非猜测判断同一性 |

### 2.3 明确的非目标

- **Apple Music API**：需 Apple Developer Program（约 $99/年）+ MusicKit 私钥 + ES256 JWT。v1 改用免费无鉴权的 **iTunes Search API**。Provider 接口需为将来接入留出空间。
- **艺术家详情页**：v1 只有「艺术家视图」（按艺术家过滤的曲目列表），不做简介/头像/外部链接。
- **用户维度的专辑表**：v1 建 `release` 表（4.10），但它是**上游事实的镜像**——存的是"这张专辑总共有哪些曲目"，用于把未入库曲目显示为灰色行。它不承载任何用户数据：专辑评分、收藏状态、实体唱片信息都不在 v1。没有上游标识的曲目（手动录入、`[无需匹配]` 入库）仍按派生分组呈现，见 7.6。
- **艺术家别名表**（`artist_alias`）：让同一艺术家的多种写法（`平野綾` / `平野 綾` / `Aya Hirano` / 假名读法）指向同一行 artist。v1 不建，不同写法就是不同的 artist 行。
- **可复用署名实体表**（`artist_credit` + 部件表）：把署名串拆成有序部件（艺术家 + 该处的署名写法 + 连接词），按 position 拼回去可无损还原原串，且多首曲目可引用同一条署名。v1 改用两个 TEXT 列存原串，理由见 4.4。
- 音频指纹匹配、批量编辑、编辑历史撤销、智能歌单。

---

## 3. 上游 Provider 契约

### 3.1 接口与 registry

```go
type ProviderID string

type Provider interface {
    ID() ProviderID                    // "musicbrainz" | "itunes" | ...
    DisplayName() string
    Capabilities() Capabilities        // 声明能提供哪些字段/能力
    RateLimit() (n int, per time.Duration)
    IsConfigured() error               // 未满足前置条件时返回可展示的错误
    SearchTracks(ctx context.Context, q Query) ([]Candidate, error)
    FetchDetail(ctx context.Context, ref Ref) (*Detail, error)
    SearchCovers(ctx context.Context, q Query) ([]CoverGroup, error)
}
```

**硬性约束**

1. **UI 中所有「选择上游」的位置必须从 `registry.List()` 动态渲染，禁止硬编码 provider 名称或数量。**
2. **每个受限端点一个进程级共享令牌桶**：同一 provider 的搜索、导入、详情拉取共用同一个桶——它们打的是同一个受限 API，否则各功能会互相把对方打到限流。**图片服务不在这个桶里**：CAA 与 `*.mzstatic.com` 独立于受限 API（见 3.3），改由封面工作池的并发上限约束（见 3.4）。
3. **所有字段使用可空类型**（`*string` / `*int` 或统一的 `Field[T]{Value, Source, Confidence}`）。缺失即缺失，不用空串或零值伪装。
4. 前端**不自行发出任何出网请求**，出网一律经 Go 侧绑定方法。否则 User-Agent 与限流无法统一管控。封面图片字节是唯一的传输特例：仍由 Go 取，但不经 IPC 桥回传，而是由 Go 通过 Wails 的 AssetServer handler 以本地 URL 供 `<img>` 加载（见 3.4）。

`Capabilities` 至少需覆盖以下位，用于驱动 UI 的列显示策略：

```
SecondaryTypes | Composer | Lyricist | PerformerRoles | Label | CatalogNumber
MediumFormat   | Country  | ISRC     | CoverArt       | MultiSizeCover
```

UI 规则：以**当次结果涉及的 provider 集合**为准（聚合搜索时是被勾选的那几个，复核队列里是当前选中的那一个）。该集合中所有 provider 都不支持某列 → 隐藏该列；只有部分支持 → 显示该列，不支持的行填 `—`。

### 3.2 字段支持对照表（MusicBrainz vs iTunes Search API）

| 字段 | MusicBrainz | iTunes Search API |
|---|---|---|
| 曲名 / 艺术家 / 专辑名 | Y | Y |
| 曲目级主要艺术家署名 | Y（结构化 artist-credit，含 join phrase） | 仅扁平字符串 `artistName` |
| 发行/专辑级主要艺术家署名 | Y | 需从 collection 推断 |
| 演唱者（含虚拟歌姬） | Y（recording artist-rels，`vocal` / `performer`） | **N** |
| 器乐演奏者 | Y（`instrument`，attribute 为乐器名） | **N** |
| 作曲 | Y（经 Work：recording→work→`composer`） | **N** |
| 作词 | Y（`lyricist`，`writer` 兜底） | **N** |
| 角色 / 声优关系 | Y（artist type=`Character` + `voice` 关系） | **N** |
| Primary Type | Y（`release-group.primary-type`） | **N** |
| Secondary Types | Y（数组） | **N** |
| 年份 | Y（`release.date` + `release-group.first-release-date`） | Y（`releaseDate`） |
| 音轨号 / 总轨数 | Y | Y（`trackNumber` / `trackCount`） |
| 碟号 / 总碟数 | Y（`media[].position` / `release.medium-count`） | Y（`discNumber` / **`discCount`**） |
| 风格 | Y（`genres[]`，源自社区标签，很稀疏） | Y（`primaryGenreName`，**被 country 本地化**） |
| 国家 / 地区 | Y（`release.country` + `release-events[]`） | **N**（只回显查询用的 country） |
| 载体格式 | Y（`media[].format`：CD / SACD / Vinyl / Digital Media …） | **N**（永远是数字发行） |
| 厂牌 | Y（`label-info[].label.name`） | **N** |
| 目录号 | Y（`label-info[].catalog-number`） | **N** |
| ISRC | Y（`inc=isrcs`） | **N** |
| 时长 | Y（`tracks[].length`，毫秒） | Y（`trackTimeMillis`） |
| 封面 | Cover Art Archive（独立服务，不占 MB 限流） | `artworkUrl100`（尺寸可构造） |
| 上游 relevance score | Y（search 接口返回 `score`） | **N** |

> **这张表直接决定了 Provider 接口不能是"最小公倍数"式的统一接口。** 必须用可空字段 + `Capabilities()` 声明，让调用方与 UI 按能力分支。

### 3.3 请求方式与限流

**MusicBrainz**

- 速率：**平均 1 req/s（按 IP）**，无 API key。
- **必须**携带 `User-Agent: Sonexa/<版本> ( <用户填写的联系方式> )`，否则可能被限流或 403。这是 HTTP 头，不是查询参数。
- Cover Art Archive 是独立服务，其请求**不占用** MB 的 1 req/s 配额。

搜索（结果需展开为 track 粒度）：

```
GET /ws/2/recording?fmt=json&limit=25
    &inc=artist-credits+releases+isrcs
    &query=recording:"<曲名>" AND artist:"<艺术家>" AND release:"<专辑>"
```

返回的每个 recording 携带 `releases[].media[].track[]`。把它展开为 **(recording × release × medium × track)** 即为 track 粒度候选，恰好覆盖列表所需的全部展示列。

详情（**关键：1 req/s 下必须一次拉满，绝不拆成多次调用**）：

```
GET /ws/2/release/{mbid}?fmt=json&inc=
      artist-credits+labels+recordings+release-groups+media
    + artist-rels+recording-level-rels+work-rels+work-level-rels
    + genres+isrcs+annotation
```

这一个请求即可取得：曲目/发行级署名、演唱者、器乐演奏者（`recording-level-rels`）、作曲/作词（`work-rels` + `work-level-rels`，因为作曲作词挂在 **Work** 上而非 recording 上）、Primary/Secondary Type、年份、曲序/碟序/总数、风格、国家、厂牌、目录号、载体格式、时长、ISRC、注释。

`inc=recordings+media` 使响应**已经包含该 release 的完整曲目清单**（`media[].tracks[]`，含 track MBID / 曲序 / 标题 / 时长 / recording MBID）。执行这个请求时应顺带写入 `release` + `release_track`（4.10）——清单是免费的，丢掉它等于以后要为同一份数据再花一次 1 req/s。

唯一需要额外请求的是角色↔声优关系：

```
GET /ws/2/artist/{mbid}?fmt=json&inc=artist-rels
```

仅当某个 artist 的 `type == "Character"` 时才发起，并缓存结果。

**iTunes Search API**

- 无鉴权、无 API key。速率约 **20 req/min（按 IP）**。
- 搜索端点：`GET https://itunes.apple.com/search?media=music&entity=song&term=<q>&country=<cc>&limit=<n>`
- 清单端点（取专辑完整曲目，见 4.10）：`GET https://itunes.apple.com/lookup?id=<collectionId>&entity=song&country=<cc>&limit=200`
  返回的 `results[0]` 是 collection 自身，其后才是各曲目；`limit` 需大于 `trackCount`，否则清单被截断。两个端点共用同一个令牌桶。
- **只传 `country`，不要传 `lang`**：`lang` 仅支持 `en_us` / `ja_jp`，而真正决定返回原生标题的是 `country`。
- `country` 取值优先级：

```
用户手动选择  >  文字系统探测建议  >  设置中的默认地区（默认跟随系统 locale）

文字系统探测规则（仅作建议，需在 UI 上明示）：
  含平假名/片假名        -> jp
  含谚文                 -> kr
  含简体特有字           -> cn
  含繁体特有字           -> tw
  含西里尔字母           -> ru
  其余                   -> 设置中的默认地区
探测命中时在界面上提示：「检测到日文，已建议使用 日本 地区」
```

**通用重试策略**

- 429 / 5xx / 超时 → 指数退避重排队，尊重 `Retry-After` 头。
- 同一条目累计失败 **5 次**后标记为失败状态，不再自动重排。

### 3.4 封面策略

**三个使用场景 × 尺寸要求**

| 场景 | 尺寸 | 缓存 |
|---|---|---|
| 上游搜索列表缩略图 | ~250px | 内存 + 磁盘 LRU，有 TTL，不进正式库 |
| 复核队列列表缩略图 | ~250px | 同上 |
| 详情抽屉 | 可得的最高质量 | 用户确认采用后按内容哈希落盘，DB 记录来源 |

**自动获取时来源跟随结果来源**（不跨源取图）：

```
结果来自 MusicBrainz  ->  Cover Art Archive
结果来自 iTunes       ->  iTunes 图片地址
```

**降级链**

```
MusicBrainz (CAA):
  /release/{mbid}/front-1200
    -> /release/{mbid}/front-500
    -> /release-group/{rgid}/front-1200
    -> 占位图
  （CAA 返回 404 是常态，大量 release 没有图，必须优雅降级）
  列表缩略图直接用 /release/{mbid}/front-250

iTunes:
  把 artworkUrl100 中的 "100x100bb.jpg" 替换为目标尺寸
  3000x3000bb -> 1200x1200bb -> 600x600bb -> artworkUrl100 原值
```

**两个上游的"规格"语义不同，必须区分处理**

| | 规格来源 | 处理 |
|---|---|---|
| MusicBrainz | CAA 的 JSON 索引**真实枚举**可用图片，且带 `type`（Front / Back / Booklet / Medium / Tray） | 直接读索引 |
| iTunes | 只返回一个 `artworkUrl100`，大图尺寸靠**构造 URL**（未文档化行为） | 只提供 `3000 / 1200 / 600 / 100` 四档；选中时先发 HEAD 验证，失败自动退档 |

> 不区分这一点，会做出「iTunes 显示的规格点开是 404」的缺陷。

**详情抽屉的手动重新获取**

用曲名 / 艺术家 / 专辑名向**所有**上游查询，结果按置信度排序；**同一结果返回的多个规格收拢为一条**并标注规格。MB 侧此处可展示 Front 之外的图（Back / Booklet），列表缩略图只用 Front。

**合规**：iTunes / Apple 图片版权受限，本地缓存自用可行，**导出时不打包图片**。

**列表缩略图的并发获取**

取缩略图是**延迟受限**而非带宽受限的：CAA 由 archive.org 提供，单张常需数秒，且 404 是常态。串行取图会让列表长时间空白，因此必须并发。

- 一个**进程级封面工作池，默认 8 并发**（设置中可调，建议不超过 16）。上游搜索列表与复核队列列表**共用同一个池**。
- **取图不占用 provider 的 API 令牌桶**：CAA 与 `*.mzstatic.com` 都不是被限流的那个端点（见 3.3），所以「MB 1 req/s」与「封面 8 并发」并不矛盾。两点补充：CAA 的 JSON 索引虽属元数据，但同样由 CAA 服务，不占 MB 的桶，与图片一起受封面池的并发上限约束；iTunes 的图片地址来自 search 响应，那一次 search 调用照常走 iTunes 的桶。
- 按缓存键合并并发请求：同一 release 出现在多行时只取一次，后到的请求等同一个结果。
- **可见性优先**：先取视口内的行；滚出视口且尚未开始的任务取消或降级。
- 单张失败（含 CAA 404）只影响该行 → 占位图。不重排整批，不阻塞其他列渲染。

**图片的传输路径（不走 IPC 桥，也不让前端出网）**

```
Go 工作池取字节 -> 写入磁盘 LRU -> 经 Wails 的 assetserver.Options.Handler 暴露本地 URL
前端只写: <img src="/cover?key=...&size=250" loading="lazy">
```

- 列表渲染时前端调**一次批量** `PrefetchCovers(refs []CoverRef)`（一批一次，不是每行一次）。这样并发度由 Go 的池决定，不受 webview 每主机连接数的限制。`<img>` 的请求到达时若仍在下载则等同一个结果，已在盘则直接命中。
- **为什么不经 IPC 桥回传字节**：Wails 绑定按 JSON 序列化，`[]byte` 会变成 base64（体积 +33%），25 行缩略图约 1MB JSON 穿过桥，且丢掉 `<img>` 自带的懒加载、滚出视口自动取消、解码不占主线程。
- **为什么不让前端直接取**：① CAA 降级链与 iTunes 的 URL 构造/退档是 provider 特有逻辑，搬到前端就要用 TS 再实现一遍，每加一个上游改两处；② webview 自身的 HTTP 缓存不透明，设不了上限、控不了 TTL、也无法被设置页的 `[清除缓存]` 清掉，而「封面缓存目录与上限」是明确的设置项（7.11）；③ `<img>` 请求无法携带自定义 User-Agent；④ 详情抽屉确认采用的图需按内容哈希落盘并记录来源，前端写不了文件。

---

## 4. 数据模型

### 4.1 分层原则

```
track             基本信息。结构稳定，字段永不增删。是列表页与搜索的主来源，
                  也是"用户歌单"的稳定载体。每首曲目必有。

track_metadata    丰富信息。可演进（后期可能增删字段、可能拆表）。用于详情展示
                  与未来写入音频文件内部的元数据标签。可整体为空。

release           发行的完整曲目清单（专辑 / EP / 单曲），见 4.10。它是**上游事实的镜像**，
release_track     不是用户数据：用户拥有哪些曲目由 track 决定，release 只回答"这张专辑
                  总共有哪些曲目"。专辑视图据此把未入库的曲目显示为灰色行。

provider_cache    上游原始响应缓存。按 (provider, endpoint, params_hash) 存储，
                  带 TTL 与 fetched_at。
```

> `release` 与 `track` 之间**没有外键**。两者通过既有的上游标识关联（`release_track.track_ref` ↔ `track_metadata.match_ref`，见 4.10），因为这个标识本来就存在，再加一条 FK 会产生两个可能不一致的事实来源。

### 4.2 基本信息表

```sql
CREATE TABLE track (
  id            INTEGER PRIMARY KEY,
  title         TEXT NOT NULL,
  artist        TEXT,              -- 纯字符串，用户看到与编辑的那个
  album         TEXT,
  title_locked  INTEGER NOT NULL DEFAULT 0,   -- 用户手动修改过则加锁
  artist_locked INTEGER NOT NULL DEFAULT 0,
  album_locked  INTEGER NOT NULL DEFAULT 0,
  created_at    DATETIME NOT NULL,
  updated_at    DATETIME NOT NULL
);
```

这三个字段只用 3 个 bool 锁，不需要 JSON provenance——因为字段集合是固定的。

**匹配成功时的默认行为**：`*_locked = 0` 的字段用上游规范值覆盖（保持基本与丰富信息一致），`locked = 1` 的不动。

### 4.3 丰富信息表

```sql
CREATE TABLE track_metadata (
  track_id           INTEGER PRIMARY KEY REFERENCES track(id) ON DELETE CASCADE,

  -- 与基本信息同名但为上游规范值
  title              TEXT,
  album              TEXT,

  -- 署名原串：原样保真，写标签与显示的主字段，不由关系表推导
  track_credit       TEXT,     -- 曲目级主要艺术家署名 "涼宮ハルヒ(CV:平野綾)"
  album_credit       TEXT,     -- 发行/专辑级主要艺术家署名
  credit_spans_json  TEXT,     -- 渲染辅助：署名串中的可点击区间，见 4.7

  -- 发行信息
  primary_type       TEXT,     -- Album | Single | EP | Broadcast | Other
  secondary_types    TEXT,     -- JSON 数组：Compilation | Soundtrack | Live |
                               -- Remix | DJ-mix | Mixtape/Street | Demo |
                               -- Audiobook | Spokenword | ...
  release_status     TEXT,     -- Official | Promotion | Bootleg | Pseudo-Release
  medium_format      TEXT,     -- CD | SACD | Vinyl | Cassette | Digital Media | ...
  country            TEXT,
  year               INTEGER,
  release_date       TEXT,
  first_release_date TEXT,     -- release-group 的首次发行日，展示优先用它

  track_number       INTEGER,
  track_number_text  TEXT,     -- 黑胶可能是 "A1" 这类非数字编号
  track_total        INTEGER,
  disc_number        INTEGER,
  disc_total         INTEGER,
  duration_ms        INTEGER,

  genre              TEXT,
  label              TEXT,
  catalog_number     TEXT,
  comment            TEXT,

  -- 封面：文件不入库
  cover_path         TEXT,
  cover_source       TEXT,
  cover_source_url   TEXT,
  cover_width        INTEGER,
  cover_height       INTEGER,
  cover_fetched_at   DATETIME,

  -- 上游标识（冗余存储，不做唯一键，供后续功能与跨上游桥接使用）
  recording_mbid       TEXT,
  release_mbid         TEXT,
  release_group_mbid   TEXT,
  work_mbid            TEXT,
  isrc                 TEXT,
  itunes_track_id      TEXT,
  itunes_collection_id TEXT,

  -- 匹配溯源，见 4.6
  match_source       TEXT,
  match_ref          TEXT,
  matched_at         DATETIME,

  -- 逐字段来源与用户锁
  provenance_json    TEXT,     -- {"year":"musicbrainz","genre":"user",...}
  locked_json        TEXT,     -- {"year":true,...}

  updated_at         DATETIME NOT NULL
);
```

**`provenance_json` 的取值**：provider id（`musicbrainz` / `itunes` / …）、`user`（用户手动修改）、`import`（来自导入文件原值）。

**为什么用 JSON 存 provenance 而不是每字段一列**：字段集合会演进，加字段时不必改 schema；且详情页需要的是「逐字段显示来源徽标」，天然是一次整体读取。

### 4.4 艺术家与角色

```sql
CREATE TABLE artist (
  id             INTEGER PRIMARY KEY,
  name           TEXT NOT NULL,
  sort_name      TEXT,
  type           TEXT,        -- person|group|character|orchestra|choir|other|NULL
  disambiguation TEXT,
  mbid           TEXT UNIQUE, -- 可空：手动录入的艺术家没有
  created_at     DATETIME NOT NULL,
  updated_at     DATETIME NOT NULL
);
CREATE UNIQUE INDEX idx_artist_name ON artist(name, COALESCE(disambiguation, ''));

-- 角色 <-> 声优关系，需求「一首歌同时挂在角色与声优名下」的核心
CREATE TABLE artist_link (
  from_artist_id INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  to_artist_id   INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  type           TEXT NOT NULL,   -- voice_of | member_of | alias_of
  PRIMARY KEY (from_artist_id, to_artist_id, type)
);

CREATE TABLE track_artist (
  track_id      INTEGER NOT NULL REFERENCES track(id) ON DELETE CASCADE,
  artist_id     INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  role          TEXT NOT NULL,
  credited_name TEXT,        -- 该曲目上的实际署名写法，可能与 artist.name 不同
  attribute     TEXT,        -- role=instrument 时存乐器名
  position      INTEGER,     -- 同 role 内的顺序
  source        TEXT,        -- musicbrainz | itunes | user | import
  PRIMARY KEY (track_id, artist_id, role)
);
CREATE INDEX idx_track_artist_lookup ON track_artist(artist_id, role);
```

**`role` 枚举**

```
vocal | instrument | performer | composer | lyricist | writer | arranger
| producer | remixer | conductor | character | album_artist
```

**写入音频文件内部的元数据标签时的取值映射**（v2 使用，v1 需保证字段齐备）

| 标签 | 取值 |
|---|---|
| `ARTIST` | `track_metadata.track_credit`（原串） |
| `ALBUMARTIST` | `track_metadata.album_credit`（原串） |
| `COMPOSER` | `role = composer` 的艺术家名，`; ` 连接 |
| `LYRICIST` | `role = lyricist` |
| `PERFORMER` | `role ∈ {vocal, instrument, performer}` |

**v1 明确不建的表**：`artist_alias`（别名 / 罗马字 / 假名读法）、`artist_credit` + `artist_credit_part`（可复用署名实体）。前者留给「同一艺术家不同写法合并」需求；后者不需要，因为跨曲目复用署名不是本期目标，两个 TEXT 列足够，且 join phrase（`feat.` / `&` / `×`）无法从关系表无损重建。

### 4.5 歌单与标签（统一模型）

```sql
CREATE TABLE collection (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL,        -- 'playlist' | 'tag'
  color      TEXT,
  sort_order INTEGER,
  created_at DATETIME NOT NULL
);
CREATE UNIQUE INDEX idx_collection_name ON collection(kind, name);

CREATE TABLE collection_item (
  collection_id INTEGER NOT NULL REFERENCES collection(id) ON DELETE CASCADE,
  track_id      INTEGER NOT NULL REFERENCES track(id) ON DELETE CASCADE,
  position      INTEGER,
  added_at      DATETIME NOT NULL,
  PRIMARY KEY (collection_id, track_id)
);
```

| | `kind = playlist` | `kind = tag` |
|---|---|---|
| `position` | 有意义，支持拖拽排序 | 忽略，按曲名/添加时间排序 |
| 侧栏呈现 | 「歌单」区，有序列表 | 「标签」区，chip |
| 详情抽屉呈现 | 不显示 | 显示为 chips |

后台一套 CRUD：`AddToCollection(collectionID, trackIDs...)` / `RemoveFromCollection(...)`。

**「全部曲目」是虚拟视图，不占 `collection` 的行。**

### 4.6 匹配溯源

```sql
-- 位于 track_metadata（见 4.3）
match_source  TEXT      -- 'musicbrainz' | 'itunes' | NULL
match_ref     TEXT
matched_at    DATETIME

CREATE UNIQUE INDEX idx_match_ref ON track_metadata(match_source, match_ref)
  WHERE match_source IS NOT NULL;
```

**`match_ref` 取值**

| provider | 取值 | 理由 |
|---|---|---|
| `musicbrainz` | **track MBID**（`media[].tracks[].id`） | MB 的 track 是 (recording × release × medium × 位置) 的唯一交点，正是所需粒度。**不可用 recording MBID**——同一 recording 出现在几十个 release 上，用它会把 CD 版与数字版判为同一条，使 release 优选阶梯（5.7）失去意义。 |
| `itunes` | `{country}:{trackId}`，例 `jp:534038851` | 同一首歌在不同地区 `trackId` 不同，必须带 country 才唯一。 |

**赋值时机**

| 时机 | 行为 |
|---|---|
| 上游搜索页 `[加入曲库]` | 写入 |
| 复核队列 `[接受]` 某条上游候选 | 写入 |
| 导入流程自动入库（6.3 的直接入库分支） | 写入 |
| 编辑页重新匹配并确认保存 | 覆盖为新值 |
| 用户在编辑页手动填入 MBID | 写入（需格式校验） |
| 复核队列 `[无需匹配]` | 置 NULL |
| 用户在编辑页手动清空 | 置 NULL |

**唯一约束带来的行为要求**

```
上游搜索结果页渲染每行时预查本地：
  已存在 -> 按钮显示 [已在库中]（禁用，可点击跳转到该曲目）
  不存在 -> [加入曲库]
真正 INSERT 时若仍撞唯一键（并发/竞态）-> 静默转为"跳转到已有曲目"，不报错
```

### 4.7 署名可点击区间

`credit_spans_json` 是**纯渲染辅助数据，不参与查询**。在解析署名时一并生成，不要在渲染时反向猜测。

```json
{
  "text": "涼宮ハルヒ(CV:平野綾)",
  "spans": [
    {"start": 0, "end": 5,  "artist_id": 42},
    {"start": 9, "end": 12, "artist_id": 17}
  ]
}
```

### 4.8 导入与复核队列表

```sql
CREATE TABLE import_batch (
  id           INTEGER PRIMARY KEY,
  filename     TEXT NOT NULL,
  encoding     TEXT,
  delimiter    TEXT,
  column_map   TEXT,       -- JSON：列索引 -> 字段名
  total_rows   INTEGER,
  status       TEXT,       -- running | paused | done
  created_at   DATETIME NOT NULL,
  finished_at  DATETIME
);

CREATE TABLE import_item (
  id            INTEGER PRIMARY KEY,
  batch_id      INTEGER NOT NULL REFERENCES import_batch(id) ON DELETE CASCADE,
  row_index     INTEGER NOT NULL,
  raw_row       TEXT NOT NULL,   -- JSON：原始整行，永不修改
  in_title      TEXT,            -- 映射后的输入值
  in_artist     TEXT,
  in_album      TEXT,
  in_year       INTEGER,
  in_track_no   INTEGER,
  in_duration_ms INTEGER,
  status        TEXT NOT NULL,   -- queued | matching | imported | review
                                 -- | failed | skipped | merged | discarded
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_error    TEXT,
  last_error_at DATETIME,
  next_retry_at DATETIME,
  candidates    TEXT,            -- JSON：候选结果快照（含来源、置信度、变体标注）
  tried_variants TEXT,           -- JSON：已尝试的查询变体，用于界面提示
  track_id      INTEGER,         -- 入库后的曲目 id
  merged_into   INTEGER,         -- [合并] 时指向库中已有曲目
  updated_at    DATETIME NOT NULL
);
CREATE INDEX idx_import_item_queue ON import_item(status, next_retry_at);
```

**队列必须持久化**：可暂停、可恢复，应用重启后继续。

### 4.9 全文检索

用 FTS5 建立曲目检索索引，内容至少包含：曲名、艺术家串、专辑名、`track_credit`、以及**该曲目所有关联艺术家名的拼接**（后者是「搜索 `平野綾` 与搜索 `涼宮ハルヒ` 都能命中同一首歌」的实现基础）。艺术家变更时同步刷新索引。

> 灰色行（4.10 的未入库曲目）**不进 FTS 索引**。搜索的是用户的收藏，不是上游目录；否则搜索结果里会出现点不开的行。

### 4.10 发行与曲目清单

**目的**：让专辑视图能显示「这张专辑共 12 首，你有 8 首」，未入库的 4 首以灰色行呈现。

```sql
CREATE TABLE release (
  id                 INTEGER PRIMARY KEY,
  provider           TEXT NOT NULL,        -- 'musicbrainz' | 'itunes'
  provider_ref       TEXT NOT NULL,        -- MB: release MBID / iTunes: {country}:{collectionId}
  release_group_ref  TEXT,                 -- MB: release-group MBID / iTunes: NULL
  title              TEXT NOT NULL,
  album_credit       TEXT,
  primary_type       TEXT,                 -- Album | Single | EP | Broadcast | Other
  secondary_types    TEXT,                 -- JSON 数组
  release_status     TEXT,
  medium_format      TEXT,
  country            TEXT,
  release_date       TEXT,
  first_release_date TEXT,
  track_total        INTEGER,              -- 上游声明的总曲目数
  disc_total         INTEGER,
  cover_path         TEXT,                 -- 发行级封面，复用 3.4 的获取与缓存策略
  cover_source_url   TEXT,
  tracklist_state    TEXT NOT NULL,        -- none | partial | complete | failed
  fetched_at         DATETIME,             -- 清单拉取时间
  updated_at         DATETIME NOT NULL
);
CREATE UNIQUE INDEX idx_release_ref ON release(provider, provider_ref);
CREATE INDEX idx_release_group ON release(release_group_ref)
  WHERE release_group_ref IS NOT NULL;

CREATE TABLE release_track (
  release_id     INTEGER NOT NULL REFERENCES release(id) ON DELETE CASCADE,
  disc_number    INTEGER NOT NULL DEFAULT 1,
  track_number   INTEGER NOT NULL,
  track_ref      TEXT,      -- 与 track_metadata.match_ref 同一取值方案（见 4.6）
  recording_ref  TEXT,      -- MB: recording MBID。用于"你有此曲的其他版本"判定
  title          TEXT NOT NULL,
  track_credit   TEXT,
  duration_ms    INTEGER,
  track_number_text TEXT,   -- 黑胶的 "A1" 这类非数字编号
  PRIMARY KEY (release_id, disc_number, track_number)
);
CREATE INDEX idx_release_track_ref ON release_track(track_ref)
  WHERE track_ref IS NOT NULL;
```

**`track_ref` 的取值方案与 `match_ref` 完全一致**（4.6）：MB 用 track MBID，iTunes 用 `{country}:{trackId}`。因此判断某个清单位置是否已入库就是一次等值 join，不需要新的关联约定：

```sql
release_track.track_ref = track_metadata.match_ref
  AND release.provider  = track_metadata.match_source
```

**每个清单位置的三种状态**

| 状态 | 判据 | 呈现 |
|---|---|---|
| 已入库 | 上面的等值 join 命中 | 正常行，可点开详情 |
| 已有其他版本 | join 未命中，但 `recording_ref` 命中某曲目的 `track_metadata.recording_mbid` | 正常行 + 小徽章「其他版本」，点击跳到已有的那首 |
| 未入库 | 都未命中 | **灰色行**，见 7.6 |

> 中间那个状态不能省。用户从 CD 版入库、专辑视图显示的是数字版清单时，同一首歌的 track MBID 不同——只做等值 join 会把用户明明有的歌标成缺失。`recording_mbid` 已存在于 4.3，判定是免费的。

**清单的获取时机**

| 时机 | 行为 |
|---|---|
| 任何 `FetchDetail` 执行时 | 顺带写入 `release` + `release_track`，`tracklist_state = complete`。**清单在该响应里本来就有**（3.3 的 MB 详情请求已含 `recordings+media`），不额外花一次请求 |
| 导入自动入库（6.2，只做 search） | 只写 `release` 行，`tracklist_state = none`——search 响应不含完整清单 |
| **首次打开该专辑视图** | 若 `tracklist_state != complete`，补一次请求（MB 详情 / iTunes lookup，见 3.3），走该 provider 的令牌桶。**不阻塞渲染**：已入库曲目立即显示，灰色行随后补入 |
| 手动 `[补全清单]` | 同上，强制重取 |

选择「首次打开时懒加载」而非匹配时就拉：MB 是 1 req/s，导入 1000 行若每行都补一次详情请求，导入时间会翻倍；而用户实际打开的专辑只是其中一小部分。设置中可切换为「匹配时即拉取」。

**清单不按日期失效**：MB 与 iTunes 来源的已存清单都不因 `fetched_at` 过期或变为 `none`；需要新数据时由用户手动重取，重取跳过响应缓存。

**删除曲目的影响**：只删 `track`，`release` 与 `release_track` 不动——该位置自然回落为灰色行。这正是期望行为：用户删掉一首歌，专辑视图应显示「这张专辑你还缺这首」。

---

## 5. 文本处理与匹配算法

### 5.1 文本标准化规则

`normalize(s)` 统一执行：

```
1. Unicode NFKC 规范化（顺带统一全角/半角）
2. 转小写
3. 罗马数字统一（Ⅲ -> III）
4. 日文假名统一（片假名 -> 平假名）
5. 去除空白与标点
```

**注意**：normalize **不剥离括号内容**。括号处理是独立的、有语义判定的步骤（5.2），不能混进标准化。

### 5.2 括号语义四分类与判定顺序

流媒体平台导出的表格中，括号有四种完全不同的语义，处理方式各不相同：

| 输入片段 | 语义 | 处理 |
|---|---|---|
| `夕立（驟雨）` | 翻译 / 异写 | 生成查询变体，分别打分取最高 |
| `3L（スリーエル）` | 翻译 / 异写 | 同上 |
| `涼宮ハルヒ(CV:平野綾)` | 角色 + 声优 | 拆为两个 artist + `voice_of` 关系 |
| `That Girl (DJLIU Remix)` | 版本标识 | **必须保留**，是曲名的一部分 |
| `Song (feat. Someone)` | 附加艺术家 | 从曲名剥离，并入艺术家 |
| `Artist (US)` | 歧义消解 | 保留为 `disambiguation` |

> **盲目剥离括号会把 `That Girl (DJLIU Remix)` 搜成 `That Girl`，从而匹配到完全不相关的歌。** 判定顺序必须写死：

```
1. 内层匹配 CV 模式 -> 走 5.4 角色/声优拆分
     (CV|cv|ＣＶ|C\.V\.|声|声優|声优|配音|Vo\.)\s*[:：.。]?

2. 内层命中「版本关键词表」-> 保留，不生成变体，不剥离
     Remix|Mix|Edit|Ver\.?|Version|Live|Acoustic|Instrumental|Inst\.?
     |Cover|feat\.?|ft\.?|with|Off ?Vocal|Karaoke|カラオケ|TV ?Size
     |Short|Full|Extended|Radio|Original|Demo|Reprise|-cut|字幕|翻唱

3. 内层命中「翻译提示词」-> 生成变体
     翻译|译名|訳|Translation|中文名|英文名|片假名|平假名|罗马音|Romaji

4. 内外层「主要文字系统不同」-> 生成变体
     文字系统：平假名/片假名 | CJK 汉字 | 拉丁 | 谚文 | 西里尔 | 泰文
     注意：日文汉字与中文汉字同属 CJK 区，需靠"外层是否含假名"辅助判定

5. 都不匹配 -> 保守处理：保留原串，同时生成一个剥离变体。
     两者都查，取分数高者，但剥离变体扣 3 分（惩罚项，
     防止误剥离时压过正确结果）
```

括号形态需覆盖：`（）` `()` `［］` `[]` `【】`；分隔符 `: ： . 。`。

### 5.3 查询变体生成（渐进式）

```
输入：曲名 = "夕立（驟雨）"，艺术家 = "3L（片假名名称）"
   |
   v
变体生成（笛卡尔积，上限 4 个组合，按优先级截断）
   V1: ("夕立（驟雨）", "3L（片假名名称）")   原串
   V2: ("夕立",        "3L")                 双剥离   <- 通常命中这个
   V3: ("驟雨",        "3L")                 取内层
   V4: ("夕立",        "片假名名称")          混合
   |
   v
渐进式执行（v1 采用）：
   先只查 V1。若 V1 产出高置信结果 -> 直接采用，不再查其他变体。
   否则依次查 V2 / V3 / V4。
   |
   v
候选池按 match_ref 去重；每条候选取其在所有已执行变体下的最高分。
分数上记录 matched_variant，复核队列显示「已尝试变体: 「A」·「B」」
```

**为什么渐进式**：MB 限速 1 req/s，若总是查全部 4 个变体，1000 行的导入将从约 17 分钟变为约 67 分钟。渐进式让大多数行只消耗 1 次请求。

### 5.4 CV 解析规则

```
输入串: "涼宮ハルヒ(CV:平野綾)"
   |
   v
[解析器] 仅当内层带 CV 前缀时才拆分
   (避免误伤 "Artist (Remix)" / "Song (feat. X)" / "Band (US)")
   |
   +-> artist A  name="涼宮ハルヒ"  type=character
   +-> artist B  name="平野綾"      type=person
   +-> artist_link(A -> B, voice_of)
   +-> track_credit = "涼宮ハルヒ(CV:平野綾)"        原串保真
   +-> credit_spans_json 记录两段可点击区间
   +-> track_artist: (A, character), (B, vocal)
```

需覆盖的形态：全角/半角括号、分隔符 `: ： . 。`、前缀 `CV / cv / ＣＶ / C.V. / 声 / 声優 / 声优 / 配音 / Vo.`。

**拆分不确定时不拆**，保留原串并标记，进复核队列由用户处理。解析规则应实现为可配置的正则表 + 一组基准测试样例（Golden Tests）。

### 5.5 置信度

**每个上游可以用不同的算法，但满分与「高置信」的判定标准必须一致**，以保证跨上游可排序（复核队列中「本地 + 上游」候选同列时需要）。

**匹配程度判定**（两个算法共用）

```
精确：normalize(a) == normalize(b)
模糊：词元集合包含（一方词元 ⊆ 另一方词元）
   OR 前缀截断（一方是另一方前缀，且长度 >= 4 或 >= 较短串的 70%）
   OR Jaro-Winkler >= 0.90
不匹配：以上均不满足
```

**MusicBrainz 算法**

```
gate: 曲名不匹配 -> 直接丢弃该候选，不计分

曲名匹配                             +55
署名匹配   精确 +20  /  模糊 +12
歌集匹配   精确 +20  /  模糊 +12
上游 score × 0.05                    （上限 +5）
--------------------------------------------
满分 100
```

**iTunes 算法**（iTunes 无 relevance score，用可靠的 `trackTimeMillis` 补足那 5 分）

```
gate: 曲名不匹配 -> 丢弃

曲名匹配                             +55
署名匹配   精确 +20  /  模糊 +12
歌集匹配   精确 +20  /  模糊 +12
时长匹配   |Δ| <= 3s +5  /  <= 10s +2   （输入含时长时才生效）
--------------------------------------------
满分 100
```

**高置信门槛（两个上游相同）**

```
曲名匹配  AND  (署名精确匹配  OR  歌集精确匹配)
```

> **这是结构性条件，不是分数线。** `55 + 12 + 12 = 79` 分但两处都只是模糊匹配 → **不算**高置信，进复核队列。而最低的高置信是 `55 + 20 = 75` 分。分数高不等于高置信。

阈值与权重需在设置中可调，并有明确默认值。

### 5.6 候选合并判定

用**记录**而非猜测判断「库中这首」与「上游这条」是否同一个。按顺序执行：

```
对每条上游候选 C：

[1] SELECT ... WHERE match_source = C.provider AND match_ref = C.ref
      命中 -> 徽章 [本地][MB]，动作变为 [合并]

[2] 未命中，且 C 有 ISRC -> 按 isrc 再查一次（跨上游桥接）
      命中 -> 徽章 [本地][MB]，标注「经 ISRC 匹配」

[3] 仍未命中 -> 用标准化后的文本做模糊比对，但作用域仅限 match_source IS NULL 的曲目
      （即「无需匹配」或手动录入的那些，它们没有标识可比）
      规则：normalize(曲名) 相等 AND normalize(主艺术家) 相等
            AND |时长差| <= 3s
      命中 -> 徽章 [本地?][MB]，问号表示"疑似"，需用户确认

[4] 都未命中 -> 纯 [MB] 候选
```

> 模糊比对只用在 [3]，作用域被压到「没有上游标识的曲目」这个小集合上，比全库模糊去重可靠得多。

### 5.7 Release 优选阶梯

多个高置信候选属于**同一 release group** 时，按此顺序选出唯一一个。**排序必须完全确定，同样输入必须产生同样结果。**

```
1. release_status = Official        （排除 Bootleg / Promotion / Pseudo-Release）
2. medium_format 优先级：
     CD > SACD > Vinyl > Cassette > Digital Media > 其他
3. country 命中「偏好国家列表」（设置项）
4. release-group.first-release-date 最早
5. Cover Art Archive 有图
6. MBID 字典序    <- 纯粹为了确定性，保证结果可复现
```

多个高置信候选**不属于**同一 release group 时，不做自动选择，整条进复核队列。

**同一阶梯也用于选「代表 release」**：专辑视图按 release group 分组时，组内可能有多个 release，需选一个作为清单来源（7.6）。用同一套阶梯，不要另写一套——两处对"哪个版本更有代表性"的判断没有理由不同，而分成两套必然漂移。

---

## 6. 导入流程

### 6.1 文件解析与自动探测

支持 **CSV / TSV / XLSX**。原则：**自动探测优先，只在预览发现问题时才需要手动设置。**

| 项 | 探测方法 |
|---|---|
| 编码 | 先查 BOM（UTF-8 / UTF-16LE / UTF-16BE）→ 无 BOM 则严格校验 UTF-8 → 失败则按候选集 `[GB18030, Shift_JIS, Big5, UTF-16]` 逐个解码，按「CJK 字符占比 + 非法/替换字符数」打分取最高 |
| 分隔符 | 扩展名先猜（`.tsv` → Tab，`.csv` → 逗号）→ 在前 50 行统计 `[, ; \t \|]` 的**每行出现次数方差**，方差最小且计数 > 0 者胜出 |
| 表头 | 首行与后续行的类型分布差异（首行全文本、后续行含数字/日期）→ 或首行命中表头词典 |
| 列映射 | 表头词典匹配（见下），未命中默认「忽略」 |
| XLSX | 无编码/分隔符问题，跳过该部分；多工作表时需选择工作表 |

**列映射词典**

```
曲名     曲名|歌名|标题|题目|title|track|song|name
艺术家   艺术家|歌手|艺人|演唱|artist|singer|performer|vocalist
专辑名   专辑|唱片|碟|歌集|album|collection|release
年份     年份|发行年|年|year|date|released
音轨号   音轨|曲序|序号|track ?no|track ?number|#|no
碟号     碟号|disc|disc ?no|disc ?number
时长     时长|长度|duration|length|time
```

**UI 行为**

```
选文件 -> 自动探测 -> 直接进入「映射 + 预览」页
   |
   +-- 顶部一行浅色状态：
       「已识别: UTF-8 · 逗号分隔 · 含表头   [调整]」
                                            ^^^^^^ 点击才展开手动控件
   |
   +-- 探测置信度低时（编码 < 80%，或未映射到「曲名」）
       自动展开手动面板并高亮
```

预览至少 20 行，并统计将被跳过的行（如缺少曲名）。

**前置条件**：MB 联系方式未配置时，导入入口禁用（见 7.10）。

### 6.2 匹配状态机与队列语义

```
  导入行（曲名 / 艺术家 / 专辑 / 可选年份、音轨号、时长）
      |
      v
  [1] 本地去重检查（5.6）
      |
      +-- 疑似重复 --> 复核队列（库中曲目作为一条候选出现）
      |
      v
  [2] MusicBrainz 搜索（全局 1 req/s 令牌桶；v1 导入阶段只用 MB）
      变体策略见 5.3（渐进式）
      |
      +-- 仅一条高置信 ----------------------> 直接入库
      +-- 多条高置信 & 同一 release group ---> release 优选阶梯（5.7）-> 入库
      +-- 多条高置信 & 跨 release group -----> 复核队列
      +-- 无结果 / 全部低置信 ---------------> 复核队列
      |
      v
  [E] 任一步 429/5xx/超时
      -> 指数退避重排队（尊重 Retry-After）
      -> 累计 5 次失败 -> 标记失败状态，不再自动重排
```

**队列语义要求**

- 持久化到 SQLite，可暂停 / 恢复，应用重启后继续。
- 每个 provider 一个进程级共享令牌桶（搜索 / 导入 / 详情共用；封面图片走独立的工作池，见 3.4）。
- 进度通过 Wails 事件推送给前端，**不使用前端轮询**。

### 6.3 各分支的提示文案

```
[疑似重复]: 输入信息: <输入的曲名/艺术家/专辑>；当前曲目库信息: <库中信息>
            -> 加入复核队列

[直接入库] <曲名> <艺术家> <歌集名> 置信率: XX%
            （仅一条高置信）

[直接入库] <曲名> <艺术家> <歌集名> 实体release 置信率: XX%
            （多条高置信且同属一个 release group，经优选阶梯选出）

[进入复核]: <输入的曲名等输入的信息>
            （多条高置信且跨 release group）-> 加入复核队列

[匹配失败]: <输入的曲名等输入的信息>
            （未找到结果）-> 加入复核队列

[发生错误]: 返回队列，<错误信息>
            -> 返回队列等待再次匹配

[导入完成/暂停]: 本次导入 XX 条，进入复核 XX 条，未匹配 XX 条，失败 XX 条
                 （连续 5 次匹配均发生错误的条目不再返回队列）
                 <换行>
                 <逐行输出所有失败曲目的输入信息>
```

---

## 7. 页面与交互规范

### 7.1 整体外壳与导航

```
+----------------------------------------------------------------------+
| Sonexa       [ 🔍 在 "全部曲目" 中搜索...  ] [▾高级]  [设置]         |
+--------------+-------------------------------------------------------+
| 资料库        |                                                       |
|   最近添加    |                                                       |
|   全部曲目    |                                                       |
|   艺术家      |               <  主内容区  >           +--------------+
|   专辑        |                                        |  详情抽屉     |
|              |                                        |  (点击行弹出) |
| 歌单     [+] |                                        |              |
|   我的收藏    |                                        |   [封面]      |
|   ...        |                                        |   歌名        |
|              |                                        |   艺术家      |
| 标签     [+] |                                        |   ---------- |
|   #ACG       |                                        |   参与者      |
|   #OST       |                                        |   发行信息    |
|   #Vocaloid  |                                        |   厂牌/目录号 |
|              |                                        |   标签 chips  |
| 任务          |                                        |   ---------- |
|   导入 32/120 |  <- 常驻一条，点击展开详细进度与日志      |   [编辑]     |
|   复核 (7)    |                                        |              |
+--------------+----------------------------------------+--------------+
```

侧栏分组：**资料库**（全部曲目 / 最近添加 / 艺术家 / 专辑）、**歌单**、**标签**、**任务**。上游搜索为独立入口。

「艺术家」与「专辑」是浏览入口：点击后中列列出全部艺术家（或专辑）+ 曲目数，选中后主区显示该艺术家（或专辑）的曲目表格。专辑条目的计数是 `已入库/清单总数`（如 `8/12`），并在主区把未入库曲目显示为灰色行——见 7.6。

### 7.2 曲目表格组件（复用规则）

**全应用只有一个曲目表格组件**，在以下位置复用：全部曲目、歌单页、标签筛选、艺术家视图、专辑视图、本地搜索结果。

- 支持多选（Shift / Cmd 点选）
- 单击行 → 打开详情抽屉
- 右键菜单：编辑 / 添加到歌单 ▸ / 添加标签 ▸ / 在艺术家视图中查看 ▸ / 复制信息 / 从当前歌单移除 / 删除
- 工具栏：`[导出]`（当前歌单 / 当前筛选结果 / 选中项 / 全库）

**灰色行（ghost row）变体**——仅专辑视图使用，见 7.6。列定义与普通行完全相同，数据来自 `release_track` 而非 `track`：

```
渲染      文字降低对比度，无封面则占位；不加删除线（它不是"已失效"，是"你还没有"）
选择      不可选中，多选与「全选」跳过
右键      只有 [加入曲库] 与 [查看上游]
单击      不打开详情抽屉（没有详情可展示）
导出      不参与——导出的是收藏，不是愿望清单
计数      不计入「已选 n 项」与列表的曲目数
```

> 组件需要显式支持这个变体，而不是让专辑视图自己做一个分叉。列是一样的，分叉会让两边逐渐漂移。

### 7.3 候选结果表格组件（复用规则）

**上游搜索结果与复核队列候选列表使用同一个组件**，列定义完全一致：

```
来源 | 缩略图 | 标题 | 艺术家 | 歌集名(Primary Type) | 时长
     | 第几曲/共几曲 | 年份 | 载体 | 国家/地区 | 置信度 | 动作
```

- 「来源」为徽章，可叠加（`[本地][MB]`）
- 某上游不支持的字段填 `—`（由 `Capabilities()` 驱动）
- 动作按上下文不同：`[加入曲库]` / `[已在库中]` / `[接受]` / `[合并]`

### 7.4 搜索交互（本地与上游的区分）

**一个表单，两个出口。** 本地结果与上游结果**不混在同一个列表里**——因为它们的动作完全不同（本地 → 打开详情；上游 → 加入曲库）。

```
顶栏:  [ 🔍  在 "全部曲目" 中搜索...              ] [▾ 高级]
                     ^^^^^^^^^^^^
       作用域跟随当前视图（在歌单页则为"在该歌单中"）

展开后:
+------------------------------------------------------------------+
|  曲名   [                     ]                                   |
|  艺术家 [                     ]                                   |
|  专辑   [                     ]                                   |
|  年份   [    ]-[    ]   标签 [#ACG ×] [+]                         |
|  载体   [全部 ▾]  国家 [全部 ▾]  类型 [全部 ▾]   (仅本地搜索有效)   |
|                                                                   |
|  作用域 (o) 当前歌单  ( ) 全部曲目                                  |
|                        [重置]   [搜索本地]   [到上游搜索 →]        |
+------------------------------------------------------------------+
```

- `[搜索本地]`：**不跳页**，在当前曲目表格内筛选；顶部出现 filter chip `筛选: 艺术家="平野綾" ×`
- `[到上游搜索 →]`：跳转上游搜索页，携带已填的三个字段

### 7.5 详情抽屉

宽度 360–420px。纵向顺序：

```
封面
歌名
  艺术家（在歌名下方）
参与者
发行信息
厂牌与目录号
标签 chips
[编辑]
```

**参与者区渲染规则**

```
演唱：优先渲染 track_credit 原串，按 credit_spans_json 把可对应到
      artist 的片段做成可点击链接；对不上的片段保持纯文本，不猜。
其余：按 role 分组，组内按 position 排序，组内用「、」连接，各自可点。
role 顺序：vocal -> instrument -> performer -> composer -> lyricist
          -> arranger -> producer -> remixer -> conductor
character 不单独成组，它只出现在 track_credit 的可点击片段中。
```

```
参与者
------------------------------------------
演唱   涼宮ハルヒ(CV:平野綾)
       └──┬───┘ └──┬──┘
          │        └─> 艺术家视图 [平野綾]        两个视图
          └──────────> 艺术家视图 [涼宮ハルヒ]     都包含这首歌
作曲   田中公平
作词   森由里子
```

**必须优雅处理丰富信息为空的情况**（「无需匹配」入库的曲目）：缺失字段显示 `—`，不报错、不空白。

底部 `[编辑]` 按钮——不要让右键成为编辑的唯一入口。

### 7.6 艺术家视图与专辑视图

艺术家视图 = **按 `artist_id` 过滤的曲目表格**，不是独立页面。

```
+---------------------------------------------------------------+
| < 返回  |  平野綾                                   14 首曲目 |
|         |  (筛选 chip: 艺术家 = 平野綾  ×)                     |
+---------------------------------------------------------------+
|  [全部]  [演唱 12]  [作曲 0]  [作词 2]      <- 按 role 分面    |
+---------------------------------------------------------------+
|  ...标准曲目表格组件（7.2），与歌单页完全相同...                 |
+---------------------------------------------------------------+
```

角色艺术家的视图标题从 `artist_link` 渲染：`涼宮ハルヒ (CV: 平野綾)`。

**专辑视图**与艺术家视图**不同构**——它要显示用户**没有**的曲目。

**分组键的三级降级**

```
1. release.release_group_ref 存在  -> 按 release group 分组（一个专辑的不同版本合为一条）
2. 否则 release.provider_ref       -> 按单个 release 分组（iTunes 无 release group 概念）
3. 两者都无（手动录入 / [无需匹配] 入库，没有上游标识）
                                   -> 派生分组：normalize(专辑名) + COALESCE(album_credit, artist)
```

第 3 级是 v1 之前的唯一做法，现在退为兜底。**这一级没有清单，因此不显示灰色行**，也不显示 `n/m` 计数——没有上游事实就不该假装知道这张专辑有几首。

> 第 1 级不能省。用户的曲目可能匹配到同一专辑的不同版本（一首来自 CD 版，一首来自数字版），按 `provider_ref` 分组会把一张专辑裂成两条。同组内选一个**代表 release** 作为清单来源，选法直接复用 5.7 的优选阶梯——它已经是确定性排序，同样输入必得同样结果。

**专辑列表（中列）**

```
+------------------------------+
|  专辑                        |
|  涼宮ハルヒの詰合        8/12 |   <- 已入库 / 清单总数
|  God knows...            1/1  |
|  そんな私に               3/-  |   <- 无清单（第 3 级分组），不显示总数
+------------------------------+
```

**至少拥有一首才会出现在列表里。** 灰色行是"补全你已开始收藏的专辑"，不是上游目录浏览器。某专辑的曲目被删光后该条目消失（`release` 行保留作缓存）。

**专辑详情**

```
+---------------------------------------------------------------------+
| < 返回 |  涼宮ハルヒの詰合            8/12 首        [仅显示已有]    |
|        |  Album · 2006 · CD · JP                                    |
|        |  清单更新于 2026-09-11  [重新获取]                          |
+---------------------------------------------------------------------+
|  #   标题                        艺术家          时长    状态        |
|  1   冒険でしょでしょ?            平野綾          4:08              |
|  2   God knows...                平野綾          5:32              |
|  3   ゆうつになる                 茅原実里        4:21    其他版本    |
|  4   さようなら                   後藤邑子        3:55   [加入曲库]   |   <- 灰色行
|  5   ハレ晴レユカイ               平野綾          4:02              |
+---------------------------------------------------------------------+
```

- **排序固定为 `disc_number, track_number`**，不是曲目表格的默认排序。专辑就该按曲序读。
- 灰色行显示 `release_track` 里的标题 / 署名 / 时长，动作是 `[加入曲库]`——**已有 track MBID，可直接带完整溯源入库，不需要再搜一次**。
- `[仅显示已有]` 开关可隐藏灰色行，状态记住。
- `tracklist_state != complete` 时不伪装完整：只列已入库曲目，末尾一行 `清单不完整 [补全清单]`。
- `tracklist_state = failed` 时同上，附错误与 `[重试]`。

**灰色行不是曲目。** 它没有 `track.id`，因此：多选跳过它、右键菜单只有 `[加入曲库]` 与 `[查看上游]`、不计入选中计数、不参与导出（7.2 的 `[导出]` 与 F12）。

### 7.7 编辑与重新匹配

**重新匹配上游只能在这里发起。** 其他任何位置都不对已入库曲目自动重新匹配。

```
+------------------------------------------------------------------+
|  编辑 / 重新匹配                                            [X]   |
+------------------------------------------------------------------+
|  匹配来源  [MB · That Girl - Natalia Lesz (2011, CD, PL)]        |
|            [更换]  [清空溯源]                                     |
+------------------------------------------------------------------+
|  基本信息                                                         |
|  字段     当前值               上游值            操作              |
|  曲名     That Girl           That Girl         = 相同            |
|  艺术家   Natalia Lesz        Natalia Lesz      = 相同            |
|  专辑     That Girl           That Girl         = 相同            |
+------------------------------------------------------------------+
|  丰富信息                                                         |
|  年份     2010 [用户]         2011              [填充] [保持]     |
|  音轨号   —                   2                 [x] 填充          |
|  风格     Pop  [用户]         Dance Pop         [填充] [保持]     |
|  载体     —                   CD                [x] 填充          |
|  ...                                                             |
|                    [全部填充(跳过用户锁定)]     [取消]  [保存]     |
+------------------------------------------------------------------+
```

规则：

- 同时展示基本信息与丰富信息两组，用同一套「填充 / 保持」控件
- `source = user` 的字段：**默认不勾选**，显示「用户」徽标，需逐项点 `[填充]`
- 其余字段：**默认勾选**填充
- `[全部填充]` 默认跳过用户锁定项
- 填充后该字段来源改为对应 provider 并清除用户锁
- 用户手动修改字段 → 来源置为 `user` 并加锁
- 匹配溯源可在此手动修改或清空

### 7.8 上游搜索页

```
+------------------------------------------------------------------+
|  上游搜索                                                          |
|  曲名 [That Girl] 艺术家 [Liu] 专辑 [        ]         [搜索]      |
|  上游 [x] MusicBrainz   [x] iTunes  地区[日本 ▾]                   |
|         ^ 从 registry 动态渲染                                     |
|                                     ^ 仅当勾选了 iTunes 时出现      |
+------------------------------------------------------------------+
|  [ 聚合 ]  [ MusicBrainz (12) ]  [ iTunes (载入中...) ]            |
|     ^ 标签页，各上游独立 loading，谁先返回先显示（MB 1 req/s 较慢）   |
+------------------------------------------------------------------+
|  ...候选结果表格组件（7.3），动作为 [加入曲库] / [已在库中]...        |
+------------------------------------------------------------------+
```

三个输入字段可任意留空。结果粒度是 **track**，不是 recording：同一 recording 在多个 release 上时作为多行分别列出。

### 7.9 导入向导

```
+--------------------------------------------------------------+
|  导入  music_list.csv                                    [X]  |
+--------------------------------------------------------------+
|  已识别: UTF-8 · 逗号分隔 · 含表头                   [调整]    |
|  (点击[调整]才展开：编码 / 分隔符 / 首行为表头 / 多值分隔符)     |
+--------------------------------------------------------------+
|  列映射                                                       |
|   Song Name  -> [曲名     ▾]   Artist -> [艺术家   ▾]         |
|   Album      -> [专辑名   ▾]   Year   -> [年份     ▾]         |
|   Note       -> [忽略     ▾]   No.    -> [音轨号   ▾]         |
+--------------------------------------------------------------+
|  预览 (前 20 行 / 共 214 行)          [!] 3 行缺少曲名, 将跳过  |
|  +--------+----------+--------+------+                        |
|  | 曲名   | 艺术家    | 专辑   | 年份 |                        |
|  +--------+----------+--------+------+                        |
+--------------------------------------------------------------+
|                              [取消]   [提交导入 (211 条)]      |
+--------------------------------------------------------------+
```

提交后窗口关闭，条目进入匹配队列并**自动开始匹配**。侧栏「任务」区的导入条显示进度；点击展开详细进度与日志。

### 7.10 复核队列

**单一界面处置全部人工判断**，不按进入原因分成多种界面。`待选 / 匹配中 / 失败` 只是状态标签，界面结构完全一致。

```
+=====================================================================+
| 复核队列                         3 条待处置 · 待选 1 · 失败 2         |
| [< 上一条]  1 / 3  [下一条 >]      (待选)                            |
+=====================================================================+
|                                                                     |
|  That Girl (DJLIU Remix)                                            |
|  Liu · That Girl · —                             来自 import_20260910|
|  导入时的原始文本                                                     |
|                                                                     |
|  [无需匹配]   [删除记录]                                              |
|                                                                     |
+---------------------------------------------------------------------+
|  重新搜索                                                            |
|  曲名 [That Girl (DJLIU Remix)]  艺术家 [Liu]  专辑 [That Girl     ] |
|  上游 [MusicBrainz ▾]                              [重新搜索]        |
|        ^ 单选，默认 MB，从 registry 动态渲染                          |
|                                                                     |
|  (仅当选 iTunes 时出现) 地区 [日本 ▾]  检测到日文，已建议 日本          |
|                                                                     |
|  改搜只影响这次查询，不改动条目的原始导入值。                           |
|  已尝试变体: 「That Girl (DJLIU Remix)」·「That Girl」                |
+---------------------------------------------------------------------+
|  ...候选结果表格组件（7.3）...                                        |
|                                                                     |
|  来源     |缩略| 标题     | 艺术家  |歌集名(类型)|时长|...|置信|      |
|  [本地]   |    | That Girl| Liu    | That Girl |3:41|...| 98 |[合并]|
|  [MB]     |img | that girl| Camille| that girl |2:38|...| 65 |[接受]|
|  [MB]     |img | That Girl| LaGay..| That Girl |5:13|...| 65 |[接受]|
|                                                                     |
|  MB 返回 12 条，已按置信度排序，显示前 10 条    [显示全部] [无一匹配]  |
+---------------------------------------------------------------------+
|  状态说明（仅失败状态显示）                                             |
|  已重试 5 次 · 最后错误: 503 Service Unavailable · 14:32:07          |
|                                              [立即重试] [查看日志]   |
+=====================================================================+
```

**动作语义**

| 动作 | 语义 |
|---|---|
| `[无需匹配]` | 按原始输入入库到基本信息表，丰富信息为空，溯源置 NULL，条目结束 |
| `[删除记录]` | 丢弃该条目，不入库 |
| `[接受]` | 采用该上游候选入库，写入 `match_source` / `match_ref` |
| `[合并]` | 该条目就是库中已有的那首歌：不新建曲目，标记 `merged_into` 后丢弃条目 |
| `[无一匹配]` | 不接受当前结果但保留在队列，可换上游再试（区别于 `[无需匹配]`） |
| `[立即重试]` | 失败状态重新进入匹配 |

**上游为单选下拉，默认 MusicBrainz**；地区下拉仅在选择 iTunes 时出现（选 MB 时它无意义，常显会误导）。

「已尝试变体」提示行必须有——变体机制对用户是黑盒，不显示会让用户困惑为什么搜出来的和输入的不一样。

结果需分页/截断（MB 常返回 25+ 条，全部渲染很吵）。

### 7.11 设置页

```
常规      语言 / 主题 / 数据库位置 / 启动行为

上游      [MusicBrainz]  联系方式（必填；未填则全局禁用出网）
                        自定义服务器地址 / 速率限制（默认 1.0 req/s）
          [iTunes]       默认地区（默认跟随系统 locale）
                        速率限制（默认 20 req/min）
          （上游列表由 registry 渲染，每个上游一个配置分组）
          上游优先级顺序

匹配      高置信阈值（默认见 5.5 的结构性门槛）/ 复核阈值
          release 优选：偏好国家列表 / 载体格式优先级
          最大重试次数（默认 5）/ 退避策略
          查询变体策略（渐进式 / 全量，默认渐进式）
          曲目清单拉取时机（首次打开专辑时 / 匹配时，默认首次打开时，见 4.10）

导入导出  默认编码 / 默认分隔符 / 多值分隔符 / 导出格式
          导出默认 UTF-8 with BOM（便于 Excel 打开）

缓存      封面缓存目录与上限 / 封面并发数（默认 8）/ 上游缓存 TTL / [清除缓存]

关于      版本 / 数据来源署名（MusicBrainz 数据为 CC0，仍建议署名；
          Apple / iTunes 条款要求署名）
```

### 7.12 全局状态

**首次启动引导**：必须先填写 MB 联系方式才能使用出网功能。

**未配置联系方式时的禁用范围：全部出网请求**——导入、上游搜索、封面获取。iTunes 虽不需要联系方式，但混着可用会让用户误以为 MB 也已配好。所有禁用入口给出**统一的原因说明 + 跳转设置的入口**。

**空态**：库为空 / 搜索无结果 / 歌单为空 / 复核队列为空，各有独立文案与引导动作。

**其他**：离线态提示；出网失败的全局提示位置统一。

---

## 8. Wails 与工程约束

1. **匹配队列跑在 Go 侧**，进度通过 `runtime.EventsEmit` 推送前端，**不使用前端轮询**。
2. **前端不自行发出出网请求**，出网一律经绑定方法。否则 User-Agent 与限流无法统一管控。封面图片字节仍由 Go 取，但不经 IPC 桥回传，而是经 `assetserver.Options.Handler` 以本地 URL 供 `<img>` 加载（见 3.4）。
3. 数据库位于 `os.UserConfigDir()` 下的应用目录；封面缓存目录同级但可在设置中更改。
4. **必须有 schema 迁移机制**（顺序编号的迁移脚本 + `schema_version` 表）。丰富信息表被设计为可演进，没有迁移机制这个设计就不成立。
5. 所有涉及外部数据的解析器（编码探测、分隔符探测、括号分类、CV 解析、文本标准化、置信度打分）都应有金样例单元测试。
6. 上游响应先落 `provider_cache` 再解析，便于离线复现问题与调试解析器。

---

## 9. 合规与署名

| 上游 | 约束 |
|---|---|
| MusicBrainz | 数据为 CC0，可自由缓存与存储。必须携带规范 User-Agent。建议在「关于」页署名。 |
| Cover Art Archive | 图片版权各异。本地缓存自用可行；**导出时不打包图片**。 |
| iTunes Search API | 无鉴权。原始响应、清单和已采用图片不设 iTunes 专属的保存天数，用户可手动刷新或清除缓存；本文不对 Apple 条款作合规结论。图片不随导出打包。「关于」页需署名。 |

---

## 10. 演进方向与触发条件

| 演进 | 触发条件 |
|---|---|
| 接入 Apple Music API | 愿意承担 Apple Developer Program 费用。收益：ISRC / UPC / 作曲 / 厂牌，且 ISRC 可作为跨上游桥接锚点，使「storefront 猜错也能自我纠正」成为可能。 |
| 建 `artist_alias` 表 | 需要「同一艺术家不同写法合并」（`平野綾` = `平野 綾` = `Aya Hirano`）。 |
| 建 `artist_credit` 实体表 | 需要跨曲目复用署名，或需要结构化编辑 join phrase。 |
| 扩展 `release` 表承载用户数据 | 需要专辑级的**用户**字段（专辑评分、收藏状态、实体唱片信息）。v1 的 `release` 只镜像上游事实，加这些字段意味着它同时成为用户数据表，需要区分"上游说的"与"用户填的"，并纳入 provenance 机制。 |
| 上游目录浏览 | 需要浏览未入库的整张专辑 / 艺术家全部作品（v1 的灰色行只出现在"已拥有至少一首"的专辑里）。 |
| 艺术家详情页 | 需要展示艺术家简介 / 头像 / 外部链接 / 统计。 |
| 功能 2（本地音频） | 本期的丰富信息字段已按「能写入音频文件内部的元数据标签」的定义设计，届时主要新增：文件扫描、指纹/路径关联、播放器、标签写入。 |
| 功能 3（流媒体同步） | 需要各平台的 OAuth 与歌单读写，Provider 接口需扩展为可写。 |
