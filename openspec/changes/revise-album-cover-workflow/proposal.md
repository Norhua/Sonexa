## Why

详情抽屉与列表共用 250px 缓存键，无法按展示用途取得高清封面；当前封面重选缺少图片预览、本地导入与更多来源，且无法持久化 CAA 已查询状态及可用规格。此次统一专辑封面存储、获取和重选流程，并移除不符合本地使用需求的 iTunes 保存期限机制。

## What Changes

- 保留专辑（现有 `release` 歌集实体）共享封面：从任一关联歌曲更新后，该歌集全部曲目同步；不改为曲目独立封面，也不扩大发行组的共享范围。
- 自动读取分为两类：曲库列表、搜索结果、复核队列及专辑名单使用缩略图（本地 `<600px`，缺失时优先下载 CAA 250）；详情抽屉与专辑顶部使用高清图（本地 `>800px`，缺失时下载 CAA 1200，无该档则原图）。按实际解码尺寸判断，不靠 CSS 放大或 URL 猜测。
- 先读本地，缺图且已有 MusicBrainz 发行身份时才自动查询 CAA；持久化完整图片规格目录、成功空结果及图片下载状态，已有目录直接复用。未绑定 MusicBrainz 时只看本地，不自动搜索或访问 iTunes、QQ、网易云。
- 无合适档位且不能取得新图时，缩略图使用本地最小图、高清图使用本地最大图；确实无图显示 `nil`，网络失败不得记成永久无图。手动采用的图片继续优先，不被自动补图悄悄替换。
- 图片保存在代表专辑的稳定目录内，名称包含类型、来源和规格，如 `Front-qq-800.jpg`；保存同图多规格、来源、实际尺寸、采用状态及内容校验信息。兼容迁移现有已采用图片，不丢失用户选择。
- 移除详情抽屉“重新获取封面”图标和旧内嵌候选区；封面区域右键显示“更新歌曲图片 → 从本地存储中选择／从网络搜索”。本地选择打开系统文件选择器，将选中文件复制到专辑目录。
- 新增独立的“从网络搜索封面”页面，支持 CAA、iTunes、QQ、网易云（`163`），可改查询条件、筛选来源、预览及选择规格后确认采用；卡片按 `tmp/image/image12.png` 标注来源和规格。来源错误与空结果分别显示。
- **BREAKING**：移除 `itunesTracklistTtlDays`、iTunes provider 的日期失效逻辑与清单到期判定；旧设置兼容读取并忽略该字段，已有 iTunes 响应改为无期限缓存，手动刷新仍可取得新响应。现行文档、关于说明和测试不再要求 iTunes 数据按日期删除或失效；保留来源署名和导出不含图片规则。

## Capabilities

### New Capabilities

- `upstream/cover-search`：专用四来源封面搜索页面、来源/规格预览、系统文件导入与显式确认采用的交互契约。

### Modified Capabilities

- `upstream/cover-art`：用途分级、本地优先、CAA 目录及负结果持久化、无 MB 身份不自动出网、共享获取和封面状态。
- `library/release-catalog`：专辑多图多规格共享及采用状态；删除 iTunes 清单日期失效要求。
- `platform/data-store`：专辑图片目录、规格目录与文件迁移、iTunes 原始响应无期限存储及缓存清理边界。
- `upstream/itunes`：移除响应和清单 30 天期限，保留显式刷新、来源说明及导出边界。
- `platform/settings`：移除 iTunes 清单期限字段及旧配置兼容，更新缓存和来源说明。
- `platform/app-shell`：详情抽屉移除重取图标，改用封面上下文菜单并接入独立搜索页面，保留现有布局与迟到请求保护。
- `upstream/provider-registry`：区分音乐元数据上游与封面专用来源，统一所有 CAA 索引和图片的出网控制及并发约束。

## Impact

- Go：`internal/covers/` 的缓存、采用、工作池与 AssetServer；`internal/provider/musicbrainz/covers.go` 的索引解析；专用 QQ/163 封面适配器；`internal/app/bindings_covers.go`、候选/详情/专辑 DTO、服务装配和设置更新。
- 数据：在 `internal/store/` 新增编号迁移与封面目录/图片记录，替代单一 `release.cover_path` 读取；保留现有发行身份与代表版本规则。缓存根目录变更需保证已采用文件仍可读取。
- iTunes：`internal/provider/itunes/itunes.go`、`internal/store/stale.go`、`internal/store/cache.go`、`internal/config/settings.go`、`internal/app/{app,bindings_albums,bindings_settings}.go` 及相应测试。
- 前端：`CoverArt.vue`、`DetailDrawer.vue`、`AlbumsView.vue`、列表/搜索/复核的封面上下文、统一 API、Pinia 页面会话及新封面搜索页；复用 Volt ContextMenu/Dialog 和主题样式，不增加前端直连上游。
- 文档：实施时同步现行规格、`openspec/design.md`、`openspec/known-gaps.md`、验证说明及 `prompt/start-prompt.md`；历史归档仅保留为历史，不再作为现行要求。删除期限主张不代表已作第三方服务条款合规结论。
- 验证：离线 HTTP fixture、存储迁移/重启/并发测试、真实绑定链路回归、Chromium/WebKit 页面测试和原生文件选择器验收。QQ/163 接口稳定性须专项验证，不引入外部代理服务、账号登录、音频下载或访问限制绕过。

本 change 仅产出规划文档；实现、现行文档清理和主规格同步均等待显式进入 apply。
