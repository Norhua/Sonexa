# 验证记录

变更：fix-track-identity-and-reduce-import-review。实施与自动化验收已完成；完整结果、历史失败及验证边界如下。未执行原生 Wails 人工或真实来源网络验收，不将自动化通过等同于这些验收。

## 核验修复后最终验收（2026-11-01）

W1（`ebab1ac`）与W2（`599abb5`）已修复，S1注释已清理。核验报告最新结论见 [verification.md](verification.md)；其历史反证继续保留，不再代表未修复项。

| 检查 | 本轮实际结果 |
|---|---|
| 全量 Go，`-count=1` | 21包、871顶层/**1846测试及子测试通过**，0失败；11项测试/子测试skip |
| matching/importer/library/store/app race，`-count=1` | 5包、532顶层/**1141测试及子测试通过**，0失败；9项测试/子测试skip |
| Wails / typecheck | 通过 |
| Vitest | 24文件/**120测试通过** |
| build | 通过；主chunk **1,065.69kB**，gzip271.52kB，既有大chunk警告保留 |
| Chromium/WebKit完整Playwright | **320/320通过**，约1.8分钟 |
| strict / diff | 通过 |

真实网络live/probe开关显式清除；无原生验收或用户数据操作。数据库表结构不变，仍 `track-identity-v9`，原因诊断复用已有JSON列；不做历史数据迁移、规格同步或归档。

### 更新后的D10指标

原15项 `TestRunnerIdentityValidationMatrix` 仍新增4、跳过0、问题11/来源11、错误自动操作0；**搜索调用17→19**。变化只有 remix_author 与 live_event 各1→2：完整版本冲突现在在展示阶段被过滤，因此继续下一查询变体，终态保护不变。

新增 `TestRunnerVersionEquivalenceAndPayloads` 使用真实Runner/library/临时Store，每项独立新库：

| 场景 | 新增 | 跳过 | 问题 | 错误自动操作 | 假搜索调用 |
|---|---:|---:|---:|---:|---:|
| instrumental_alias | 1 | 0 | 0 | 0 | 1 |
| remix_form | 1 | 0 | 0 | 0 | 1 |
| live_form | 1 | 0 | 0 | 0 | 1 |
| remaster_form | 1 | 0 | 0 | 0 | 1 |
| remix_author | 0 | 0 | 1 | 0 | 2 |
| live_event | 0 | 0 | 1 | 0 | 2 |
| rerecording_year | 0 | 0 | 1 | 0 | 2 |

7项合计新增4、跳过0、问题3、搜索10；与原15项合计新增8、问题14、搜索29，反例错误自动操作0。断言来源快照保留上游原文；版本反例虽无展示候选仍保存中文冲突说明。原you/成对CSV/TSV及MusicBrainz replay随全量通过，原有计数不变。

### W2证据

- `TestRunnerSuccessfulDecisionEvidenceAndLogs` 覆盖新建、本地已有、远程已有：规则版本、可靠异写、默认偏好和精确已有原因在终态JSON、operation_result及日志一致。
- `TestAutomaticDecisionEvidenceRollbackAndPlanSeal` 拒绝篡改说明；证据写失败使终态和操作结果全部回滚，去掉故障后同计划可提交并幂等返回同说明。
- `TestRecheckCurrentEvidenceNeverCopiesManualChoice` / `TestRecheckNoResultUsesSufficientCurrentFactsOnly` 验证后台重评也保存依据，且不会把排除竞争者的冲突描述为成功目标的事实。
- selection回归验证已有发行、曲数、国家及稳定排序偏好随签名计划和结果保留；App只读说明投影不改行修订或原证据。
- `review-problems.spec.ts` 验证位置/时长/未知专辑/兼容已收藏偏好中文短句，不显示内部detail作为主说明。

### 本轮历史失败（已处理）

W1新测试先在旧实现按预期失败，确认等价漏召回与不同负载展示放行；实现后扩大回归仅旧D10的两个搜索调用断言失败，按上面明确的行为变化更新，未删除终态/错误自动操作断言。W2编辑中一次参数列表多余括号使gofmt报错，修正后定向及最终全量通过。未将这些中间失败隐去或当作最终结果。

临时日志：`/tmp/sonexa-fix-{go,race}.json`、`/tmp/sonexa-fix-{wails,e2e}.log`；本节记录持久结论，不依赖临时产物留存。

## 后续独立核验（2026-11-01，以下为修复前历史）

对实现 `dae0e64` 重新执行本文件的全量自动化检查，结果仍为 Go 1812、五包 race 1107、前端单测120、Chromium/WebKit320/320，Wails/typecheck/build/strict/diff 通过。但逐规格核验及离线生产入口探针发现 **2 项 WARNING、1 项 SUGGESTION**：版本等价候选被旧检索门槛提前过滤，自动成功原因没有完整传递/保存及转换为简明中文，另有过时注释。没有复现错误自动接受/跳过。

完整位置、复现数据、45需求/124场景证据索引及修复建议见 [verification.md](verification.md)。这不推翻下文实际测试结果，但说明全绿套件不等于规格完全满足；本轮只记录发现，未修改生产代码、重开任务、同步主规格或归档。


## 本轮入口矩阵实际结果

| 用例 | 新增收藏 | 已存在跳过 | 独立问题 / 来源行 | 错误自动操作 | 搜索调用 |
|---|---:|---:|---:|---:|---:|
| 成对 CSV/TSV，原名/译名/全角/等值时长 + 未匹配反例 | 1 | 1 | 1 / 2 | 0 | 7 |
| you：复核添加合集，再导入特典截断 | 2（含初始特典） | 1 | 0 / 0 | 0 | 1 |
| you：搜索添加合集，再导入特典截断 | 2（含初始特典） | 1 | 0 / 0 | 0 | 2 |
| you：显式合集导入，再导入特典截断 | 2（含初始特典） | 1 | 0 / 0 | 0 | 2 |
| you：无专辑输入，再导入特典截断 | 1（初始特典） | 1 | 1 / 1 | 0 | 2 |

- 测试：`TestSubmitImportPairedCSVTSVIdentityAndProblems`、`TestYouRealTrackIdentitiesAcrossReviewSearchAndImport`，定向普通/race 均通过。
- 成对文件经 SubmitImport→生产 Runner→Store，逐项断言精确 track/release、原文不变；第二次前删除首个终态输入，不依赖该历史行。
- you 使用设计提供的两个真实 track MBID 和共同 Recording/ISRC；发行/组及附加字段为确定性构造 fixture，不冒充完整真实来源响应。检查旧字段、来源快照、图片选择与歌单不变。
- 上表“搜索调用”为离线假 provider 方法调用，不是外网请求；纯比较测试不应伪造入库/问题计数。
- D10 已执行：六个业务/来源包定向运行 **152 个顶层测试、445 个 test/subtest，通过且无失败**。新增真实 Runner 的 15 项文本/限定/时长矩阵，避免仅用纯比较器推断终态。完整映射和实际指标见下表；纯比较器用例标注其验证层次，不伪造数据库计数。

### D10 逐项证据映射

以下测试均纳入本轮定向执行（包名前缀为 `internal/`）：

| D10 集合 | 正例及反例对应测试 | 验证边界与结果 |
|---|---|---|
| 所有权 | matching `TestResolveLocal*`；app `TestYouRealTrackIdentitiesAcrossReviewSearchAndImport`；library `TestSingleInputSkipAndDuplicateAcceptNeverChangeLibrary`、`TestSkipRevalidatesDeletionRebindingAndRelease` | source+track+release 才 owned；同 Recording/ISRC 两 track 可并存；删除/换绑/发行冲突拒绝旧跳过，旧收藏表保持不变 |
| 异写与完整版本 | matching `TestMetadataTextAliases`、`TestMetadataQualifierPayloads`、`TestMetadataCreditsPreserveIdentity`、`TestMetadataCreditSourceExplanation`；importer `TestRunnerIdentityValidationMatrix`；app 成对文件测试 | 原名/译名/读音/形式规范化有正例；未知注释、纯译名、不同 remix/live/角色/合作、Off Vocal 反例均无自动接受/跳过 |
| 专辑 | importer `TestRunnerInputAlbumConstrainsBeforeCrossGroupGuard`、`TestRunnerExplicitAlbumExcludesDifferentContentBeforeGroupCheck`；app you 矩阵；matching `TestMetadataPoolConstrainsAlbumBeforeUnknownCompetitors` | you 截断和合集分别命中指定 track，无专辑/未知竞争者保持问题；短前缀与不同专辑不能因同录音消歧 |
| 位置与完整性 | provider/musicbrainz `TestMusicBrainzResolvesPositionsWithoutChoosingFirst`；importer `TestRunnerDecisionFactsReplayAndRevisionBoundary`、`TestRunnerLocalKnownPositionCanDisambiguateRepeatedRecording` | 第二碟精确位置新增1；重复位置/部分清单/请求失败/旧修订新增0、跳过0、问题1；每个 replay 仅1次必要 HTTP，排除发行零请求 |
| 默认偏好 | library `TestSelectionCompatibleOwnedTrackPreference`、`TestAutomaticSelectionPrioritizesOwnedReleaseAndKnownTotal`、`TestSelectionEvidenceUnknownOrFailedFactsNeverManufactureUnique` | 相容已有目标不新增；显式 Deluxe 不被吞掉，跨组/录音/位置/未知总数不默认；已有单目标摘要请求0，多已有比较子集2，不请求无关第三发行 |
| 本地召回 | store `TestLocalRecall*`；importer `TestRunnerLocalFactsSkipWithoutHistoryOrNetwork`、`TestRunnerSparseOrAmbiguousLocalFactsStillSearch`、`TestRunnerSparseExactOwnedTargetStillSearchesBeforeSkipping`、`TestRunnerUnboundTextDoesNotBlockUpstreamAdmission` | 完整本地资料两次导入各跳过1，删除历史批次仍同结果，网络0；稀疏或歧义需搜索；未绑定文本不赋予 owned；超过2000条可召回、截断显式 |
| 问题归并 | matching `TestProblemStrongTextProof`、`TestProblemAnchorRejectsNonTransitiveAliasChains`、`TestProblemExactIdentityNeedsCurrentUnambiguousEvidence`；store `TestProblemLifecycle*`、`TestProblemSplit*`、`TestProblemStats*`；app 成对文件 | 两表异写保留两行/一个问题；不同专辑、缺失通配、相同候选池/非传递别名不归并；拆出不自动归回，跨批状态及原文保留 |
| 原子事务 | library `TestMultiInputAdmissionAtomicAccounting`、`TestMultiInputFailureRollsBackEveryMemberAndEntity`、`TestConcurrentMultiInputPlansFinishOnlyTheirOwnScope`、`TestInputScope*`；app `TestAppMultiInputAdmissionAndExactSkip` | 两行新增时新增1/跳过1/问题0；已有目标跳过2；两个问题并发同目标新增1/跳过3；任一成员/结果/文件失败全部回滚，旧批准不扩范围 |
| 异步与重评 | library `TestRecheckCurrentEvidenceNeverCopiesManualChoice`、`TestRecheckDoesNotConsumeLeaseOrConcurrentFactChange`；app `TestLibraryEventSchedulesCurrentProblemWithoutNetwork`、`TestLateReviewSearchCannotRestoreSkippedInput`、`TestProblemResearchAppRejectsMemberChangeAndKeepsQuery`；store/importer `TestProblemLease*` / `TestRunnerDefersInteractingProblemWithoutNetworkOrFailure` | 只结束当前证据明确已有的输入；跨组/显式另一版本不继承人工决定，资料不足排原队列；交互中无后台终态、旧搜索/计划不可恢复，释放后重评 |

### 真实 Runner 文本/限定矩阵指标

每项独立新库，不含预置收藏。`searches` 是固定搜索器调用；自动入口保留 relevance=100，时长冲突仍独立生效。

| 场景 | 新增 | 跳过 | 问题 / 来源行 | 错误自动操作 | searches |
|---|---:|---:|---:|---:|---:|
| explicit_aliases | 1 | 0 | 0 / 0 | 0 | 1 |
| ordinary_vocal | 1 | 0 | 0 / 0 | 0 | 1 |
| duration_3s | 1 | 0 | 0 / 0 | 0 | 1 |
| duration_10s | 1 | 0 | 0 / 0 | 0 | 1 |
| duration_over_10s | 0 | 0 | 1 / 1 | 0 | 1 |
| unknown_annotation | 0 | 0 | 1 / 1 | 0 | 1 |
| pure_translation | 0 | 0 | 1 / 1 | 0 | 2 |
| remix_author | 0 | 0 | 1 / 1 | 0 | 1 |
| live_event | 0 | 0 | 1 / 1 | 0 | 1 |
| off_vocal | 0 | 0 | 1 / 1 | 0 | 2 |
| different_role | 0 | 0 | 1 / 1 | 0 | 1 |
| different_collaboration | 0 | 0 | 1 / 1 | 0 | 1 |
| explicit_deluxe | 0 | 0 | 1 / 1 | 0 | 1 |
| short_album_prefix | 0 | 0 | 1 / 1 | 0 | 1 |
| wrong_disc | 0 | 0 | 1 / 1 | 0 | 1 |

15 项合计：新增4、跳过0、独立问题11/来源11、错误自动操作0、搜索调用17。纯译名与 Off Vocal 被展示过滤后尝试第二查询变体，不能把无结果当作身份已确认。

MusicBrainz replay 五项均为一次必要本地 HTTP：second_disc 新增1；repeated_position、partial_list、failed_request、stale_input 各新增0/跳过0/问题1。failed_request 为失败问题，其余为待选；精简清单不冒充完整真实网络响应。

## 最终全量验证

以下在当前实现上实际执行，Go 使用 `-count=1` 禁用测试缓存：

```sh
go test -json ./... -count=1
go test -json -race ./internal/matching ./internal/importer ./internal/library ./internal/store ./internal/app -count=1
/Users/amadeus/.go/bin/wails generate module
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run build
SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e
openspec validate fix-track-identity-and-reduce-import-review --strict
git diff --check
```

| 检查 | 实际结果 |
|---|---|
| 全量 Go | 21 个有测试包通过，**864 个顶层测试 / 1812 个测试及子测试通过，0 失败**；根包与 qrcdes 无测试 |
| 五包 race | 5 包通过，**525 个顶层测试 / 1107 个测试及子测试通过，0 失败** |
| Wails 绑定 | 重新生成通过；生成目录按项目约定忽略 |
| 前端 typecheck | 通过 |
| Vitest | **24 文件 / 120 测试通过** |
| 前端 build | 通过；主 chunk **1,064.87 kB**，gzip 271.35 kB，保留 >500 kB 既有警告 |
| 全量 Playwright | Chromium/WebKit **320/320 通过**，约 1.8 分钟，使用构建预览与 Wails 替身 |
| 严格规格 / diff | 通过 |

最后一致性扫描发现自动新增日志仍含“置信率 xx%”，已改为“检索分”，不改变评分/选择；删除未被生产调用的旧四项 SummaryLine 后再次完整运行 Go/race。最终测试计数因此比上一轮各少 1。前端产物未因该后端日志文案调整变化。

真实网络 probe/live 用例保持默认跳过（NetEase/QQ probe、live covers/lyrics/QQ 等）；另有验证默认跳过门禁的三个子用例主动 skip。这些不计入上述通过数，不宣称真实来源连通性已验证。完整本次自动化结果已汇总在本文件；`/tmp/sonexa-{go,race}-release.json` 与 `/tmp/sonexa-e2e-final-fixed.log` 仅为临时诊断，不是持久交付依赖。

## 一致性核对

- `ResolveLocal` 的消费接口只有 `FindExactBinding`，不提供 ISRC/文本查询；检查了来源精确身份、发行关系及有效修订才返回 owned。
- `MergeCandidate`、`MarkMerged`、`merged_into`、`StatusMerged`、`CheckLocalDuplicate` 的生产引用为零；剩余测试字符串用于断言旧协议/列不存在，说明文档中的名称为删除记录。
- 自动流水线检查完整 profile→当前候选事实→输入筛选→相容版本偏好→精确所有权。保留人工候选，不按全人工列表中存在跨组候选就否决自动目标；仍相容的跨组/不同内容/位置冲突继续复核。
- 读取 `operation_result` 仅按成功计划 ID/hash 用于同请求幂等；本地召回和所有权不查历史输入/选版，活跃问题查询约束 active 状态。事务重验完整 InputScope/ProblemScope，不以历史批准扩大未来范围。
- 来源身份解释在 provider 的 CandidateResolver 实现中；matching/library/importer 无新增 MusicBrainz/iTunes/QQ 来源 switch 分发，App 集中装配注入。
- 以本变更实现前 `d5c60dd` 为差异基线，`internal/lyrics`、`internal/lyricdoc`、前端歌词业务代码及旧 CompareTitle/CompareCredit/ScoreCandidate 行为无改动；歌词全量 Go 与前端回归通过。App 上游装配仅额外发布身份能力，不修改歌词协议。
- 保留编辑器归组/成员合并及专辑合并展示，它们不是候选所有权合并。兼容逐行读取入口仍存在但 UI 已使用问题队列，旧单行修改不能绕过范围或租约。
- 更新 matching/store 局部 README 中过时的阶段状态；README 标明本版本未做原生验收。主规格 delta 同步及归档不在本次执行范围，尚未进行。

## 历史失败与修复（不掩盖）

### Go

执行 `go test -json ./...`：1796 个 test/subtest 通过，`internal/covers.TestCancelOutOfView` 失败：

```text
TempDir RemoveAll cleanup: unlinkat .../TestCancelOutOfView.../001/tmp: directory not empty
```

当时测试退出前释放 HTTP handler，但没有等待 visible Fetch 写盘结束，后续 `&& race` 被阻断。用户确认后仅修改测试：用启动信号和实际在途登记替代固定等待，退出前等待可见任务完成并调用 Pool.Close；断言取消原因、未发出的排队请求及保留任务成功。普通测试重复50次、race重复20次通过，随后最终全量 Go/race 均通过。生产下载池未修改。

### Playwright

此前一次构建预览模式的全量 Playwright：**314/320 通过，6 失败**。

- 新增来源 Select 导致旧 `list-edges` 与 `provider-capabilities` 各双浏览器定位不唯一。已按“上游/地区”可访问名称定位，相关两文件14/14通过。
- `cover-grid` 双浏览器原断言 `/variantId=32$/`（另有11/12）不允许 URL 后带 `searchId`。用户确认后改为解析参数，同时核对各来源自己的 searchId，保留最小规格/请求数量/显式采用断言。首版测试错误使用全局最后会话 ID，随后核实 store 的逐来源会话约定并修正。双浏览器重复三轮12/12通过，最终完整320/320通过。未修改封面生产行为或移除 searchId。
- 实施期间发现并修复的相关问题还包括：队列快照覆盖本地身份刷新失败后的不可操作状态、租约重新获取事件引起假 stale、来源 Select 无可访问名称。均有对应浏览回归通过。

截图/trace 产物位于忽略目录 `frontend/test-results/`，后续运行可能覆盖；完整通过与历史失败分别记录，不用局部通过替代全量。

## 验证边界与后续

- 不运行原生 Wails 人工验收或真实网络；浏览器用 Wails 替身，Go 用临时 SQLite/httptest/固定事实。未清理、迁移或读取用户应用数据。
- 数据代际仍 `track-identity-v9`；未同步主规格、未归档。
- 自动化任务完成后可独立执行规格核验、主规格同步及归档；本次未归档，不以构建/浏览器替身通过宣称原生或真实网络验收完成。
