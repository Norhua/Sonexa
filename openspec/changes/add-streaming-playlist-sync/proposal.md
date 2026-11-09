# Proposal

## Why

用户需要把 QQ 音乐和 Spotify 的公开歌单持续同步到 Sonexa，而不是反复导出表格、重新匹配整个歌单。现有模型把保存曲目等同于加入音乐库，需要分开“完成导入”和“已入库”，才能保留同步歌单内容而不污染用户的音乐库。

## What Changes

- 新增只读歌单上游能力，首批支持 QQ 音乐和 Spotify 的匿名可读公开歌单；选择平台、输入链接或 ID、读取名称后绑定，可填写本地别名。
- QQ 音乐在现有 `qqmusic.Provider` 实现消费侧歌单接口；Spotify 由专用上游适配器调用 Python `SpotifyScraper`，随 Sonexa 分发独立运行环境，不要求用户安装 Python、pip 或 uv。
- 持久化同步歌单、上游条目及原始曲名/署名艺术家/专辑/毫秒时长、上游存在状态、导入状态和忽略状态。完整拉取后预览增删，确认后原子提交，只为新增或主动恢复的条目建立导入需求。
- **BREAKING**：`track` 存在不再代表已加入音乐库。新增 `in_library` 和独立入库时间，未入库曲目不参与全部曲目、最近添加、艺术家、专辑、资料库搜索或全库导出；完成导入的同步曲目默认只归属同步歌单。
- 导入、复核、无需匹配、重复跳过和活动输入合并共用现有流程。判重继续查询整个 `track` 表，不以 `in_library` 过滤；可靠重复复用已有曲目，跳过不自动入库。多个同步歌单分别维护状态、共享已确认的本地曲目。
- 上游移除的未导入项清理其记录和待处理需求；已导入项保留并置灰。主动移出队列记为忽略，刷新不自动恢复；同步确认弹窗提供默认折叠的已忽略条目，可勾选恢复。
- 侧栏在普通歌单下增加独立“同步歌单”分组与绑定按钮、删除菜单；独立页面只展示已导入项，名称后提供刷新图标，支持可选的绿色勾选“已入库”列及多选加入音乐库确认。
- **BREAKING**：从音乐库删除仍被同步歌单引用的曲目时仅取消入库并清理普通集合关系，不删除同步内容。删除同步歌单清理自身任务/引用，仅回收未入库且不被其他同步歌单引用的曲目。普通歌单和标签仍只接纳已入库曲目，不能通过同步页面绕过入库。
- 不包含登录/OAuth/Cookie 配置、私密歌单、向上游写入、同步歌单本地增删成员、定时同步、音乐播放/下载或历史应用数据迁移。

## Capabilities

### New Capabilities

- `upstream/playlist-read`: 公开歌单链接解析、完整只读获取、统一数据/错误契约及 QQ/Spotify 适配边界。
- `library/synced-playlists`: 绑定、差异确认、增量导入、上游消失/恢复、忽略管理、共享曲目、删除及同步页面。
- `platform/sidecar-runtime`: 随应用交付的 Spotify Python 辅助进程、版本校验、安全通信、取消和无外部环境运行。

### Modified Capabilities

- `import/table-import`: 文件向导仍以音乐库为目标，移除“系统不支持增量同步”的旧限制，区分输入来源与导入目标。
- `matching/pipeline`: 全 track 判重、多目标活动输入合并、目标完成投递、终态和目标感知统计。
- `matching/provenance`: 区分本地精确绑定与音乐库成员，重复跳过不隐式入库。
- `review/review-queue`: 展示全部导入目标，接受/跳过向目标投递，移出同步任务进入忽略列表。
- `library/track-library`: 导入与入库分离、显式入库、独立入库时间和引用感知删除。
- `library/admission`: 准入计划封存导入目标与修订，结果及目标投递原子提交。
- `library/collections`: 普通集合与同步歌单分离，服务端拒绝未入库曲目加入普通集合。
- `library/browse-views`: 名单、计数、分面、专辑分区和可操作收藏均限定已入库曲目。
- `library/release-catalog`: 清单收藏标记和展示代表限定已入库范围，保留同步曲目仍引用的发行。
- `library/search`: 资料库搜索与同步歌单内搜索分别限定范围。
- `library/export`: 全库及普通导出限定已入库曲目，不因存在本地 ID 而导出同步专属内容。
- `platform/app-shell`: 新侧栏分组、导入目标、同步行与专辑灰色占位的语义区分、列偏好和候选动作。
- `platform/data-store`: 曲库归属、同步条目/导入目标关系、索引及引用清理规则。
- `platform/settings`: QQ 配额说明包含歌单读取，来源说明包含 SpotifyScraper 和随附运行环境。
- `upstream/provider-registry`: 新能力登记与注入，Spotify 不进入元数据列表，歌单请求遵守网络门禁和平台配额。

## Impact

- 后端主要涉及 `internal/app/upstreams.go`、导入/复核/准入/曲库/浏览/导出绑定，`internal/importer/runner.go`，`internal/library/`，`internal/store/` 的结构、事务和读模型；新增消费侧 `internal/playlists/` 与 `internal/provider/spotify/`。
- 前端主要涉及 `AppSidebar.vue`、`App.vue`、`ReviewView.vue`、`ImportQueueView.vue`、`TrackTable.vue`、候选表格、列偏好、Pinia 状态及统一 API；新增绑定、同步差异、批量入库弹窗和同步歌单页面。
- 构建增加版本锁定的 SpotifyScraper、Python 运行环境/冻结构建工具及传递依赖、许可证和按平台打包/签名验证。生产不执行运行时安装，不调用用户环境中的 Python。
- 现有 `tmp/demo/` 仅作为可行性及协议风险参考，不复制 CSV 流程、默认测试歌单、Cookie 支持或宽泛回退链。项目当前没有 Python 集成；第三方发布接口在本次规划环境中无法读取，具体版本和传输扩展点须在实现前置验证中锁定，不把 demo 成功当作生产完整性证据。
- 数据模型按项目约定以清空历史应用数据后验收；本变更仅产出规划，不修改主规格、业务代码或执行在线歌单抓取。
