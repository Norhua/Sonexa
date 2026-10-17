# 文档与实现核对

## 2026-10-17 上游能力重构验收（进行中）

变更：`refactor-upstream-capabilities`。本节为此次实际执行结果；后面的 2026-10-16 记录是历史证据，不混算为本轮验证。

### 后端全量回归

- `go test -count=1 -race ./internal/...`：全部通过，未使用结果缓存，未开启 live 环境变量。元数据快照、导入、library 准入、封面目录/文件回滚及歌词事务相关包均通过。按 8.4 再次执行 `go list ./internal/...` 校正计数：共 21 个包、18 个有测试并通过；`provider/kugou`、`provider/kuwo`、`provider/qqmusic/qrcdes` 没有独立测试文件，相关协议/解码在组合或 QQ 包测试中覆盖，不将其写成独立测试通过。
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

### 再次授权的回退样例查找（仍未命中）

用户要求找到真实回退样例并继续完成 8.3。本轮先检查已有本地响应和公开资料：旧
`qq-detail-yqq.json` 虽然没有详情 info，但使用了不同请求参数，不能替代当前生产路径的
回退证据；未用该参数修改生产请求。[NeriPlayer 的歌词修复记录](https://github.com/cwuom/NeriPlayer/commit/7f684d1194caea557168490ef95f723c378320a6)
提供「夜に駆ける」的旧歌词接口成功线索，[公开歌曲页分析](https://qihe-no-study.github.io/ai_reverse_study/qq_music/%E9%80%86%E5%90%91%E6%96%87%E6%A1%A3.html)
提供「爱你（青春离别版）」的歌曲身份；两者都不证明详情缺少内联歌词，仅用于选定查找标题。

为避免反复硬编码标题，现有 `TestLiveQQFallbackDiscovery` 增加
`SONEXA_QQ_DISCOVERY_TITLES`：换行分隔、去空/去重、最多两个标题，默认标题不变。
仍只检查各搜索的前三个候选，使用生产装配和 30 请求/分钟的桶，总发送硬上限 16、
90 秒上下文，保留 HTTP/业务错误与传输失败停止规则，不改变回退行为或成功断言。

实际命令（Bash）：

```sh
SONEXA_QQ_FALLBACK_DISCOVERY=1 \
  SONEXA_QQ_DISCOVERY_TITLES=$'夜に駆ける\n爱你 (青春离别版)' \
  go test -count=1 -v ./internal/app -run '^TestLiveQQFallbackDiscovery$'
```

两次搜索各返回 3 个候选，其中 5 个具有可用的歌曲/专辑身份，共发送 7 次请求：

| 标题 | 专辑 MID | 歌曲 MID | 详情歌词字节 | 实际回退 |
| --- | --- | --- | --- | --- |
| 夜に駆ける | `002k2G8Z0j3wcO` | `003WFMXk4O5ywc` | 3011 | 否 |
| 夜に駆ける | `002c8UX62IYpOP` | `003fE7F72h8tND` | 3096 | 否 |
| 夜に駆ける | `000Dy9HS1TLM8S` | `000I42sc1Lrn0d` | 3094 | 否 |
| 爱你 (青春离别版) | `0035DC6W4ZpSqf` | `0004BoFH1kpYFC` | 2402 | 否 |
| 爱你 (青春离别版) | `002QeESA2WKPUX` | `003gFKV72HQHmo` | 397 | 否 |

全部 HTTP 200、顶层和所用请求块 code=0，无 HTTP 429、无 Retry-After。六个相邻发送间隔
依次为 2.001100250、1.999864625、2.000134583、1.999999000、2.000028250、1.999565167 秒。
五份详情均有可用内联歌词，因此没有请求旧歌词端点。测试在 7 次请求后明确失败并停止，
未扩大查找范围，未改写响应，未记录歌词正文。原始摘要日志只保存在本地
`tmp/upstream-capabilities-live/qq-fallback-discovery.log`，不提交。

本轮没有发现风控，但也未取得真实回退证据。**8.3 仍未完成**，四歌词/四封面及速率入口
本轮未运行，先前 45 档 2001 的记录和未验证边界不变；不能把本次 30 档详情成功当作
45/60 档验通或原生验收。查找辅助代码的默认/空白、去重、单/双标题与超额拒绝测试，以及
默认跳过、观察器停止、速率停止的定向 race 测试通过。关闭所有 live/probe 开关后执行
`go test -count=1 -race ./internal/app/...` 通过（49.192 秒）；`go vet ./internal/app/...`、
`openspec validate refactor-upstream-capabilities --strict` 与 `git diff --check` 均通过。
本轮未改生产代码、DTO 或前端，也未重跑全量后端/浏览器验收。

### 扩大有界查找及独立来源复测（最新结果）

用户明确授权一次最多 10 个标题、40 次实际请求的查找，并允许不等待回退样例，独立推进
其他在线验收。测试新增显式 `SONEXA_QQ_DISCOVERY_EXPANDED=1`，默认仍为两标题/16 次；
扩大模式限 3 分钟、每标题最多三个详情，全部 QQ 请求仍走生产装配与 30 请求/分钟桶。
`TestLiveQQMetadata` 的成功判定补齐与查找入口相同的通用歌词可用性校验，防止把非空
占位内容当作成功回退；不修改生产协议、响应或元数据存储行为。

#### QQ：真实触发回退，但没有取得可用歌词，随后 -1901 停止

实际命令（Bash）：

```sh
SONEXA_QQ_FALLBACK_DISCOVERY=1 SONEXA_QQ_DISCOVERY_EXPANDED=1 \
  SONEXA_QQ_DISCOVERY_TITLES=$'An Ending (Ascent)\nAvril 14th\nOn the Nature of Daylight\nEverybody’s Free (To Wear Sunscreen)\nBohemian Rhapsody (Live)\nBaba Yetu\nSogno di Volare\n小さな手のひら\n初音ミクの消失\n夜空中最亮的星 (Live)' \
  go test -count=1 -v ./internal/app -run '^TestLiveQQFallbackDiscovery$'
```

只执行了第一个标题 `An Ending (Ascent)`，其搜索返回 3 个候选，随后在第二个候选的
旧歌词请求遇到业务错误停止，没有执行其余九个标题、重试或另开 QQ 入口：

| 请求 | 路径/用途 | 间隔 | HTTP / 业务状态 | 结果 |
| --- | --- | --- | --- | --- |
| 1 | musicu 搜索 | 首次 | 200；顶层/搜索块 0 | 3 候选 |
| 2 | musicu 第一候选详情 | 2.000400417 秒 | 200；顶层及三块 0 | 详情缺少内联歌词，自动进入回退 |
| 3 | 旧歌词接口 | 2.000662750 秒 | 200；code/retcode/subcode 均 0 | 61 字节，但通用歌词可用性校验失败 |
| 4 | musicu 第二候选详情 | 1.999952333 秒 | 200；顶层及三块 0 | 随后继续清单分页 |
| 5 | musicu 清单分页 | 2.000449375 秒 | 200；顶层/请求块 0 | 随后自动进入回退 |
| 6 | 旧歌词接口 | 1.999561542 秒 | 200；code/retcode/subcode 均 -1901 | 观察器触发停止 |

第一候选的专辑 MID 为 `001Frvoh4E456m`，歌曲 MID 为 `000W46Kt3BWQ0i`。
它是**真实触发回退但未取得可用歌词**的样例，不能用于宣告“成功补取歌词”通过。
第二候选由于观察器提前停止，没有输出身份摘要，不猜测其 MID。
六次响应均无 Retry-After、无 HTTP 429；`-1901` 的具体原因未验证，不将它直接等同于
速率风控或据此绕过限制。其后本轮未运行 QQ 单碟/多碟固定入口、手动歌词、封面或速率入口。

#### 不访问 QQ 的独立验收

沿用现有 live 入口及其硬预算，用 Go 子测试过滤只选择尚未运行的其他平台；每个来源
只尝试一次。组合测试某来源失败后返回，因此后续命令仅执行之前未到达的来源，不重试
失败来源。歌词使用默认「青花瓷 / 周杰伦」，封面使用现有固定样例和地区。

```sh
SONEXA_LYRICS_LIVE=1 go test -count=1 -v ./internal/app -run '^TestLiveSources$/(163|kugou|kuwo)$'
SONEXA_LYRICS_LIVE=1 go test -count=1 -v ./internal/app -run '^TestLiveSources$/(kugou|kuwo)$'
SONEXA_LIVE_COVERS=1 go test -count=1 -v ./internal/app -run '^TestLiveCoverSources$/(caa|itunes|163)$'
SONEXA_LIVE_COVERS=1 go test -count=1 -v ./internal/app -run '^TestLiveCoverSources$/(itunes|163)$'
SONEXA_LIVE_COVERS=1 go test -count=1 -v ./internal/app -run '^TestLiveCoverSources$/163$'
```

实际执行时显式清空了歌词 SOURCE/TITLE/ARTIST/ALBUM/DURATION_MS 和封面 SOURCE 的覆盖变量。
上面分别为 5 次命令，不是 5 次全量复测；子测试选择没有访问 QQ。

| 来源/业务 | 请求与结果 | 验收结论 |
| --- | --- | --- |
| 网易云歌词 | 搜索/正文 2 次 HTTP 200、code=200，间隔 1.000600250 秒；仅留下候选 `3381543730`，时长 95037 ms，正文不可用 | 失败，未改查其他歌或把“HTTP 成功”写成取得歌词 |
| 酷狗歌词 | 搜索/歌词候选/下载共 3 次 HTTP 200；状态分别为 status=1/errcode=0、status=200/errcode=200、status=200/error_code=0；间隔 1.000562208、1.000444667 秒 | 通过，候选 `37a8f50a9ec3b267c3cc6bec633d9c4a`，239000 ms，1961 字节 LRC；无警告；原文/翻译/罗马音摘要均为 unknown，不能记作三个独立通道已验通 |
| 酷我歌词 | 搜索 HTTP 200，留下 2 候选，首个 `482253701`；正文在 1.000644500 秒后返回 HTTP 200/status=301 | 失败，观察器停止；没有尝试第二候选或再次请求酷我 |
| CAA 封面 | MB 召回首次请求发生 `http.tlsHandshakeTimeoutError`，未取得 HTTP 响应 | 失败，未进入目录/图片，不关闭 TLS、不改代理或重试 |
| iTunes 封面 | 搜索 1 次 HTTP 200，返回 33587 字节；没有满足成功且非空卡片断言 | 失败；旧测试只记录结果条数，无法从日志区分空结果/来源处理错误，根因未确定，不据此修改适配器或相关性规则 |
| 网易云封面 | 搜索 1 次 HTTP 200/code=200，图片/目录门禁许可 1 次，真实 handler 预览后显式采用 | 通过，请求档位 250、实际 240×240、16545 字节、adopted_revision=1；不把构造尺寸当实测尺寸 |

酷狗到酷我首请求相隔 106.442167 ms，是不同平台桶，不与同平台约 1 秒的间隔混算。
取得 HTTP 响应的 API 请求均未返回 Retry-After 或 HTTP 429；没有原始响应/歌词正文转储。
全部在线测试使用临时库/目录，不触碰真实收藏；原始摘要日志在本地
`tmp/upstream-capabilities-live/` 的 `qq-fallback-expanded.log`、`lyrics-non-qq.log`、
`lyrics-kugou-kuwo.log`、`covers-non-qq.log`、`covers-itunes-163.log`、`covers-163.log`，不提交。

**8.3 仍未完成**：已有真实回退触发证据但无成功正文；QQ -1901、酷我 301、MB TLS 超时、
网易云当前歌词样例和 iTunes 图片结果未通过。QQ 45/60、速率入口取消断言及四来源全体
最新验收均未完成；本轮不再发在线请求，不下调验收条件、不关闭 G14/G16/G17。

离线验证：默认/扩大模式的显式开关、标题上限、请求预算和现有停止规则定向 race 通过；
关闭所有 live/probe 开关后，`go test -count=1 -race ./internal/app/...` 通过（49.092 秒），
`go vet ./internal/app/...`、`openspec validate refactor-upstream-capabilities --strict` 及
`git diff --check` 通过。本轮只改测试辅助与证据文档，未改生产代码/公开绑定/前端，
未重跑全量后端、浏览器或原生验收。

### 8.4 证据核对与清理审阅（已完成；8.3 仍未完成）

用户明确选择先完成本项，保留 8.3。审阅基于重构前提交 `6814557`、此前实现至
`70d245d` 以及本次清理；范围为该 change 的四份 delta、design 和
[原始提示词](changes/refactor-upstream-capabilities/resource/0-original-prompt.txt)。本项完成表示
实现/离线证据和未验证边界已核对，**不表示整个变更验收完成或可以略过在线阻塞归档**。

#### 原始三个问题与实现结论

| 原始问题 | 结论与源码证据 |
| --- | --- |
| 是否采用业务声明接口、上游按能力实现并注入？ | 是。`lyrics/provider.go` 的 `Provider` 仅搜索/获取歌词，`covers/sources/provider.go` 的 `Provider` 仅搜索封面；均嵌入只含 ID/显示名的 `provider.Upstream`。`app/upstreams.go` 显式装配具体实现，业务服务与歌词/封面搜索绑定不解释平台协议。 |
| 原 Provider 的作用是什么？ | 元数据身份/字段能力、查询候选、快照、精确补全、发行摘要/详情及配置/速率契约；实际消费者仍为 `provider.Registry`、`library/service.go`、`importer/runner.go` 与 `app/bindings_upstream.go`，不能把这些成熟流程删掉或改成歌词/封面 DTO。 |
| 原 Provider 保留还是取消？ | 保留上述元数据职责并正名为 `MetadataProvider`，删除强制 `SearchCovers` 和旧接口名称，不留类型别名兼容壳。`VersionLister`、`ArtworkStrategist` 仍为可选契约；各上游具体结构体仍可叫 `Provider`，这不是旧父包大接口。 |

单能力不只是接口声明：`TestSingleLyricsProviderThroughRealBindings` 验证仅歌词假来源的列表、
搜索、候选归属、预览、采用、修订冲突及过期；`TestCoverOnlyProviderExistingCatalogNotSavedAgain`
验证仅封面假来源用自己的图片主机完成搜索、真实本地 HTTP handler 预览和采用，且不进入元数据注册表。
两者都是离线适配器/临时库集成测试，不是新增真实来源的网络可用性证明。

#### 四份 delta 逐项核对

以下路径在 `internal/` 下（前端路径除外）；“通过”均指源码及离线/既有浏览器证据，不替代 8.3。

| Delta / Requirement | 场景、实现与测试证据 | 结论与边界 |
| --- | --- | --- |
| `platform/settings` / 上游设置 | `app/bindings_settings.go:ListProviders/SaveSettings/CheckOutbound`；`app/upstreams.go` 登记三个元数据来源并按设置排序；`SettingsView.vue` 保留 QQ 设置字段，说明合计三类 API、不含图片。`TestProductionAssemblyListsAndFailedBuildDoesNotPublish`、`TestCoverOutboundGateAcrossBindingsAndAssets` 与 `provider-capabilities.spec.ts` 覆盖列表、地区和设置生效。 | 单歌词/封面来源不进入元数据选项；联系方式门禁统一。QQ 非公开接口风险仍在 G16。 |
| `platform/settings` / QQ 合计请求速率设置 | `provider/quota.go:QQRatePerMinute` 是后端唯一速率描述，config 和 QQ 构造共用；`config/settings.go:Validate/NormalizeStartup`、`SaveSettings` 在保存/移动/装配发布前校验。`config/qq_rate_test.go` 覆盖缺失、30、边界、小数、越界、零/负数、非有限值与损坏 JSON；`app/qq_settings_test.go` 覆盖磁盘/目录/配额无副作用、有效值到生产桶及排队保存。前端 `qqRate.ts`、单测与浏览器断言使用相同边界。 | 默认 45、范围 1–60、burst=1；合法旧值不提高、保存不重置预约。它是工程保护范围，45/60 不是在线验通额度。 |
| `upstream/cover-search` / 四来源结果与失败隔离 | `sources.Registry` 与 `StartCoverSearch/searchCoverSource` 统一列表、校验、按来源并行执行和 Result 登记。`cover_search_test.go` 覆盖部分成功、全部错误与合法空结果区分、单源重试授权和迟到取消；`cover_sources_test.go`、`provider/caa/search_test.go` 覆盖绑定发行优先、最多 10 个、部分目录失败与主动刷新。`cover_quota_test.go` 证明 QQ 与元数据/歌词共享、其他平台不被占用。 | 四适配器及离线失败隔离通过；四封面当前网络搜索/预览/采用仍未复测，不据此关闭 G14。 |
| `upstream/cover-search` / 来源声明的图片 URL 安全范围 | 四个适配包 `image_policy.go` 声明，`app/upstreams.go` 校验并注入；`provider/image_policy.go` 与 `covers/image_policy.go/pool.go/transport.go` 检查首次、自动降级及逐跳请求。`TestPoolSourcePolicyChecksFirstRequestAndEveryRedirect`、`TestPoolPolicyRedirectGateCancelAndLimit` 覆盖跨来源、伪装子域、HTTP、用户信息、非允许端口、未登记策略、动态门禁/取消/跳数；`cover_capability_test.go` 与 `upstream_lifecycle_test.go` 覆盖新主机和旧会话快照。 | 不使用全部来源许可并集；本地命中不新增门禁；正常 TLS 和 DNS 后私网拒绝保持，图片无环境代理。 |
| `upstream/provider-registry` / 接口与注册表 | `provider/provider.go/registry.go`、`lyrics/registry.go`、`covers/sources/registry.go`、`app/upstreams.go`；集合单测拒绝重复/未知能力，元数据同 ID 覆盖仍有 `TestMetadataRegistryStillReplacesDuplicateID`。生产装配列表测试固定三元数据、四歌词、四封面的顺序及名称；`app/app.go:rebuildRunner` 明确只把 MB 交给导入。 | 保留元数据字段能力和导入仅 MB；两个真实绑定单能力测试证明无需伪造其他业务方法。 |
| `upstream/provider-registry` / 后端请求与共享限流 | `provider/client.go/limiter.go/quota.go`；`TestQQThreeCapabilitiesShareHTTPQuotaIncludingRetryAndFallback` 记账三搜索、正文、回退及重试到达时间；`TestPlatformQuotasIndependentAndNetEaseCapabilitiesShared`、`TestCoverSearchSharesMetadataQuotaButCAADirectoryDoesNot`、`TestCoverPreviewAdoptionAndAutomaticImagesSharePool` 覆盖平台独立、163 共桶和图片/CAA 例外。`cache_mode_test.go`、`app/lyrics_cache_test.go` 覆盖 NoStore 零读写与刷新写回；门禁、重试、取消和关闭由对应 provider/covers/App 回归覆盖。 | 生产无 `cover-qq`、`cover-163`、`lyrics-*` 配额键；构造不配置桶，新实例无额外令牌。所有统计为本地 HTTP 替身，不推定在线额度。 |
| `upstream/provider-registry` / 统一封面行为 | `provider/artwork.go`、各适配器 `ArtworkStrategy` 和 `covers/resolver.go` 保留真实图片来源；MB 薄委托 CAA。`TestLibraryImagesReuseAfterCandidateAdmission`、`cover_production_test.go`、`cover_adoption_edges_test.go` 覆盖预览入库复用、采用隔离与失败保护。CAA 既有 Result 只读，`TestCoverOnlyProviderExistingCatalogNotSavedAgain` 断言目录版本和 ActualReleaseURL 不被覆盖。 | 不创建虚假发行，不因接线增加 API 图片探测；目录、文件与工作池保持原语义。 |
| `upstream/provider-registry` / 业务能力调用契约 | 搜索绑定仅传 Query、消费 Candidate/Payload/Result；QQ/163 原语分别投影业务结果，酷狗 hash/accesskey、酷我时间、QQ QRC 在适配包。`TestUpstreamCapabilityDependencyBoundaries` 扫描生产 AST（包括当前平台未编译文件），检查反向依赖、平台 URL/加密解析及来源分支；检查器本身有拒绝回归用例。 | 消费侧无需新增平台参数或解析；错误不伪装空结果。AST 检查与下述调用检索结合，不把单一正则当作整体设计证明。 |
| `upstream/qqmusic` / 请求与限流 | `qqmusic/search.go` 唯一搜索构造调用 `musicu`，`parse.go:decodeEnvelope/decodeBlock` 共享状态解析；旧歌词只经 `legacy_lyrics.go`。`search_test.go`、`covers_test.go`、`lyrics_test.go` 捕获 POST/Referer/关键词/数量；`legacy_lyrics_test.go` 验证 JSON/JSONP、编码、原文和详情优先/缺失回退。新增 `TestQQPlayBlockErrorsPreserveOtherChannels` 覆盖 req/plain 的状态错误、缺失/null/非法 data，只要另一块有效就保留原文/翻译，不额外回退。 | HTTP 200 中的业务错误仍报错；元数据正常/刷新缓存与手动 NoStore 不混淆；元数据真实缺失歌词回退仍无在线样例。 |

#### Design 关键不变量

| 设计决策 | 本次对照结果 |
| --- | --- |
| 1. 消费侧小接口、保留元数据边界 | `Upstream`、两种 `Provider`、`MetadataProvider` 及可选契约分工吻合；`Result.Validate` 的双重/空形式、缺少身份拒绝由 `TestResultExclusiveForms` 覆盖；公共歌词与封面 DTO 未加入内部 Payload。 |
| 2. 单向依赖和集中登记 | 具体适配器可导入消费侧契约，lyrics/covers 不反向导入适配器，provider 父包仅依赖 domain 与通用库；`assembleUpstreams` 同时构建三集合及图片策略，失败不发布半套。没有反射/万能能力注册框架。 |
| 3. 协议唯一、业务投影独立 | QQ 元数据默认 25/合法上限 50，TitleOnly 独立；QQ/163 封面召回 30、最多 20，歌词 20；对应请求捕获及投影单测通过，无歌曲 MID 的专辑封面、163 `pic-...` 与未知真实尺寸均保留。歌词评分/去重、2 MiB 文本、通道合并在 lyrics，40 秒总 Fetch 上下文包含所有子请求。 |
| 4. CAA 和既有封面机制 | CAA 的窄 recall 与 CatalogLoader 不直接写 store；固定发行优先、10 个去重、并发仍进入同一池。目录与缩略图共享基本结构解析但不互相套用身份校验；actual release/group 回退、archive.org 直连/规范地址降级及文件保护回归保持。 |
| 5. 缓存/传输/平台桶 | 三种 CacheMode，QQ/163 共用协议不共享手动响应缓存；请求级 HTTP client 浅副本不改共享 CheckRedirect。API 桶与图片池分离，重试/旧歌词回退重新取令牌。清除旧无调用 HEAD 路径后，没有第二套上游图片探测链。 |
| 6. 设置与会话生命周期 | `upstream_lifecycle_test.go` 覆盖未获取旧候选、搜索/下载期间保存、MoveRoot、禁网后的本地采用与排队拒绝、关闭等待下载/搜索/采用后再关 Store；`qq_settings_test.go` 验证原位变速与新会话无突发续杯。 |
| 7. 测试迁移、接口和验证边界 | 生产边界测试、外部组合测试及原 fixture 保留；对比 `6814557` 的歌词/封面公开绑定和 12 个 DTO 的带 JSON 标签字段，名称/类型/标签/顺序一致（忽略纯注释变化）。离线/浏览器/在线/原生证据按阶段分别记录。 |

#### 重复协议、旧链路及测试/许可清理

审阅不是仅按目录名判断。执行 `git diff --name-status --find-renames=40% 6814557..HEAD`、
生产源码 `rg` 检索、函数调用追踪和 AST 边界测试，并逐项处理如下：

| 对象 | 删除、收敛或保留的依据 |
| --- | --- |
| QQ 三份歌曲搜索 | 唯一 `DoSearchForQQMusicDesktop` 请求构造在 `qqmusic/search.go`；`SearchTracks/SearchCovers/SearchLyrics` 都调用 `searchSongs → musicu → parseSongs`，不再从歌词/封面走元数据候选裁剪。 |
| QQ musicu 状态解析 | 审阅发现手动歌词仍单独解码顶层与块状态。本次抽出 `decodeEnvelope/decodeBlock`，元数据 `decodeBlocks` 和手动 `qqBlockData` 共用，删除冗余 `qqLyricBlock` 类型；业务分别保留整次失败与通道部分成功。旧端点 JSON/JSONP/原文解析原已集中，继续复用。 |
| 网易云重复 WEAPI/搜索 | 表单和加密只有 `netease/weapi.go:Form`，请求/歌曲结构只有 `netease.go`；`lyrics.go/covers.go` 只按各业务映射。酷狗/酷我协议只在各自适配包。 |
| CAA 索引重复解码 | 审阅发现目录与缩略图各自 Unmarshal/检查 images。本次共用 `urls.go:parseIndex`，`ParseCoverCatalog` 保留持久化身份/档位校验，缩略图只投影 URL；`index_test.go` 覆盖相同结构错误、合法空数组与不同身份要求，未把目录限制扩大到缩略图。 |
| 旧直连 iTunes 图片探测 | `ResolveArtwork → VerifyArtwork → Client.Head` 除自身和 Head 门禁测试外无生产调用，本次全部删除；移除只服务已删 Head 的断言，保留 API 缓存/排队门禁测试。图片首次/重定向门禁、下载校验与自动降级继续由 covers/App 既有测试覆盖。 |
| 旧大接口与生产路径 | `provider.Provider`、`CoverGroup`、`WebSource`、旧静态来源校验、旧配额用途前缀、MusicBrainz 旧 SearchCovers/直连目录实现、lyrics 内 QQ/QRC/四来源请求解析已删除；旧同名测试如 `TestWebSourceFixturesAndErrors` 现在调用实际 QQ/163 能力，不是旧生产壳。新增 `TestRetiredUpstreamPathsStayRemoved` 防止旧文件/接口/探测入口恢复。 |
| 仍使用的通用/图片工具 | `provider.CoverImage` 仍被 iTunes `ArtworkLadder` 和 CAA `CAAFrontLadder` 使用；`LegacyLadder` 经 `app.coverLadder` 服务 `BuildCoverRefs` 与自动降级，不能当死代码删掉。`ParseCAAThumbnailIndex` 由 App 注入共享池；`ArtworkURL/CoverImages/ImageURLs` 仍服务搜索、目录及自动图片。`store.CoverImage` 是复用 domain 并维持既有 DTO 命名的类型别名，不是旧 Provider 兼容壳。 |
| 原测试迁移 | 以 `6814557` 的 lyrics、sources、MB、QQ、iTunes 测试函数清单核对。四来源排序/时长测试改名为 App 的 `TestAllLyricSourcesRankAndDeduplicate/TestLyricSearchUsesTargetDuration`；旧 CAA SearchCovers 测试改为适配器 ArtworkStrategy 测试，配额键常量比较升级为 MB 薄委托及真实 HTTP 记账测试。QQ/QRC 和 CAA 目录 fixture、池合并/取消、文件回滚、歌词事务覆盖均保留。 |
| QRC 许可和依赖 | `provider/qqmusic/qrcdes/{LICENSE,README.md,data.go,des_impl.go,int_helper.go}` 与原 `lyrics/qrcdes/` 逐文件字节一致，保留 MIT 许可及来源提交 `866e996416b0cec7bef648400633f6483c4200d5`；QRC 调用层的有界解压/转换测试仍在 QQ 包。`go.mod/go.sum` 未变，不新增依赖、不提交第三方歌词全文或本机数据。 |

本次只修正上述范围内的死链、重复解析与失效注释，未扩展到无关模块、数据库兼容、播放器、
新歌词格式或其他 known gaps。主架构同步说明共享解析和旧探测清理，不等后续 archive 自动补写。

#### 本次 8.4 实际执行

所有 live/probe 开关均显式置空；使用本地 fixture、HTTP 替身/回环服务与临时存储，未发送真实来源探测：

```sh
env SONEXA_LYRICS_LIVE= SONEXA_LIVE_COVERS= SONEXA_LIVE_ITUNES= \
  SONEXA_QQ_METADATA_LIVE= SONEXA_QQ_FALLBACK_DISCOVERY= SONEXA_QQ_RATE_LIVE= \
  SONEXA_QQ_PROBE= go test -count=1 -race ./internal/...
go vet ./...
go list ./internal/...
npm --prefix frontend test
npm --prefix frontend run typecheck
openspec validate refactor-upstream-capabilities --strict
git diff --check
```

- 清理后全量 race：18 个有测试包全部通过、3 个无独立测试文件；App 52.557 秒、covers 23.054 秒、provider/qqmusic 1.400 秒、acceptance 9.362 秒。`go vet`、`go list` 通过，未使用 Go 结果缓存替代本次回归。
- 定向 provider/acceptance race 通过；另以 `-v -count=1` 执行依赖边界、检查器拒绝回归和旧路径删除共 3 个顶层测试，全部通过。
- 显式关闭开关后执行 `TestLive.*` 与 `TestLocalQQProbe`：四来源歌词/封面、QQ 元数据/查找/速率、iTunes live 与本地 probe 均跳过。随该正则执行的 `TestLiveAuditStopsBeforeAnotherRequest` 是注入传输的离线观察器测试，其 2001/429/301 等日志是合成响应，**不是新在线证据**。
- 前端：15 个文件、71/71 单测通过，`vue-tsc --noEmit` 通过。本次未重新生成绑定、运行 build 或 Playwright；任务 8.2 的构建和 Chromium/WebKit 76/76 结果保留为先前阶段证据，不冒充本次执行。此轮仅改内部 Go 解析/死链、测试与文档，公开 DTO/绑定未改；生成绑定、dist、报告不提交。
- `openspec validate refactor-upstream-capabilities --strict`、最终 `git diff --check` 通过。四份 delta 未同步至主规格、未归档；主架构仍链接本变更的唯一 QQ 规范性速率规则，主规格的旧值/配额描述不能当作新实现的现状。

#### 尚未验证与保留阻塞

任务 **8.3 保持未完成**。最新扩大查找已在 `An Ending (Ascent)` 真实触发缺少内联歌词的
回退，但正文不可用，后续候选返回 -1901 后停止；“成功补取”仍无样例。独立复测仅酷狗歌词
和网易云封面通过，其他失败/未执行项见上面的最新结果。前一轮 45 档出现 2001 后停止，
60 档未发送，速率入口的取消断言未到达；本轮不再重跑 QQ 或提高速率。
共享解析清理的离线证据与各阶段在线记录分开，不把历史通过算作本轮全体来源通过。

原生窗口、输入法、文件选择器、真实断网及大曲库未新增人工验收；既有截图测试断言和大 chunk
提示未解决。**G14/G16/G17 保持未关闭**，不根据本次源码/fixture/浏览器证据推定上游可用、
长期安全额度或原生体验通过。后续由用户决定补在线证据及进入 verify/archive 的时机。

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
