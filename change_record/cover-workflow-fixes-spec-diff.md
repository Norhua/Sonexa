# 封面流程修正：与现行规格的差异与冲突

用途：供后续依据本文件修订 spec。行号基于 2026-09-24 的 `openspec/specs/`（主规格）与 `openspec/changes/revise-album-cover-workflow/`（变更 delta）。
标记：**冲突**＝现有 SHALL 语句与实现相反，必须改写；**差异**＝规格未覆盖或描述过时，需要补充或更新。

## 1. 并发与传输

| 位置 | 现文 | 实现 | 类型 |
|------|------|------|------|
| `specs/upstream/cover-art/spec.md:65`（delta 同名 Requirement「并发与缓存」:55） | 「共用**默认并发 8** 的进程级封面工作池」 | 默认 32，上限 64 | **冲突** |
| `specs/platform/settings/spec.md:35`（delta settings:4） | 「默认容量 2 GiB、**并发 8**、通用有效期 30 天」 | 默认并发 32；设置控件上限 64 | **冲突** |
| `specs/upstream/cover-art/spec.md`（无对应条文） | — | CAA 图片下载先直连 archive.org 最终地址，失败退回 CAA 规范地址；CAA 目录仍经 coverartarchive.org（archive.org 对缺失条目返回 503，不能判定「确认无图」） | 差异（建议补充为实现约束或 design 说明） |
| `specs/upstream/cover-search/spec.md`（CAA 搜索） | 未规定读取顺序 | 固定目标与召回发行（≤10）并发读取目录，结果顺序保持「固定目标在前」 | 差异 |
| 变更 `implementation-resume-checkpoint.md:21` | 「禁止图片传输使用环境代理」 | 未变 | 无冲突（记录保留） |

## 2. QQ / 163 来源

| 位置 | 现文 | 实现 | 类型 |
|------|------|------|------|
| 变更 `design.md:128` | 163「后端直连网易云公开搜索（**专辑 type=10** 或歌曲关联专辑）」 | 只用 `weapi/cloudsearch/pc` **歌曲搜索 type=1** 并按 `al` 聚合专辑；请求经 WEAPI 加密 | **冲突**（需要改写接口描述） |
| 变更 `design.md:133` 引用的 `/api/search/get` 请求形状 | 明文 search/get | 改为 WEAPI；仍无需登录、Cookie、代理 | 差异 |
| 变更 `implementation-checkpoint.md:59` / `known-gaps.md:70`（G14） | QQ 使用 `c.y.qq.com/soso/fcgi-bin/client_search_cp`，现返回 500 | 改用 `POST u.y.qq.com/cgi-bin/musicu.fcg`（`DoSearchForQQMusicDesktop`，search_type=0），已真实验通 | **冲突**：G14 的 QQ 部分应关闭或改写；原生验收部分仍然有效 |
| `specs/upstream/cover-search/spec.md:9` | 「QQ 公开搜索接口可用性……见 G14」 | QQ 已可用 | 差异（删除或更新引用） |
| `specs/upstream/cover-search/spec.md:62–66` 场景「QQ 返回已验证的 800×800」 | 默认 QQ 为 800 | QQ 构造档位 300/500/800/**1200**（1200 可能 404，采用时逐级回退）；实际尺寸可能小于档位（实测 800 档返回 500×500） | 差异（场景本身仍成立，建议补充档位列表和「构造档位≠实际尺寸」的例子） |
| 163 身份 | 以专辑 ID 为来源对象 | 专辑 ID 为 0（用户上传歌曲）时以 `pic-<图片 id>` 作为来源对象 | 差异 |
| 检索词 | 规格未写 | QQ/163：`歌曲名（缺失用专辑名）+ 艺术家`；iTunes 保持原样 | 差异 |

## 3. 搜索结果相关性（新行为）

| 位置 | 现文 | 实现 | 类型 |
|------|------|------|------|
| `specs/upstream/cover-search/spec.md:51`「系统 SHALL 提供可工作的……封面搜索适配」 | 未要求相关性过滤 | iTunes、QQ、163 结果须满足：专辑名匹配；或歌曲名匹配且（若给出）艺术家匹配。比较前标准化并容忍括号版本后缀。仅有艺术家时按艺术家过滤，无条件时不过滤。CAA 不过滤 | 差异（建议新增 Requirement 及场景，例如 image13：查询「世界中の雨 / チームドラゴン from AKB48 / 心の羽根」时，不显示 AKB48 其他单曲） |
| 同上「无结果」状态 | — | 上游返回数据但全部被过滤时，来源显示「无结果」（empty），不是错误 | 差异 |

## 4. 清除封面（新功能）

| 位置 | 现文 | 实现 | 类型 |
|------|------|------|------|
| `specs/upstream/cover-search/spec.md:14`「封面右键入口」 | 菜单子项**只有**「从本地存储中选择」和「从网络搜索」 | 另有顶层「清除封面」（与「更新歌曲图片」以分隔线分开），无所属歌集时禁用，执行前弹出危险确认 | **冲突**（需要改写菜单构成） |
| `specs/platform/app-shell/spec.md:79` | 上下文菜单「通向系统文件导入或应用内封面搜索大弹窗」 | 还包括清除封面 | 差异 |
| `specs/library/release-catalog/spec.md:50` | 「记录当前显式采用的图片」 | 采用关系可以是「已清除」状态（图片为空，revision 递增）；「有封面」判据不计已清除 | 差异（需要定义新状态） |
| `specs/platform/data-store/spec.md:46` | 清除**临时缓存**不删除显式采用文件和采用记录 | 不变；「清除封面」是另一个针对单个歌集的用户操作：删除该歌集目录下全部已保存文件（含采用文件）及其记录，并置空旧版 `cover_path`，旧版原文件保留 | 差异（需要区分两种「清除」） |
| `specs/upstream/cover-art/spec.md:5,14,27`（自动获取条件） | 本地缺图且有 MusicBrainz 发行身份即自动查 CAA | 已清除的歌集**不**自动查 CAA，也不复用临时预览，直接显示 nil，直到用户重新采用 | **冲突**（自动获取需要加上「未被用户清除」的前提） |
| 数据表 | `release_cover_selection.image_id/variant_id NOT NULL`（变更 design 数据模型） | 改为可空，空值表示已清除 | 差异 |
| 事件 | `cover:changed` 仅在采用时发出 | 清除后同样发出，并携带新的 revision | 差异 |

## 5. 封面目录命名

| 位置 | 现文 | 实现 | 类型 |
|------|------|------|------|
| `specs/platform/data-store/spec.md:71` | 「封面文件 SHALL 存放在代表该歌集的稳定目录内，**不按歌曲或可变专辑标题散落**」 | 目录为 `albums/release-<id>-<歌集名>`，名称经清理（保留字符改 `_`、≤80 字符、去掉首尾的点和空格，为空时退回 `release-<id>`） | **冲突**（需要允许目录名包含歌集名，并说明改名语义） |
| 变更 `tasks.md:9`、`design.md:50,211` | `albums/release-<id>/` | 同上 | **冲突** |
| 改名语义 | 规格要求目录「稳定」 | 歌集改名后，新文件进入新目录；已登记文件按数据库相对路径读取，不迁移 | 差异（需要明确「按 id 隔离，目录名可读但不作为身份」） |

## 6. 低/高质量图片规则（用户澄清）

| 位置 | 澄清 | 与现规格/实现的关系 |
|------|------|----------------------|
| `specs/upstream/cover-art/spec.md:25–29` | 缩略图：本地 <600px → 否则看是否已从上游获取 → 否则去上游下载较小规格；已获取且上游无图时用本地最小，本地也无则 nil | 与规格和实现一致。规格写的「上游」只针对 CAA（MusicBrainz 发行身份）；其他来源的封面在导入或搜索时已经下载，因此无需自动访问。建议把这段澄清写进规格的说明性文字 |
| 同上 | 高清：本地 >800px → 否则看是否已从 CAA 获取 → 否则读 CAA 目录并保存全部规格、下载 1200，没有 1200 时下载原图；已获取且 CAA 无图时用本地最大，本地也无则 nil | 与规格（:27、:29）和实现一致，无需修改 |
| 新增前提 | — | 两条规则都要加上「歌集未被用户清除封面」（见第 4 节） |

## 7. 建议同步的其他文档

- `openspec/known-gaps.md` G14：QQ 接口问题已解决（改用 musicu.fcg）；原生验收项保留。可新增一条：图片传输禁用环境代理，需要代理访问 archive.org 的网络无法加速。
- `openspec/changes/revise-album-cover-workflow/acceptance.md`：补充 QQ/163 新接口的真实验证记录（见计划文档第 2 节）。
- `testdata/golden/responses/covers/README.md`：夹具已改为新接口的响应形状，说明文字仍然适用。
