## Why

Sonexa 要解决的问题是：用户的音乐收藏散落在流媒体导出的表格里，缺少一个**以收藏本身为中心**的管理工具——收藏的存在不应依赖本地是否有音频文件，也不应依赖上游是否匹配成功。现有工具要么绑定本地文件（音频标签编辑器），要么绑定某个平台账号（流媒体客户端），都无法承载「跨平台、跨版本、含角色/声优关系的长期收藏」。

本次是 v1 的首次落地：仓库当前只有 OpenSpec 脚手架与 `prompt/` 下的需求文档，没有任何实现代码。本提案把 `prompt/start-prompt.md` 与 `prompt/features.md` 固化为可验收的规范，并排出实施顺序。

## What Changes

- **新建整个应用**：Wails v2 外壳 + Go 后端 + Vue 3/TypeScript 前端（Tailwind CSS v4、shadcn-vue、Lucide、按需 Pinia）+ SQLite（含 FTS5）。
- **两层曲目模型**：`track`（基本信息，结构稳定，3 个布尔锁）与 `track_metadata`（丰富信息，可演进，逐字段 `provenance_json` + `locked_json`）。丰富信息字段以「未来可写入音频文件内部的元数据标签」为设计目标。
- **关系化的艺术家模型**：`artist` / `artist_link` / `track_artist`，`角色名(CV:艺人名)` 拆为两个艺术家 + 一条 `voice_of` 关系，同时以 `track_credit` 原串保真并附可点击区间。
- **统一的歌单/标签模型**：`collection` + `collection_item`，用 `kind` 区分两种前端呈现。
- **上游 Provider 抽象**：统一接口 + registry + `Capabilities()` 能力声明 + 每 provider 一个进程级共享令牌桶。v1 落地 MusicBrainz 与 iTunes Search API 两个实现（含真实 HTTP 端点、限流、重试退避）。所有「选上游」的 UI 从 registry 动态渲染，禁止硬编码。
- **发行清单镜像**：`release` + `release_track` 存储上游事实（「这张专辑总共有哪些曲目」），使专辑视图能把未入库曲目显示为灰色行并计数 `已入库/总数`。与 `track` 之间不建外键，通过既有上游标识等值 join。
- **匹配管线**：文本标准化 → 括号语义四分类 → 渐进式查询变体 → 置信度评分（各上游算法不同但满分与「高置信」判定统一）→ release 优选阶梯 → 分流（直接入库 / 复核队列 / 退避重排）。队列持久化到 SQLite，可暂停恢复，进度经 Wails 事件推送而非前端轮询。
- **表格导入**：CSV/TSV/XLSX，自动探测编码、分隔符、表头、列映射，预览后提交。
- **复核队列**：单一界面处置全部人工判断，不按进入原因分叉界面。
- **编辑与重新匹配**：唯一的重新匹配入口，逐字段「填充 / 保持」，用户改过的字段默认不被覆盖。
- **封面获取**：三场景 × 尺寸阶梯 × 来源跟随，进程级 8 并发工作池，图片字节经 Wails AssetServer 以本地 URL 供 `<img>` 加载，不经 IPC 桥、不由前端出网。
- **导出**：CSV/JSON，默认 UTF-8 with BOM，不打包封面图片。
- **设置与全局前置条件**：MusicBrainz 联系方式是**全部出网功能**的前置条件，未配置时导入、上游搜索、封面获取统一禁用并给出跳转入口。
- **schema 迁移机制**：顺序编号迁移脚本 + `schema_version` 表。丰富信息表被设计为可演进，没有迁移机制该设计不成立。

本期**不实现**：本地音频扫描/播放/写标签、流媒体账户同步、Apple Music API、艺术家详情页、`artist_alias` 表、`artist_credit` 实体表、音频指纹匹配、批量编辑、编辑历史撤销、智能歌单。

无 **BREAKING** 项——这是首次实现，不存在既有行为。

## Capabilities

### New Capabilities

- `platform/data-store`：SQLite 库位置、完整 schema、`schema_version` 迁移机制、FTS5 索引、`provider_cache` 与 TTL。
- `platform/app-shell`：应用外壳与侧栏导航、可复用的曲目表格组件（含灰色行变体）与候选结果表格组件、详情抽屉、空态与全局出网禁用态。
- `platform/settings`：常规/上游/匹配/导入导出/缓存/关于六组设置项、默认值、首次启动引导。
- `library/track-library`：曲目的创建、读取、更新、删除；基本信息与丰富信息两层；逐字段来源与用户锁。
- `library/collections`：歌单与标签的统一模型、CRUD、曲目归属与排序。
- `library/artists`：艺术家实体、角色↔声优关系、曲目参与者与署名原串渲染。
- `library/release-catalog`：`release` / `release_track` 的写入时机、清单状态机、懒加载与 iTunes TTL 约束。
- `library/search`：库内快速搜索与高级搜索、作用域跟随、筛选 chip、FTS5 命中规则。
- `library/browse-views`：艺术家视图与专辑视图、专辑分组键三级降级、灰色行的呈现与行为约束。
- `library/export`：CSV/JSON 导出、范围选择、字段深度选择、编码与封面约束。
- `upstream/provider-registry`：`Provider` 接口、registry、`Capabilities()` 驱动的 UI 列策略、每 provider 共享令牌桶、可空字段约定、出网前置条件。
- `upstream/musicbrainz`：MB 的搜索/详情端点、`User-Agent` 要求、1 req/s 限流、一次拉满的详情请求、角色关系补充请求。
- `upstream/itunes`：iTunes search/lookup 端点、20 req/min 限流、`country` 取值优先级与文字系统探测建议、TTL 合规约束。
- `upstream/search`：上游搜索页、多选上游与聚合标签页、track 粒度结果、`[加入曲库]` / `[已在库中]`。
- `upstream/cover-art`：三场景尺寸策略、来源跟随、两个上游不同的规格语义、降级链、并发工作池与 AssetServer 传输路径。
- `matching/text-processing`：`normalize()` 规则、括号语义四分类与判定顺序、查询变体生成与渐进式执行、CV 解析。
- `matching/confidence`：两个上游各自的评分算法、统一的满分与「高置信」结构性门槛、release 优选阶梯的确定性排序。
- `matching/provenance`：`match_source` / `match_ref` 的取值方案、赋值与置空时机、唯一约束、候选合并四步判定。
- `matching/pipeline`：导入匹配状态机、持久化队列语义、退避重试与失败状态、进度事件与统计输出文案。
- `import/table-import`：文件解析与编码/分隔符/表头/列映射自动探测、导入向导 UI、预览与跳过统计。
- `review/review-queue`：单一复核界面、条目动作与候选动作语义、改写搜索、已尝试变体提示、失败状态说明。
- `editing/track-editing`：编辑页、重新匹配的唯一入口、逐字段填充/保持、用户锁与来源改写、溯源手动修改。

### Modified Capabilities

无。`openspec/specs/` 当前为空，本次全部为新建能力。

## Impact

- **代码**：全新仓库结构。Go 侧 `internal/`（store / migrations / provider / matching / importer / covers / export）与 Wails 绑定层；前端 `frontend/src/`（views / components / stores / api）。
- **依赖**：Go 侧 Wails v2、`modernc.org/sqlite` 或 `mattn/go-sqlite3`（需 FTS5）、XLSX 解析库、编码转换库、Jaro-Winkler 实现；前端 Vue 3、Tailwind CSS v4、shadcn-vue、Lucide。
- **外部系统**：MusicBrainz WS/2（1 req/s，需规范 User-Agent）、Cover Art Archive、iTunes Search API（约 20 req/min）、`*.mzstatic.com` 图片。
- **合规**：MB 数据 CC0 可长期留存；iTunes 元数据与曲目清单必须设 TTL、不得作为长期事实来源；两者的图片均不随导出打包；「关于」页需署名。
- **本地数据**：数据库位于 `os.UserConfigDir()` 下的应用目录，封面缓存目录同级且可在设置中更改。
