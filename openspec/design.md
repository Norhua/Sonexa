# Sonexa v1 现行设计

本文是 2026-09-19 归档后的实现设计基线，解释现行规格的实现方式。历史 [add-sonexa-v1/design.md](changes/archive/2026-09-19-add-sonexa-v1/design.md) 保留决策过程，不继续代表当前架构；已知未接线能力见 [known-gaps.md](known-gaps.md)。

## 技术栈与组件约定（替代原 proposal、任务 1.2）

- Wails v2 + Go；Vue 3 + TypeScript + Tailwind CSS v4 + Volt + Lucide Icons + 按需 Pinia。
- Volt 源码在 `frontend/src/volt/`，基于固定的 `primevue@4.5.5` unstyled 行为与本地 Tailwind pass-through；来源、哈希与 MIT 许可见组件目录。不是主题包，不迁回 shadcn-vue。
- 通用控件先复用 Volt；缺失的 ContextMenu/TieredMenu 等按同构方式放进该目录。业务导航和可点击署名不必强行包装。
- 唯一调色板在 `frontend/src/style.css` 的 `--p-*`；primary 引用 surface，选中/hover/focus 分离。图标使用 Lucide，不引入 PrimeIcons 字体。业务 PT 与默认主题显式合并。
- 模态表单按需载入；当前主包仍超过 500 kB，不声称已经完成首帧或大曲库性能优化。

## D1/D2/D3：曲目、歌集与来源锁

`track` 为单表，保存 title、credit、位置、时长、风格、注释、上游身份、补全状态和逐字段 JSON 来源/锁。`track_metadata` 仅可能作为旧代码 DTO 名称出现，不再是一张表。

`release` 为共享歌集实体，保存标题、发行署名、类型、日期、碟数/总曲数、厂牌、载体、国家、封面及独立来源/锁。`track.release_id → release.id` 可空；`release.release_group_id` 关联发行组。年份由 release_date/first_release_date 派生，不在曲目重复存储。

来源为 provider id 或 user；候选入库不产生 import，也不借导入输入补缺。来源未记录不能当作已知。旧 title_locked/artist_locked/album_locked 数据列已由 JSON 锁取代，DTO 兼容字段不代表第二套存储模型。后台详情与编辑会话尚未全面遵守歌集/曲目锁，不能据模型设计认定 G01 已完成。

`release_track` 是上游清单位置，与 track **不建外键**，按同来源同 track 标识判断拥有。曲目到歌集的外键与清单位置的身份映射是两件事，不沿用原 D3「release 与 track 均不建外键」的结论。

本地歌集无 provider/ref，按 trim 后标题与署名的大小写不敏感精确匹配归拢，不做标准化异写自动合并；无歌集名则不建实体、不显示未命名派生专辑。本地空歌集删除，上游实体/清单保留。歌集标题不可全局唯一；非空上游标识使用部分唯一索引，ON CONFLICT 条件必须重复其谓词。

## D4：身份与入库统一

MusicBrainz 使用 track MBID，iTunes 使用 country:trackId。`recording_ref` 仅是录音元数据，不再使清单位置变成「其他版本已拥有」。现行清单只有 owned/missing。

导入、搜索加入、复核接受、灰色行加入共用 `store.AdmitCandidate`，但灰色行当前尚未传齐歌集与位置，不能把共用函数等同四路完整数据一致。初次 Year/歌集署名/逐字段来源映射限制见 G06。原始输入仍留在 import_item，用于改搜、恢复与无需匹配；不混入已接受候选。

搜索可能缺真实 track 标识而产生 recording@release 合成候选。该身份不能用于精确详情定位；当前明确失败并保留原记录，不自动映射为某个位置。

## D5：署名成员与实际角色分开

- `track.credit` / `release.credit` 保存原串。
- `track_credit` / `release_credit` 保存成员、position、credited_name、字节区间；成员可查询，区间坐标仅用于显示。position 允许同艺术家重复出现。
- `track_artist` 保存实际角色、顺序、来源、乐器，不再保存 credited_name。
- MusicBrainz `Detail.Credits` 与 `Detail.Artists` 分离，结构化边界优先。文本回退只拆明确连接词，保护括号和结构化乐队名。
- CV 整串是一个完整身份；不派生两人、voice_of 或 vocal，不把其中一方 MBID 赋给复合身份。仅真实演唱关系在同曲目能唯一对应时映射到完整 CV 身份。
- 艺术家名单成员为署名成员 ∪ 实际 vocal；进入某身份后查询署名 ∪ 所有角色。署名分面与演唱分面不同。
- 无实际角色时显示署名及缺失说明，不推断演唱；区间先验证文本，再从 UTF-8 字节转换到 JS 下标。
- 歌集署名成员生产写入、无详情曲目初次成员建立、不确定署名人工处置仍未接线。

## D6/D7：Provider 与出网调度

统一 registry、可空字段、Capabilities 保留。搜索范围、复核下拉、设置分组动态枚举 provider；搜索只选一家，本地和上游不混排。能力声明只控制当前表格已定义列，不恢复已删除的厂牌/目录号列。

每 provider 一个进程共享令牌桶。优先级闸门按 context 传递，默认后台，交互搜索/改搜/重匹配/刷新优先；取得令牌即释放，不握到 HTTP 返回，不抢占已有排队权。

传输层 `Client.Get` 对可重试错误最多尝试 6 次，1 秒基数、指数上限 20 秒、抖动，每次重新取令牌。Retry-After 是下界，不被抖动削短。条目 Runner 另有默认 5 次失败上限，不能把一次传输尝试算作一次条目失败。

CAA/图片服务不消费 MB 元数据配额；自动索引与图片复用封面池。手动封面搜索的索引仍在池之外，统一并发边界仍是 G05。

## D8/D9：封面来源、状态与共享

Go 获取字节 → 磁盘 LRU → Wails AssetServer 本地 URL；不通过绑定传 base64、不让前端直连外部图片。URL 携带 provider/ref/rg 或基准图片地址，已采用图用落盘路径。handler 能独立构造链，预取只暖缓存，不再依赖不可逆缓存键的预注册。

CAA 自动链 Front 优先：发行目标档及后备档 → 发行组 Front → 发行/发行组 JSON 索引，索引里 Front 后其他图，每图尝试 250/500/1200/原图。仅展开一层，在 singleflight 内缓存图片而非 JSON；官方 CAA/archive.org 的 http 图片地址升级 https。手动搜索枚举真实类型/地址/规格，每次至多检查十张去重发行。

iTunes 保留基准图地址与 3000/1200/600/100 构造档，采用前验证并退档。采用图按内容哈希保存到歌集，一次采用全歌集生效。

`CoverArt` 共用无来源、加载、故障、无图和成功状态；无图仅限全部 404/有效空索引，其他错误不吞成无图。图片失败成因在 30ms 窗口攒批 IPC，异步返回校验 URL。lazy 图片保留布局盒，只透明隐藏，不用 display:none 等待成功。

抽屉 CSS 280px 与自动取图默认 250px 是两个独立数值；最高质量、复核预取和视口取消仍是未完成要求。

## D10/D11/D12：查询阶梯与严格分流

查询与判分分离：SearchCore 清截断及非版本注释，原始 Input 判分。最多三级：主干锚定、可用内层、仅曲名；不设原串级、不扣剥离分。MB 锚定为 recording AND (release OR artist OR alternate)，备选至多两个。

三道判断各自负责：

1. 曲名闸：版本词集合必须一致（括号外也检查），再比较主干、异写和相似度；不用词元包含放宽版本。
2. 展示闸：可比的署名/歌集至少一项精确或模糊，或没有可比项。缺失不是反证。
3. 高置信：有曲名匹配且署名或歌集精确。空曲名手动搜索可以返回结果，但无曲名分且不高置信。

默认权重：曲名 55、署名精确/模糊 20/12、歌集 20/12；MB relevance ×0.05 上限 5，iTunes 时差 ≤3 秒得 5、≤10 秒得 2；默认 Jaro-Winkler 0.90。渐进在有可展示候选时停，全量执行全部有效级别。四入口共用 Engine；手动检索忽略自动入库分流结果。

唯一高置信直接入库；同组多条只在非空且非 Digital Media 等非实体集合过滤后恰剩一条时入库；跨组、同组剩零或多条均复核，无跨组例外。代表发行仍按 Official、载体、国家、日期、有图、标识字典序确定，但**不决定多义入库**。

## D13/D15：详情补全、清单与导入

接受/搜索加入先本地写入，不同步补详情。track 保存 pending/fetched/absent/skipped、attempts/error；聚合 matched=fetched+absent，pending 未匹配，skipped 不进分母。后台按入库顺序处理有发行身份且未达 5 次失败的 pending；导入/用户检索/出网禁用时整体让路，手动暂停优先。开始清零失败次数，Stop 有界等待退出。

共用详情入口在出网前读取当前来源、发行、track 标识，返回后再次核对，精确定位后才应用。MB 空目标仅返回清单，非空必须命中；iTunes 核对地区与 track。不同曲目复用发行原始缓存但各自解析目标，不缓存首曲解析结果。

同一次详情顺带保存非空清单，完整标 complete，截断标 partial，总曲数跨碟汇总。仅取清单不再隐式丰富已拥有曲目；写入链路不据此宣称整体原子事务。

import_item 保持原始行不可变，并保存 duplicate/ambiguous/no_match 成因。四类统计互斥，进度以实际入队数为分母；未匹配是业务判定、重试耗尽是发生错误。事件经 `App.emitEvent` 唯一出口，测试用可替换发射器，不向非 Wails 生命周期 context 直接发送运行时事件。

## D14：文件列表与外壳

TrackTable/CandidateTable 是唯一业务表格；DataTable 负责结构与排序事件，关闭内部选择及二次排序。`useListSelection` 独占选择/锚点/焦点，`trackRows` 独占当前顺序与位置 key；自然比较使用 zh-Hans-CN、numeric/base，缺失始终最后。清单位置与曲目 id 分离，批量按曲目去重。

默认单击选中，抽屉已打开才跟随；双击/Enter/菜单打开。文字拖选与行选中共存，Shift 防止整页文本选区，输入等非列表焦点不接管快捷键。无手柄、不整行拖动、不提交歌单重排。

普通与专辑列偏好分别保存，音轨号在封面前、默认仅专辑显示；加入时间默认隐藏，本地 yyyy-mm-dd 显示但排序用完整时间。专辑固定位置顺序，其余表头升/降/默认三态。

侧栏搜索独立、四组滚动、设置底部；标题行 hover/focus 显示新增按钮，无 hover 常显。PaneHeader 48px，输入和按钮 no-drag，移除独立顶栏。视图选项仅提供标签子菜单，名单文本查找不等于曲目 FTS。标签与集合/文本/角色取交集，先确定专辑代表再过滤，完整收藏统计不随过滤冒充结果数。

Dialog 限制尺寸、内部滚动，dialogStack 控制 Escape 只关顶层，Select/Popover 消费后不冒泡关闭父层。禁止 window.prompt/confirm/alert。绑定时间使用 RFC3339 字符串，后端专用时间不导出。

## D16–D20：保留的基础选择与修订边界

- SQLite 使用 modernc.org/sqlite，开启 WAL；迁移逐单元事务与 schema_version 保留。v1 未发布，开发期 0002/0003 已并回 0001，不提供旧开发库兼容。本次不执行用户数据库清理。
- FTS5 contentless 表 + 影子表保留，删除使用原索引值；关系与索引同步。快速搜索 2000 身份上限与无分页限制见 G07。
- 原始 provider 响应先落盘后解析，可离线重放；iTunes 原始响应与清单 TTL 保留，已收藏元数据尚不具备统一生命周期。
- XLSX 用 excelize，编码转换用 x/text，探测使用假名/CJK/罕用字/非法字等领域信号；Jaro-Winkler 本地实现。相关金样例继续维护。
- 模块主线为 internal/app（应用协调、Wails 绑定、后台补全及同包测试）、internal/store、provider、matching、importer、covers、export，以及 frontend 的 components/views/stores/api/volt。根目录 main.go 仅嵌入前端资源并启动 Wails，`internal/app.DesktopOptions` 负责装配同一个 App 的生命周期、封面 handler 与绑定，内部生命周期不导出为前端方法。
- App 的 Go 包名从 main 改为 app 后，Wails 生成接口位于 `frontend/wailsjs/go/app/`，DTO 命名空间为 `Models.app`；前端统一入口和测试桩同步使用 `window.go.app.App`。方法名、参数及数据字段不变；整个 frontend/wailsjs/ 与 md5、dist、测试报告均为忽略产物，首次检出或切换 Go API 后须先用 Wails 重新生成绑定，再构建/测试前端。历史提交中的生成绑定与 md5 缓存亦已清除；手写 bindings_*.go 继续跟踪。
- Go 回归测试与实现同包放置，不把 *_test.go 移入不受 Go 工具扫描的目录；共享金样例继续在根 testdata/。临时手动调试程序放被忽略的 tmp/_scratch/（下划线目录不被 Go 递归发现），不能替代正式回归。详见 [仓库目录说明](../README.md)。

## 验证原则

真实生产绑定入口 + 原始 JSON 回放 + 隔离 SQLite 验证接线，纯 store 单测只能证明原语。前端使用真实应用/组件与 Wails 桩件，通过 Chromium/WebKit 验证真实鼠标键盘、样式与迟到响应。不会把双浏览器自动化等同于原生 Wails 红绿灯、输入法、文件对话框或真实用户库验收。
