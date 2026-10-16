# 实现与当前 spec 差异

本文件由各次修改共同维护；记录**当前代码行为**与 `openspec/` 现行规格的差异，不替代或修改主 spec。后续若对齐规格，应更新或移除相应条目。

## 修改来源

- [`5e5b1865-plan.md`](./5e5b1865-plan.md)：起始提交 `5e5b18659db6e1d7ec719584c73dbe69db479828`；image18 / image19 的详情抽屉修复及歌词管理。
- [`a169712d-plan.md`](./a169712d-plan.md)：起始提交 `a169712d90171b4516be6b44cb4af68d0d93dbef`；歌词搜索相关度、QQ 翻译与 QRC 逐字歌词修复。本次更新 D04 / D05 并新增 D06–D08；其余差异继续保留。

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
- **搜索页**：应用内模态，背景不可交互。歌曲、艺术家为关键词，专辑辅助匹配（仅填专辑时作为关键词）；来源多选。左栏按来源列出候选、艺术家/专辑、来源、歌曲时长、匹配分/确认提示及实际取得后的类型；右栏显示选中歌词的合并正文与通道状态。未获取前明确“待获取歌词”，不能以歌曲命中冒充歌词命中。支持文本、LRC 时间轴歌词、增强型 LRC。
- **状态**：来源独立查询/失败/重试，选中候选才取歌词；所有当前来源结束后，仅在没有人工选择时自动预览排序最高的高置信候选；低置信结果需要手动选择，不再首条返回即预览。无歌曲、歌词缺失、业务错误、解析失败分别呈现，只有成功预览的候选可采用。关闭、切歌、切换视图会取消会话，迟到结果不覆盖当前目标。
- **保存**：复用 `StageLyrics / SaveLyrics` 的临时文件、哈希校验、事务引用及曲目修订比较；冲突保留原值，提示关闭后重新打开搜索重试。仍采用现行 user/follow 比较规则，未修改数据库结构、歌词 FTS 或导出规则。
- **相关代码**：`internal/app/bindings_lyrics.go`、`frontend/src/stores/lyricsSearch.ts`、`frontend/src/views/LyricsSearchView.vue`、`frontend/src/App.vue`。

## D05：歌词专用上游、缓存和格式边界（扩展 / 缓存例外）

- **涉及 spec**：`openspec/specs/upstream/provider-registry/spec.md` 只列元数据和封面来源；`openspec/specs/platform/data-store/spec.md` →「响应缓存」描述上游成功响应先落盘。
- **当前实现**：增加独立的网易云音乐 / QQ 音乐 / 酷狗音乐 / 酷我音乐歌词来源列表，不扩充元数据注册表或自动导入来源。
- **网络**：全部经 Go 请求，共用传输重试和统一出网门禁；QQ 使用元数据的共享配额，网易云共用 `cover-163`，酷狗/酷我各 1 req/s。请求/下载支持取消、40 秒获取流程总超时（包含 QQ 回退或酷狗搜索/下载）、4 MiB 单响应上限，保留 TLS 校验并限制 HTTPS 重定向域名。WEAPI 加密提取为封面/歌词共享模块。
- **缓存例外**：手动歌词搜索不写原始响应缓存；候选和预览仅保存在受限内存会话，关闭即释放，过期 30 分钟后不可采用。只有用户明确采用的歌词进入既有不可变文件存储。这与通用“先缓存后解析”的表述不同。
- **格式边界**：本地只接受不超过 2 MiB 的 UTF-8 `.txt` / `.lrc`，处理 BOM、CRLF；拒绝无正文、无效编码和 KRC/QRC 二进制。网络处理 QQ JSONP/Base64/实体及在线 QRC、网易云按时间合并翻译/罗马音、酷我秒转毫秒并排序去重。QQ QRC 经解码、解压及逐字校验后转为增强型 LRC；酷狗仍仅下载 LRC，不实现 KRC；网易云未接入 YRC。未提供的逐字时间不生成；QRC 解压及文本转换、合并输出保持 2 MiB 边界。
- **接口稳定性**：四来源均为非公开稳定契约；均有成功实测，同时 QQ `2001`、酷我 `301` 已观察到波动，直接报告错误，不绕过认证、限流或证书检查。网易云“纯音乐，请欣赏”占位不视为可用歌词。
- **相关代码**：`internal/lyrics/`、`internal/provider/netease/weapi.go`、`internal/provider/client.go`。

## D06：歌词搜索复用文本评分，但采用独立候选准入与自动预览规则（扩展 / 局部差异）

- **现行 spec**：`openspec/specs/matching/confidence/spec.md` 定义曲名版本检查、可比艺术家/专辑至少一项匹配、高置信及默认权重；`openspec/specs/matching/text-processing/spec.md` 描述统一检索阶梯。独立歌词候选此前无专门规格。
- **当前实现**：四来源沿用单次关键词查询、每来源最多 20 个上游结果，不引入元数据检索阶梯；在本地复用 `matching.DefaultWeights().ScoreCandidate` 及标题/署名/专辑比较，加入目标时长和去重，高置信优先、同级按分数排序。不更改全局匹配引擎或元数据导入规则。
- **歌词专用差异**：曲名和版本不符过滤，并额外区分中文伴奏/纯音乐标记；只填艺术家或专辑时至少要有相应匹配证据。可比艺术家/专辑均不符时，仅“精确曲名 + 时长差 ≤10 秒”可例外保留为人工确认候选（团体/个人署名差异），不作为身份匹配事实。
- **更保守的自动预览**：要求曲名精确且艺术家或专辑精确；已填写的艺术家不匹配/缺失，或已知时长差 >10 秒，均取消高置信。时长差 >10 秒另扣 20 分但不自动丢弃已有标题与旁证匹配的候选。等待来源完成，不由网络速度决定选择，不覆盖用户已选项；自动预览绝不自动采用。
- **相关代码**：`internal/lyrics/search.go`、`service.go`、`frontend/src/stores/lyricsSearch.ts`、`LyricsSearchView.vue`。

## D07：手动 QQ 歌词搜索改用显式翻译与逐字请求（端点及内容扩展）

- **现行 spec**：`openspec/specs/upstream/qqmusic/spec.md` →「请求与限流」「歌词」规定详情歌词优先、空时使用 `fcg_query_lyric_new.fcg`，保存上游原文；未定义 `GetPlayLyricInfo`、翻译/罗马音通道或 QRC 转换。
- **当前实现**：仅手动歌词搜索使用 `music.musichallSong.PlayLyricInfo.GetPlayLyricInfo`；一个 `musicu.fcg` 请求批量取逐字与普通两块，明确 `songMid`、`trans=1`、`roma=1`、`crypt=0`、`qrc=1/0`。普通 Base64 原文和翻译独立解码；QRC/罗马音仍可能为十六进制密文，使用 QQ 变体 DES 与 zlib 解码。
- **逐字转换**：识别 XML `LyricContent` 或逐字正文，按上游绝对毫秒时间生成增强型 LRC 的词起止标签；验证行/词时间、顺序和边界，保留空词间隔与普通署名行，不按字数猜时间。无 QRC 使用同批普通 LRC；失败明确提示，必要时回退旧接口，并保留新接口已经取得的翻译/罗马音。两个端点都失败时不保存。
- **作用边界**：原 `internal/provider/qqmusic` 元数据详情/补全链路不变。本地二进制 QRC 不导入；无播放器或播放同步高亮功能新增。只保证提供可供播放器消费的增强型 LRC 文本。
- **依赖与安全**：仅内置 MIT 许可的 `jixunmoe-go/qrc` 变体 DES 块变换，保留 LICENSE 和来源提交；不用其无界解压，不修改 `go.mod`。所有请求仍走共用配额、出网门禁、TLS 和取消，不绕过账号/限流限制。
- **相关代码**：`internal/lyrics/qq.go`、`qrc.go`、`qrcdes/`。

## D08：歌词通道状态与无同步翻译保留（规格缺项 / 保存内容细化）

- **涉及 spec**：`openspec/specs/platform/app-shell/spec.md`、`openspec/specs/platform/data-store/spec.md` 和 `openspec/specs/upstream/qqmusic/spec.md` 仅定义正文文件和时间标签展示，不定义多通道状态及部分失败。
- **当前实现**：QQ / 网易云预览文档分别保存原文、翻译、罗马音通道（内存 DTO），区分已获取、上游未提供、获取/解析失败；逐字状态另区分未接入。普通获取正常但翻译缺失，不等同于整个接口失败；可选通道失败不直接丢弃可用原文，并显示警告。
- **合并与采用**：按时间戳而非数组下标排序、同刻去重；无时间戳附加文本保留为非同步正文并提示，不静默丢弃，不伪造时间。预览的最终合并 UTF-8 文本才是采用对象，原文/翻译/罗马音及增强标签均随文件保留；不新增数据库字段，也不把诊断状态或原始密文写入数据库。
- **未改变的规则**：仍只改当前歌曲，不改候选对应的标题/艺术家/绑定；沿用修订冲突、文件哈希与事务引用；歌词仍不进入 FTS 或既有表格导出，本次没有新增独立歌词导出 UI。
- **相关代码**：`internal/lyrics/qq.go`、`text.go`、`service.go`、`frontend/src/views/LyricsSearchView.vue`。
