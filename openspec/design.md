# Sonexa 架构设计

行为规格见 [功能索引](README.md)，实现限制见 [已知问题](known-gaps.md)。

## 技术栈与模块

Wails v2 + Go；Vue 3 + TypeScript + Tailwind CSS v4 + Volt + Lucide Icons + Pinia。

| 目录 | 职责 |
| --- | --- |
| `main.go`、`internal/app/desktop.go` | 嵌入前端资源、启动 Wails、配置生命周期和本地封面服务 |
| `internal/app/` | 前端绑定、业务协调、导入与详情补全调度 |
| `internal/store/` | SQLite、迁移、查询、字段来源与锁、全文索引 |
| `internal/provider/` | 上游注册、HTTP 缓存、限流、重试和出网检查；MusicBrainz、iTunes 实现 |
| `internal/matching/` | 文本处理、候选评分、本地重复检查与自动入库判定 |
| `internal/importer/`、`internal/export/` | 表格解析、持久化队列、CSV / JSON 导出 |
| `internal/covers/`、`internal/config/` | 封面用途解析、CAA 目录、下载工作池、专辑图片文件与本地图片服务，`covers/sources/` 为四个封面搜索来源；数据路径与设置 |
| `frontend/src/` | 页面、共享组件、状态管理与统一 API 入口 |

Volt 组件源码位于 `frontend/src/volt/`，基于 `primevue@4.5.5` 的无样式模式，使用本地 Tailwind 样式与 Lucide 图标。颜色统一定义在 `frontend/src/style.css` 的 `--p-*` 变量中。来源与许可见 [组件说明](../frontend/src/volt/README.md)，依赖版本以锁文件为准。

## 数据模型

```text
release_group ──< release ──< track
                     └────< release_track
track ──< track_credit >── artist
track ──< track_artist >── artist
collection ──< collection_item >── track
import_batch ──< import_item
cover_catalog ──< cover_image ──< cover_variant ──< cover_file >── release
release ──  release_cover_selection >── cover_variant
```

- `track` 保存曲名、署名、碟号、音轨号、时长、风格、注释、匹配身份和补全状态。
- `release` 保存共享标题、署名、类型、日期、总数、厂牌、载体和国家。`track.release_id` 可空；年份由发行日期或首次发行日期派生。旧版 `release.cover_path` 只作为迁移来源，由 `cover_legacy_migration` 记录迁移进度。
- `track` 与 `release` 分别保存 `provenance_json`、`locked_json`。来源为上游标识或 `user`；未记录的来源为未知。自动写入的锁保护尚不完整，见 G01。
- `release_track` 保存上游清单位置，不与 `track` 建外键；按同来源、同曲目标识判断是否收藏。
- 本地歌集按去除首尾空白后的标题、署名做大小写不敏感精确匹配。无歌集名时不建本地歌集；无曲目引用的本地歌集可删除，上游歌集和清单保留。
- `track_credit` 保存署名成员、顺序、实际写法及 UTF-8 字节区间；`track_artist` 保存真实角色、顺序、来源与乐器。`release_credit` 有存储接口，尚无业务写入入口。
- `collection.kind` 区分歌单与标签；歌单顺序保存在 `collection_item.position`。
- `import_item` 保存原始行、解析输入、候选、状态、复核原因和失败信息。原始行不随改搜修改。
- 封面按“来源目录 → 图片 → 规格 → 文件”分层：`cover_catalog` 以来源、对象类型和来源标识唯一，记录 unqueried、found、empty、error；`cover_image` 保存上游图片身份与类型；`cover_variant` 保存上游档位、地址、是否构造及实际宽高；`cover_file` 保存受控相对路径、内容哈希、实际尺寸和是否受采用保护，`release_id` 为空表示未采用的预览。`release_cover_selection` 每个歌集至多一条显式采用记录，带递增 revision。

SQLite 使用 `modernc.org/sqlite`，开启 WAL 与外键。迁移按编号逐个事务执行，版本记录在 `schema_version`；当前为 v4（初始结构、封面目录、iTunes 缓存改为不过期、预览来源字段）。旧版本程序遇到更高版本数据库时拒绝打开，回滚需恢复升级前的数据库与封面目录备份。

FTS5 使用 contentless 索引和影子表：影子表保存删除索引所需的原值。索引覆盖曲名、署名、歌集名与关联艺术家；曲目和关系写入时更新。

## 主要流程

1. **检索**：导入、搜索、复核改搜、编辑重匹配共用 `matching.Engine`。查询变体用于召回，原始输入用于评分；具体规则见 [文本处理](specs/matching/text-processing/spec.md)与[评分](specs/matching/confidence/spec.md)。
2. **入库**：候选统一经 `store.AdmitCandidate` 写入，只使用候选已知值。搜索加入和复核接受不等待详情请求；无需匹配按原始曲名、艺术家、专辑入库。灰色行参数与部分字段映射限制见 G02、G06。
3. **补全**：后台按加入顺序处理 `pending` 曲目，向导入和用户检索让出请求机会。共用详情入口在请求前后核对来源、发行、曲目标识，再写字段、成员、角色及清单；整个流程尚非单一事务。
4. **请求**：每个上游共用一个进程级令牌桶，交互请求优先。传输层最多尝试 6 次，条目层默认累计失败 5 次后停止自动重试。原始响应先缓存再解析，同发行响应可复用，但按每首目标重新解析。
5. **封面**：图片地址携带歌集或候选身份及 thumbnail/detail 用途，AssetServer 与批量预取共用同一解析器：先看显式采用，再看本地文件（缩略图取 <600px 最小、高清取 >800px 最大），缺图且有 MusicBrainz 发行身份时复用或获取 CAA 目录并只下载所需档位。目录与规格分别合并请求，CAA 索引、图片、预览和采用共用可调并发的封面工作池，所有出网前检查出网条件。歌集文件位于 `albums/release-<id>/`，按“类型-来源-规格”命名；未收藏候选的预览位于 `cache/sources/`。采用先写临时文件、校验后改名，再提交记录；失败保留旧选择。网络搜索按来源渐进返回，QQ/163 仅作为封面来源，不进入元数据注册表。

## 前端约定

- `TrackTable`、`CandidateTable` 分别统一曲目和候选列表。`useListSelection` 管理选择、焦点与锚点，`trackRows` 管理排序和稳定行标识，避免组件内部再次排序或选择。
- 普通列表与专辑列表分别保存列偏好；专辑固定位置顺序。浏览条件取交集，名单查找与曲目搜索分别处理。
- 详情、设置、导入、编辑使用应用内组件。模态限制尺寸并内部滚动，`dialogStack` 控制 Escape 只关闭顶层。
- 前端通过 `frontend/src/api/` 调用 `frontend/wailsjs/go/app/`；模型命名空间为 `Models.app`。时间跨绑定传递为 RFC3339 字符串。
- `App.emitEvent` 统一发送事件，测试可替换事件发送函数。导入与补全进度由事件更新；封面采用后发送带歌集与 revision 的封面变更事件，列表、详情、专辑和候选按目标与版本刷新。
- 封面搜索大弹窗由 `App.vue` 挂载，会话状态在 `stores/coverSearch.ts`；打开时背景设为 inert，关闭或目标变化即取消会话。

## 构建与测试

Wails 绑定、`dist/`、测试报告为生成产物，不提交。首次检出或修改 Go API 后先生成绑定，命令见[开发说明](../README.md#开发与验证)。

Go 测试与实现同包，离线测试数据位于 `testdata/`。业务回归应经过实际绑定、解析和隔离数据库；仅测试存储函数不足以证明页面功能完成。前端使用 Vitest 及带 Wails 测试替身的 Chromium/WebKit 回归，原生窗口、输入法、系统文件对话框和大曲库性能需要另行验收。
