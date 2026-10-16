## Why

提交 `a169712d`、`a656ca3` 已完成详情抽屉与独立歌词管理，但主规格仍包含固定宽度、折叠歌词及署名兜底等旧规则。将 `change_record/spec-diff.md` 的 D01–D08 与两份计划的最终执行结果同步到主规格后，删除临时记录，避免现行文档互相矛盾。

## What Changes

- 修正详情参与者空态、可调宽抽屉、分组缩进和独立歌词页。
- 补齐独立歌词菜单、本地立即保存、双栏网络搜索、来源状态、候选评分与安全自动预览。
- 定义四个歌词专用来源、出网与配额、无磁盘响应缓存、会话过期和格式边界。
- 定义 QQ 手动歌词的翻译/罗马音、在线 QRC 解码转换及普通歌词回退，保留元数据补全原流程。
- 明确多通道合并、不可变文件采用、修订冲突、草稿与立即保存的边界；不修改数据库、全局匹配或导出。
- 更新文档索引、架构及验证边界，验证后删除 `change_record`。本变更仅同步已实现行为，不新增业务代码。

## Capabilities

### New Capabilities

- `upstream/lyrics-search`: 独立歌词搜索会话、候选准入与排序、四来源协议、通道与格式处理。

### Modified Capabilities

- `platform/app-shell`: 详情空态、调宽、歌词页及独立操作入口。
- `library/artists`: 删除参与者区域的普通署名兜底，保留真实角色和可靠 CV 投影。
- `editing/track-editing`: 编辑器歌词草稿与独立即时保存的边界。
- `platform/data-store`: 手动歌词缓存例外、最终合并文件、事务引用及冲突。
- `upstream/provider-registry`: 歌词专用来源与共享请求、配额和门禁。
- `upstream/qqmusic`: 手动歌词新端点、QRC、翻译与回退，区分自动元数据链路。
- `matching/confidence`: 声明歌词复用默认评分但使用局部准入及高置信规则。
- `matching/text-processing`: 声明歌词单次关键词查询不执行元数据检索阶梯。

## Impact

- 核对实现：`DetailDrawer.vue`、`useDrawerWidth.ts`、`lyricsSearch.ts`、`LyricsSearchView.vue`、`App.vue`、`internal/app/bindings_lyrics.go`、`internal/lyrics/`、共享 provider 客户端及歌词存储。
- 来源记录：起始提交 `5e5b18659db6e1d7ec719584c73dbe69db479828` 和 `a169712d90171b4516be6b44cb4af68d0d93dbef` 的修改计划，以及 `a656ca3` 时的共享差异记录；原文可从 Git 恢复。
- 本次无代码、依赖或数据库变更。非公开接口波动、本地编码与逐字格式限制、既有浏览器测试失败须保留验证边界，不由文档同步推定修复。
