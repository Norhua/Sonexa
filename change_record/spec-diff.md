# 实现与当前 spec 差异

本文件由各次修改共同维护；记录**当前代码行为**与 `openspec/` 现行规格的差异，不替代或修改主 spec。后续若对齐规格，应更新或移除相应条目。

## 本次来源

- 修改计划：[`5e5b1865-plan.md`](./5e5b1865-plan.md)
- 起始提交：`5e5b18659db6e1d7ec719584c73dbe69db479828`
- 需求：image18 / image19 的详情抽屉修复及歌词管理。

## D01：无参与角色时不再以署名兜底（明确冲突）

- **现行 spec**：`openspec/specs/platform/app-shell/spec.md` →「详情抽屉 / 缺少详情」要求仍无可显示角色时显示署名和暂无角色提示，按来源追加说明。
- **当前实现**：参与者区域只显示真实角色或已有可靠 CV 角色投影；无角色时仅显示 **“暂无参与角色信息”**，不显示“署名”身份，也不追加来源相关说明。曲名下的歌曲艺术家文字保持不变；不删除数据库中的署名成员或艺术家关联。
- **相关代码**：`frontend/src/components/DetailDrawer.vue`；`internal/app/bindings_library.go` 注释同步。
- **调查结论**：MusicBrainz 目标歌曲「綿雪の夢」的发行详情和独立 recording 接口均返回 `relations: []`；现有请求包含 `artist-rels + recording-level-rels + work-rels + work-level-rels`，未发现该曲角色被解析遗漏，因此未凭署名补造角色，也未改 MusicBrainz 解析语义。完整证据摘要见计划执行记录。

## D02：详情抽屉可调整宽度，分组内容缩进（冲突 / 细化）

- **现行 spec**：`openspec/specs/platform/app-shell/spec.md` →「详情抽屉」固定宽度 340px。
- **当前实现**：默认 340px；拖拽左侧分隔条或聚焦分隔条使用左右方向键、Home / End 调整；下限 280px，上限按视口限制且不超过 900px；宽度偏好保存到 `localStorage`。视口缩小时收敛到可用范围。
- **布局细化**：参与者、发行信息、厂牌与目录号的内容相对标题缩进 12px；保留各段独立的标签/值列宽、长文字换行及封面最大 280px。
- **相关代码**：`DetailDrawer.vue`、`frontend/src/lib/useDrawerWidth.ts`。

## D03：歌词从折叠段落改为独立抽屉页（明确冲突）

- **现行 spec**：`openspec/specs/platform/app-shell/spec.md` →「详情抽屉」「查看歌词」要求在发行信息下方显示默认折叠歌词，无歌词不显示入口。
- **当前实现**：删除该折叠区域；封面下方、曲名右侧始终显示歌词图标。已有正文为绿色，无正文在浅色主题为黑色（深色主题使用浅色以保持可读）。左键切换为歌词页，页头为“歌词 / >”，右箭头返回详情；歌词页不展示其他详情区域和底部编辑按钮。无歌词时显示添加指引。
- **正文显示**：详情歌词页去掉元信息、行时间与增强型 LRC 的逐字时间标签；原始文本仍保存在文件中，搜索预览显示原文时间轴，编辑器既有草稿入口保留。
- **相关代码**：`DetailDrawer.vue`、`frontend/src/lib/lyrics.ts`。

## D04：新增独立歌词操作与双栏搜索（规格缺项）

- **涉及 spec**：`openspec/specs/platform/app-shell/spec.md`、`openspec/specs/editing/track-editing/spec.md`、`openspec/specs/upstream/provider-registry/spec.md`；现行规格只描述编辑草稿中的歌词和独立封面搜索，未定义独立歌词流程。
- **当前实现**：歌词图标右键 / 菜单键 / Shift+F10 提供“从网络搜索歌词 / 从本地选择歌词 / 清除歌词”。本地选择成功及网络点击“应用到歌曲”立即保存当前歌曲，不是编辑器未保存草稿；清除需确认。不更改同专辑其他歌曲、不改变曲目或发行绑定。
- **搜索页**：应用内模态，背景不可交互。歌曲、艺术家为关键词，专辑优先排序（仅填专辑时作为关键词）；来源多选。左栏按来源列出候选、艺术家/专辑、来源、歌曲时长及实际取得后的类型；右栏显示选中歌词原文。未获取前明确“待获取歌词”，不能以歌曲命中冒充歌词命中。支持文本、LRC 时间轴歌词、增强型 LRC。
- **状态**：来源独立查询/失败/重试，选中候选才取歌词；首个候选自动预览。无歌曲、歌词缺失、业务错误、解析失败分别呈现，只有成功预览的候选可采用。关闭、切歌、切换视图会取消会话，迟到结果不覆盖当前目标。
- **保存**：复用 `StageLyrics / SaveLyrics` 的临时文件、哈希校验、事务引用及曲目修订比较；冲突保留原值，提示关闭后重新打开搜索重试。仍采用现行 user/follow 比较规则，未修改数据库结构、歌词 FTS 或导出规则。
- **相关代码**：`internal/app/bindings_lyrics.go`、`frontend/src/stores/lyricsSearch.ts`、`frontend/src/views/LyricsSearchView.vue`、`frontend/src/App.vue`。

## D05：歌词专用上游、缓存和格式边界（扩展 / 缓存例外）

- **涉及 spec**：`openspec/specs/upstream/provider-registry/spec.md` 只列元数据和封面来源；`openspec/specs/platform/data-store/spec.md` →「响应缓存」描述上游成功响应先落盘。
- **当前实现**：增加独立的网易云音乐 / QQ 音乐 / 酷狗音乐 / 酷我音乐歌词来源列表，不扩充元数据注册表或自动导入来源。
- **网络**：全部经 Go 请求，共用传输重试和统一出网门禁；QQ 使用元数据的共享配额，网易云共用 `cover-163`，酷狗/酷我各 1 req/s。请求/下载支持取消、40 秒单次端点流程总超时、4 MiB 响应上限，保留 TLS 校验并限制 HTTPS 重定向域名。WEAPI 加密提取为封面/歌词共享模块。
- **缓存例外**：手动歌词搜索不写原始响应缓存；候选和预览仅保存在受限内存会话，关闭即释放，过期 30 分钟后不可采用。只有用户明确采用的歌词进入既有不可变文件存储。这与通用“先缓存后解析”的表述不同。
- **格式边界**：本地只接受不超过 2 MiB 的 UTF-8 `.txt` / `.lrc`，处理 BOM、CRLF；拒绝无正文、无效编码和 KRC/QRC 二进制。网络处理 QQ JSONP/Base64/实体、网易云按时间合并翻译/罗马音、酷我秒转毫秒并排序去重。酷狗使用已核验的 LRC 下载，不实现 KRC 解密；QQ 不将 QRC 冒充 LRC；增强型 LRC 原文可保存并识别，未提供的逐字时间不生成。
- **接口稳定性**：四来源均为非公开稳定契约；均有成功实测，同时 QQ `2001`、酷我 `301` 已观察到波动，直接报告错误，不绕过认证、限流或证书检查。网易云“纯音乐，请欣赏”占位不视为可用歌词。
- **相关代码**：`internal/lyrics/`、`internal/provider/netease/weapi.go`、`internal/provider/client.go`。
