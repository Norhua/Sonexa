# 文档与实现核对

## 2026-10-17 上游能力重构验收（进行中）

变更：`refactor-upstream-capabilities`。本节为此次实际执行结果；后面的 2026-10-16 记录是历史证据，不混算为本轮验证。

### 后端全量回归

- `go test -count=1 -race ./internal/...`：全部通过，未使用结果缓存，未开启 live 环境变量。元数据快照、导入、library 准入、封面目录/文件回滚及歌词事务相关包均通过。22 个包中 19 个有测试并通过；`provider/kugou`、`provider/kuwo`、`provider/qqmusic/qrcdes` 没有独立测试文件，相关协议/解码在组合或 QQ 包测试中覆盖，不将其写成独立测试通过。
- `/Users/amadeus/.go/bin/wails generate module`：成功。当前 PATH 未包含该现有安装，使用绝对路径，未安装或替换 Wails。绑定是生成产物，不提交。
- 生成绑定且已有前端 `dist/` 后执行 `go vet ./...`：通过，无输出。
- 生命周期、共享 HTTP 到达配额、非法保存、队列变速、生产依赖边界及在线入口的默认跳过/限额/停止条件定向 race 测试均通过。

### 前端阶段验证

- `npm run typecheck`、QQ 速率 2 项单元测试、`npm run build`：通过。构建仍报告大于 500 kB 的 chunk 提示，未作为本次重构附带解决。
- `SONEXA_E2E_PREVIEW=1 npm run test:e2e -- tests/e2e/provider-capabilities.spec.ts`：Chromium/WebKit 共 4/4 通过，包含空值、负值、越界拒绝及小数、边界、合法旧值和默认值保存。
- 生成绑定后完整执行 `npm test`：15 文件、71/71 通过；`npm run build`：通过，保留大 chunk 提示。
- `SONEXA_E2E_PREVIEW=1 npm run test:e2e -- tests/e2e/lyrics.spec.ts tests/e2e/cover-search.spec.ts tests/e2e/provider-capabilities.spec.ts tests/e2e/editor.spec.ts tests/e2e/shell.spec.ts`：76/76 通过（Chromium 38、WebKit 38）。覆盖封面 14、编辑器 38、歌词 6、来源能力/QQ 设置 4、应用壳 14 项；列表、失败隔离、会话取消、预览采用、地区和编辑保存均按现有断言通过。
- 此命令不含 `screenshot-review.spec.ts`，不能据此关闭历史“用户”徽标断言失败；未修改该测试，也未声称全量 Playwright 通过。生成绑定、构建产物及报告均未提交。

### 在线部分证据与阻塞

命令：`SONEXA_QQ_METADATA_LIVE=1 go test -v ./internal/app -run '^TestLiveQQMetadata$' -count=1`。
使用生产装配、临时存储及 30 请求/分钟的共享桶，最多 12 次请求；本次实际仅发送 3 次。

| 请求 | 发送间隔 | HTTP / 业务状态 | 结果摘要 |
| --- | --- | --- | --- |
| QQ 元数据搜索「晴天 / 周杰伦」 | 首次 | 200；顶层及搜索块 code=0；无 Retry-After | 25 候选 |
| 单碟专辑 `000MkMni19ClKG` / 曲目 `0039MnYb0qxYhV` | 2.001006458 秒 | 200；顶层及 album/songs/song 块均 code=0；无 Retry-After | 1 碟、11 首、9 个角色条目、歌词 2548 字节 |
| 多碟专辑 `0032ezFm3F53yO` / 曲目 `000zwjWq0T51ys` | 1.999332750 秒 | 200；顶层及 album/songs/song 块均 code=0；无 Retry-After | 2 碟、25 首、5 个角色条目、歌词 1795 字节 |

单碟/多碟身份和逐碟连续曲序断言通过，这 3 次请求未发现 HTTP 429 或业务限制。上述三次成功
不能推定任何持续安全额度。日志只有状态、数量和字节数，未转储歌词全文。

测试总体明确失败在缺少 `SONEXA_QQ_FALLBACK_ALBUM/TRACK`：两个固定样例均带内联歌词，
没有真实回退证据。随后用户授权了一次有界样例查找，结果如下；未改写响应、未以 fixture 代替。

#### 已授权的有界样例查找

命令：`SONEXA_QQ_FALLBACK_DISCOVERY=1 go test -count=1 -v ./internal/app -run '^TestLiveQQFallbackDiscovery$'`。
限定 QQ 30 请求/分钟、2 次固定搜索、每次最多 3 个候选详情，总计最多 16 次实际 API 发送
（含分页、回退及重定向）。实际用完 2 次搜索，只取得 4 个可用于详情的有效歌曲/专辑身份，
共 6 次请求；没有额外搜索或扩大范围。

| 搜索词 | 专辑 MID | 歌曲 MID | 详情歌词字节 | 实际回退 |
| --- | --- | --- | --- | --- |
| 綿雪の夢 | `004PnF2J2PtS6M` | `001wMai814HLgJ` | 1947 | 否，内联歌词有效 |
| 綿雪の夢 | `000joZoQ0iwgrK` | `0039iUwd1JqweD` | 2324 | 否，内联歌词有效 |
| 世界中の雨 | `004BBPXb2Nxd4l` | `004ZqEuN3AFkCh` | 1535 | 否，内联歌词有效 |
| 世界中の雨 | `004gEk2012QeyU` | `004LwgWR0hpfzv` | 3087 | 否，内联歌词有效 |

6 次请求全部 HTTP 200、顶层和所用请求块 code=0、无 Retry-After；相邻发送间隔为
2.001031458、2.000035084、1.999494625、2.000080541、2.000387375 秒。
4 份歌词通过通用文本可用性校验，日志只输出身份、字节数和布尔结果，没有歌词正文。
查找测试因未找到成功回退样例而明确失败，不是风控或传输失败；未将“没有触发”记成回退通过。
默认离线调用此新增入口会跳过。

查找未命中后曾按“找到后继续验证”的授权边界暂停。用户随后确认继续独立验证，
QQ 有界速率样本的实际结果如下；回退要求本身未豁免。

#### QQ 有界速率样本：45 档遇业务错误后停止

命令：`SONEXA_QQ_RATE_LIVE=1 go test -v ./internal/app -run '^TestLiveQQRateSamples$' -count=1`。
按 30 → 45 → 60 顺序，每档仅元数据、封面、歌词各一次查询；上游限制立即停止，
本次实际发送 5 次。各业务均查询「晴天 / 周杰伦」，使用生产适配器和同一长期共享桶，
切档通过真实 `SaveSettings` 原位更新。

| 请求 | 设置（次/分钟） | 用途 | 与上次发送间隔 | HTTP / 业务状态 |
| --- | --- | --- | --- | --- |
| 1 | 30 | 元数据搜索 | 首次 | 200；顶层及请求块 code=0 |
| 2 | 30 | 封面搜索 | 2.000546583 秒 | 200；顶层及请求块 code=0 |
| 3 | 30 | 歌词搜索 | 2.000061917 秒 | 200；顶层及请求块 code=0 |
| 4 | 45 | 元数据搜索 | 1.415558042 秒 | 200；顶层及请求块 code=0 |
| 5 | 45 | 封面搜索 | 1.332582541 秒 | 200；顶层 code=0、请求块 code=2001 |

5 次响应均没有 Retry-After，未观察到 HTTP 429。30 档三种查询均返回非空结果；
45 档只有第一次元数据请求成功，第二次封面请求被上游拒绝，不能将该档记为通过。
测试观察器识别业务码 2001 后停止，既没有重试，也没有发送 45 档歌词查询或进入 60 档。
2001 的具体成因未验证，不能仅凭这次少量样本断定为速率限制；同样不能将 45 或 60
视为已验证安全额度。未修改已确认的默认值、范围或生产重试规则。

测试总体失败且任务 8.3 继续未完成。遇到新阻塞后暂停后续在线验证；四歌词正文/通道、
四封面搜索/预览/采用的独立 live 入口本轮仍未执行。速率入口末尾的取消检查也未到达，
不能记为在线通过。G16 的业务错误和缺少真实回退样例分别保留。

### 尚未验证

任务 8.4 的最终逐项证据核对与清理审阅尚未完成。原生窗口、文件选择器、真实断网及大曲库
未新增人工验收。G14/G16/G17 保持未关闭。

---

## 2026-10-16 文档同步记录

日期：2026-10-16。范围：提交 `a169712d`、`a656ca3` 的详情抽屉与歌词管理，以及 `change_record/spec-diff.md` 的 D01–D08、`5e5b1865-plan.md`、`a169712d-plan.md` 两份计划的最终执行结果。以 `a656ca3` 的现有代码为依据，不把早期方案或尚未实现格式写成当前规则。

## 核对结果

主规格从 26 份增至 27 份：修改 8 份既有规格，新增 `upstream/lyrics-search`。本次 delta 覆盖 9 个能力、22 个需求，未删除既有需求或场景；未涉及的需求保持不变。索引、架构、已知问题和本记录同步更新。

| 差异 | 主规格去向与最终规则 | 实现证据 |
| --- | --- | --- |
| D01 | `platform/app-shell`、`library/artists`：无参与角色时只显示空态，保留可靠 CV 投影，不显示署名兜底或来源说明；不删署名和艺术家关联 | `DetailDrawer.vue`、`bindings_library.go`、既有 CV 读模型 |
| D02 | `platform/app-shell`：默认 340px、鼠标/键盘调宽和偏好保存、视口范围约束，分组内容缩进 12px | `useDrawerWidth.ts`、`DetailDrawer.vue` |
| D03 | `platform/app-shell`：常显歌词状态图标，独立歌词页及返回，无歌词引导；详情隐藏时间标签但不改文件 | `DetailDrawer.vue`、`frontend/src/lib/lyrics.ts` |
| D04 | `editing/track-editing`、`upstream/lyrics-search`：菜单、本地导入、确认清除、双栏搜索与独立来源重试；成功预览后明确采用，只改当前歌词，区别于编辑器草稿 | `bindings_lyrics.go`、`LyricsSearchView.vue`、`lyricsSearch.ts`、`App.vue` |
| D05 | `upstream/provider-registry`、`platform/data-store`、`upstream/lyrics-search`：四个歌词专用来源、共享配额/出网/取消、40 秒/4 MiB/2 MiB 边界、无响应磁盘缓存、30 分钟会话失效 | `internal/lyrics/service.go`、`text.go`、`provider/client.go`、共享 WEAPI 模块 |
| D06 | `matching/confidence`、`matching/text-processing`、`upstream/lyrics-search`：默认评分复用、单次关键词、版本过滤、近时长人工确认例外；所有来源完成后才自动预览高置信项，不自动采用或修改元数据全局规则 | `internal/lyrics/search.go`、`search_test.go`、`lyricsSearch.ts` |
| D07 | `upstream/qqmusic`：仅手动歌词批量取普通与逐字块，显式翻译/罗马音，在线 QRC 有界转换与回退；元数据详情/补全链路不变 | `qq.go`、`qrc.go`、`qrcdes/`、`qq_test.go` |
| D08 | `upstream/lyrics-search`、`platform/data-store`：通道缺失/失败分离、无时间轴附加正文保留、按真实时间合并；最终预览文本采用，不落诊断和密文，不新增字段、FTS 或导出 | `text.go`、`qq.go`、`bindings_lyrics.go`、`store/lyrics_files.go`、`internal/app/lyrics_test.go` |

同时收窄“共享候选表格”和 QQ 组合词搜索的表述为元数据流程，避免误用于独立歌词；`library/artists` 中重复的旧署名兜底规则已一并移除。

## 本次实际执行

- `openspec validate --specs --strict`：27/27 通过；部分既有长需求有 INFO 提示，无验证失败。
- `openspec validate sync-lyrics-and-drawer-specs --strict`：通过。
- 主规格与 status 列出的全部 delta 逐项比较：9 文件、22 需求一致；既有需求和场景保留，未涉及需求不变，主规格无 delta 操作标题。
- 主文档链接目标检查及 `git diff --check`：通过。
- `go test ./...`：通过（命中 Go 测试缓存）。
- `go vet ./...`：通过。
- `npm --prefix frontend test`：14 文件、69 项通过。

本次只修改规格、相关文档和清理记录，不修改业务代码；未重跑浏览器、Wails 构建或真实网络接口，未操作真实用户数据或新增原生人工验收。

## 原实现计划的证据与边界

以下为被吸收记录中的历史结果，不是本次重新执行的结果：

- **第一轮（`5e5b1865` 起始，提交 `a169712d`）**：Go 测试、race、vet、前端 64 项单元、生产构建和 Wails 构建通过；Chromium/WebKit 全量 242 项通过。歌词文件选择器使用可注入 picker 与浏览器替身，未人工操作原生窗口。
- **第二轮（`a169712d` 起始，提交 `a656ca3`）**：Go 测试、race、vet、QRC 短时 fuzz、前端 69 项单元及构建、Wails 构建通过；歌词定向 Chromium/WebKit 共 6 项通过。全量 Playwright 为 241/242，失败为 `screenshot-review.spec.ts:107` 的编辑器“用户”徽标旧断言，基线定向复跑也有 2/3 复现。未修改或弱化该测试，基线只做 Vite 构建，不能据此声称旧源码完整类型检查通过。
- **MusicBrainz 角色调查**：目标「綿雪の夢 / ハトリアヤ / 狼と巫女とお菓子の家」的 release `54953b76-c791-4415-8092-1f5487cb8df9` 与 recording `7f4f192d-efb5-4d52-9005-74a435b1fd00` 实测 HTTP 200，目标关系均为 `relations: []`。现有请求包含录音/作品级角色扩展，没有解析遗漏证据，因此不从署名补造角色，也不改 MusicBrainz 语义；有精简回放回归 `TestSnowCreditSurvivesEnrichmentWithoutInventedParticipants`。
- **四来源网络核验**：四来源均有成功实测，也观察过 QQ `2001`、酷我 `301`；不承诺所有歌曲/任意时刻均可用。第二轮 QQ 目标取得 15370 字节增强歌词，含原文、翻译、罗马音；「晴天」原文成功而翻译/罗马音未提供，不能将缺失记为解析成功或接口失败。
- **安全与范围**：酷狗不使用证书主机名不匹配的旧域名，不关闭 TLS；QQ 仅内置有 MIT LICENSE 与来源提交的 DES 块变换，不引入无界解压。网易云 YRC、酷狗 KRC、本地二进制 QRC、播放器/实时高亮、独立歌词导出 UI 未实现。原始网络证据仅本地 `tmp/` 保存，仓库测试使用合成或精简内容，不提交完整第三方歌词。

此前用户确认的原生人工验收属于 `unify-upstream-and-redesign-track-editor`，不覆盖后续歌词功能。旧核对记录可用 `git show 5e5b1865:openspec/verification.md` 查看。剩余问题见 [known-gaps.md](known-gaps.md)，特别是 G16 元数据网络/封面配额与 G17 歌词来源及验证边界；Vite 大于 500 kB 的既有提示不视为已解决。

## 记录追溯

本次正式 delta、设计和任务位于 `openspec/changes/sync-lyrics-and-drawer-specs/`，主规格为当前行为依据。`change_record` 删除后，原差异及计划仍可从 Git 查询：

```sh
git show a656ca3:change_record/spec-diff.md
git show a656ca3:change_record/5e5b1865-plan.md
git show a656ca3:change_record/a169712d-plan.md
```
