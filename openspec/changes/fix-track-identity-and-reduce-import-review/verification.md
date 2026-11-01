# 独立核验报告

变更：`fix-track-identity-and-reduce-import-review`

日期：2026-11-01
核验实现：`dae0e64`；实现差异基线：`d5c60dd`。本轮不修改生产代码、实施任务或主规格，不归档，不操作用户应用数据。

## 结论

**现有自动化套件全部重新通过，但不能据此称为规格完全一致。发现 2 项 WARNING、1 项 SUGGESTION，无 CRITICAL。**

| 维度 | 核验结果 |
|---|---|
| 完整性 | CLI `all_done`，48/48 任务；10 份 delta 共 45 个 ADDED/MODIFIED 需求、124 个场景，均有可定位的相关实现；没有 REMOVED/RENAMED 需求 |
| 正确性 | 45/45 需求均完成实现定位，并按场景核对代码/测试证据；发现 W1（版本检索门槛）、W2（决策说明链路）两类偏差及关联覆盖缺口，不将已定位计为全部符合 |
| 一致性 | D1–D9 的身份、接口、事实分离、事务和租约架构总体遵循，D10 现有矩阵通过；检索入口和说明链路尚不完全符合 D2/D9，另有过时注释 |

以上是规格核验的代码/自动化证据判断，不是逐条场景的原生人工验收。未执行原生 Wails 或真实网络验收；它们是本次报告的验证边界，不新增为归档前置条件。主规格尚未同步不作为当前 delta 实现的错误。

按核验流程可带已注明警告进入后续归档评估；**建议先修复 W1/W2，再同步和归档**，不要把当前 48/48 与全绿套件解释为没有规格缺口。本轮不自动转入实施。

## 问题

### CRITICAL

无。未发现剩余未勾选任务、完全缺失的需求实现，或候选 ISRC 合并/多行部分提交一类阻断性问题。

### WARNING W1：元数据版本语义没有接入生产检索准入

**规格：** `specs/matching/confidence/spec.md:5–18` 的“曲名与辅助字段检查”，特别是“可识别版本等价写法”；关联 D2 与共用检索入口约束。

**位置：**

- `internal/matching/pipeline.go:110–124`：生产 `Engine.Run` 先调用旧 `ScoreCandidate`，并按 `Rejected/Plausible` 丢弃候选。
- `internal/matching/confidence.go:90–134`：旧 `CompareTitle` 先比较字面版本关键词集合，再比较文本相似度。
- `internal/matching/metadata_qualifiers.go:17–25`、`internal/matching/metadata_qualifiers_test.go:16`：新比较器已明确支持 Instrumental / off vocal 等价及完整版本负载。
- `internal/importer/runner.go:261–265`：只有通过旧检索池的候选才进入新的 `SelectCandidate`。

**离线实际结果（相同 Artist/Album，精确位置齐备）：**

| 输入 → 上游候选 | 新元数据证据 | 旧检索是否拒绝 | Engine 展示候选数 |
|---|---|---:|---:|
| `Song (Instrumental)` → `Song (off vocal)` | eligible / verified_alias | 是 | 0 |
| `Song (Live Tokyo 2020)` → `Song (Live Osaka 2020)` | excluded / conflict | 否 | 1 |
| `Song (A Remix)` → `Song (B Remix)` | excluded / conflict | 否 | 1 |
| `Song (Re-recorded 2020)` → `Song (Re-recorded 2021)` | excluded / conflict | 否 | 1 |

进一步通过现有测试 helper 驱动真实 Runner→library→临时 Store：Instrumental 输入、off vocal 唯一正确候选得到 **新增0、跳过0、复核1、问题1、未匹配1、假搜索调用2**。这不是外网没有结果，而是正确候选被本地旧门槛过滤。

反向的不同 Remix/Live 仍会被后续自动证据排除；**本轮没有复现错误自动接受/跳过**，不能把展示放行误报为已经错误入库。但展示行为仍偏离“完整限定冲突不因共有关键词而放行”的要求。

**建议：** 在元数据共用检索入口增加独立的版本语义准入/比较路径：可靠等价形式不能被旧关键词集合提前拒绝，完整版本负载冲突不能仅因高相似度通过。保持未知注释可召回、辅助字段规则及默认数值权重；不要直接改歌词仍复用的旧 `CompareTitle/ScoreCandidate` 行为。补 Engine/四入口共享链路及 Runner 正反例，至少包括上述四对，断言候选、终态、目标和请求数。

现有 `TestMetadataQualifierPayloads` 只证明纯比较器支持；`TestRunnerIdentityValidationMatrix` 有普通人声和版本冲突反例，没有 Instrumental/off vocal 的生产正例，因此现有全绿套件没有暴露该缺口。

### WARNING W2：自动成功分支丢失决策说明，复核说明未转换为用户短句

**规格：** `specs/matching/confidence/spec.md:96–101` 的“自动判断说明”及“沿用兼容的已收藏发行”；关联 D9、pipeline 日志与 app-shell 的说明展示约束。

**位置与实际证据：**

- `internal/library/selection.go:157`、`:212` 已生成选版与字段 `Reasons`。
- `internal/library/local_existing.go:87–95` 的本地成功路径只返回偏好原因，没有汇总所采用的字段证据。
- `internal/importer/runner.go:292–301` 在接受/跳过成功时提前返回通用日志；`:326–340` 的证据序列化只对进入复核的分支运行。
- `internal/store/admission_plan.go:213` 保存终态及 `existing_track` 等终态原因，不保存本次选版说明；`AdmissionResult` 也没有传递字段/偏好原因。
- `frontend/src/components/ReviewReasons.vue:27–29` 直接显示 `item.detail || item.code`，大量只有 code 的字段证据显示为 `duration_conflict`、`position_ambiguous`、`target_album_unresolved` 等内部码，不是简明中文说明。

离线 Runner 探针，预置完整有效本地事实，导入 `Song（译名：歌曲） / Artist / Album`：

```text
selection_reasons=[{Code:prefer_owned_compatible_track Field: Detail:相容候选内的默认选版偏好，不构成身份等价}]
status=skipped
resolution_reason=existing_track
review_evidence=<nil>
line=[已存在跳过] Song（译名：歌曲） · Artist · Album；精确目标 musicbrainz:local-track（曲目 #1）
```

精确跳过本身正确，收藏没有被修改；缺失的是“未指定细发行，优先已收藏的兼容版本”及可靠异写等决策依据。通用 `existing_track` 不能替代为什么选定这个 track 的说明。

**建议：** 把本次实际采用的字段证据和偏好原因贯穿本地/远程成功路径、计划/结果和终态诊断，在相同提交边界保存；日志及复核 UI 将稳定 code 转成中文短句，code 可作为辅助诊断保留。不得把这些诊断重新用于未来输入选版。补成功接受、自动已有跳过、后台重评的原因保存/输出测试，以及冲突/未知/默认偏好的中文展示断言。

### SUGGESTION S1：仍有与新行为相反的代码注释

- `internal/matching/matching.go:1–2` 仍把“候选合并判定”列为本包职责。
- `internal/matching/confidence.go:212–213`、`:313` 仍称 HighConf 决定是否需要人工/入库，而新流程已明确独立自动证据。
- `internal/app/bindings_review_problem.go:13–14` 仍说多来源 UI 尚未接线。

**建议：** 仅更新这些注释，明确旧评分只服务展示、旧逐行 API 是受范围约束的兼容入口；不要因清理注释改变歌词比较行为。同步主规格时另外核对 `openspec/specs/matching/provenance/spec.md` 的 Purpose，常规 delta 合并不会自动替换其旧 ISRC 描述。

## 需求与场景证据索引

以下覆盖全部 45 个需求；每组场景数来自已完整读取的 delta。代码映射结合对应场景条件与测试检查，不以任务打勾或测试总数替代行为证据。已有详细 D10 fixture 指标见 [validation.md](validation.md)。

| 能力 / 需求（完整列举） | 需求 / 场景 | 主要实现及测试证据 |
|---|---:|---|
| library/admission：原子结果与重复请求；精确跳过的原子校验；问题成员批准范围 | 3 / 9 | `internal/store/admission_plan.go:70,176`、`internal/store/input_scope.go:64,206`；`TestSingleInputSkipAndDuplicateAcceptNeverChangeLibrary`、`TestSkipRevalidatesDeletionRebindingAndRelease`、`TestMultiInputAdmissionAtomicAccounting`、`TestMultiInputFailureRollsBackEveryMemberAndEntity`、`TestConcurrentMultiInputPlansFinishOnlyTheirOwnScope`、`TestInputScopeInvalidatesApprovalsWithoutExpandingRows` |
| matching/confidence：曲名与辅助字段检查；评分与高置信；配置与代表发行；独立歌词匹配的适用边界；自动决策证据独立于展示分数；先确定目标专辑再比较发行；位置与时长作为独立证据；自动判断说明 | 8 / 18 | `internal/matching/metadata_evidence.go:76`、`internal/library/selection_evidence.go:23`、`internal/library/selection.go:42,131`；`TestMetadataEvidenceDoesNotUseSearchScore`、`TestMetadataPoolConstrainsAlbumBeforeUnknownCompetitors`、`TestMetadataPositionAndMissingEvidence`、`TestHighConfidenceIsStructural`、`TestLadderIsDeterministic`、`TestDisplayPreferencesDoNotChangeSelectionOrBindings`；**W1/W2 为已复现的偏差** |
| matching/pipeline：导入处理与状态；自动入库判定；持久化、重试与恢复；进度与日志；共用检索规则；缺失基本信息的分流；本地候选与上游事实共同解析；当前证据重评而非历史选择复用 | 8 / 23 | `internal/importer/runner.go:231`、`internal/library/local_existing.go:15`、`internal/library/selection.go:182`、`internal/library/problem_recheck.go:30`；真实成对 SubmitImport、you 多入口矩阵、`TestRunnerSparseOrAmbiguousLocalFactsStillSearch`、`TestSelectionCompatibleOwnedTrackPreference`、`TestRecheckCurrentEvidenceNeverCopiesManualChoice`、`TestSummarySeparatesExistingSkipsFromNewTracksAndProblems`、`TestFinalProgressReadsStoppedBatch`；共用检索及日志边界见 W1/W2 |
| matching/provenance：身份与唯一性；候选本地识别；重复入库处理 | 3 / 9 | `internal/matching/provenance.go:108`、`internal/store/lookup.go:27`、`internal/library/identity.go:35`；`TestResolveLocal*`、`TestExactCandidateOwnershipFromCurrentBinding`、`TestAdmissionResolvesSyntheticTargetAndRejectsSeedBypass`、`TestYouRealTrackIdentitiesAcrossReviewSearchAndImport`；前端 `candidate-ownership.spec.ts`、`identity-resolution.spec.ts` |
| matching/text-processing：注释分类与查询主体；三字段异写证据；录音版本与发行限定；署名身份与参与信息；截断与缺失信息 | 5 / 12 | `internal/matching/metadata_text.go:27,88`、`metadata_qualifiers.go:36`、`metadata_credit.go:65`、`internal/importer/profile.go:19`、`parse.go:388`；`TestMetadataTextAliases`、`TestMetadataQualifierPayloads`、`TestMetadataCreditSourceExplanation`、`TestMappedProfilePreservedThroughQueueAndRunner`、`TestRunnerInputAlbumConstrainsBeforeCrossGroupGuard`；新比较器与旧检索连接问题单列 W1 |
| platform/app-shell：导航与页头；共享候选表格；问题数与行数展示 | 3 / 8 | `frontend/src/components/AppSidebar.vue`、`CandidateTable.vue`、`frontend/src/lib/candidateActions.ts:15`、`frontend/src/stores/tasks.ts:23`；`visual-controls.spec.ts`、`candidate-ownership.spec.ts`、`task-problem-counts.spec.ts`；说明呈现边界见 W2 |
| platform/data-store：补全与复核状态；原始行与活跃问题分离；可重建的当前匹配证据；队列终态原因与一致性 | 4 / 12 | `internal/store/migrations.go:253,298,315`、`problems.go:53`、`local_recall.go:63,159`、`queue.go:291,513`；`TestQueueIntegerRevisionAndOriginalInput`、`TestProblemLifecycleConcurrentAcrossBatchesAndRestart`、`TestLocalRecallBeyondBrowseLimitAndExplicitTruncation`、`TestLocalRecallRebuildVersionRestartAndCancelledRead`、`TestEnrichStateCountsPartitionAllRows`、`TestRunnerLocalFactsSkipWithoutHistoryOrNetwork` |
| review/review-queue：队列浏览；候选与请求归属；处理动作；逐条确认；活跃问题语义归并；归并范围修订与人工搜索 | 6 / 19 | `internal/matching/problem_equivalence.go:100`、`internal/store/problem_protocol.go:191,225,271,320,361`、`problem_lease.go:154,336`、`frontend/src/views/ReviewView.vue:96,211,249,265`；`TestProblemAnchorRejectsNonTransitiveAliasChains`、`TestProblemQueryCASRejectsLateResultsAndScopeChanges`、`TestProblemLease*`、`TestProblemNoMatchNeededUsesDisplayedSingleScopeAndLease`；`review-problems`、`review-problem-actions`、`review-leases`、`review-actions` 浏览用例 |
| upstream/musicbrainz：查询与结果粒度；合成候选解析为真实位置；决策资料获取有界且不创建收藏 | 3 / 9 | `internal/provider/musicbrainz/identity.go:18,42,88,169`；`TestMusicBrainzResolvesPositionsWithoutChoosingFirst`、`TestMusicBrainzSameRecordingDifferentReleaseRemainsSeparate`、`TestDecisionRequestSharesReleaseAndNeverEnumeratesGroup`、`TestDecisionFetchErrorsGatesRetriesAndCancellation`、真实 Runner `TestRunnerDecisionFactsReplayAndRevisionBoundary` |
| upstream/provider-registry：候选精确身份与决策资料契约；决策资料不等于收藏或身份映射 | 2 / 5 | 消费接口 `internal/matching/identity.go:68`；注入 `internal/app/upstreams.go:61,114`；来源实现 `internal/provider/{musicbrainz,itunes,qqmusic}/identity.go`；`TestAlreadyExactProviderIdentityContractsWithoutNetwork`、`internal/matching/identity_test.go` 契约反例、`TestAdmissionNeverBorrowsImportValues` |

### 架构及旧行为扫描

- 所有权接口只提供 `FindExactBinding`，source+track+release 与当前修订共同校验；同 Recording/ISRC 不同 track 可分别收藏。
- `MergeCandidate`、`MarkMerged`、`StatusMerged`、`merged_into`、`CheckLocalDuplicate` 没有生产路径引用；保留的测试字符串用于确认旧接口/列不存在，编辑器归组合并不属于候选合并。
- `CandidateSources` 按注入接口分派；matching/library/importer 未发现新增 MusicBrainz/iTunes/QQ 身份格式 switch。
- 当前值、来源快照和清单保持独立事实视图；问题等价保留公共证明，不使用别名两两相容的传递闭包。
- InputScope、问题/查询/候选/成员修订纳入计划；事务在写实体前重验；精确跳过不写收藏。后台重评复用当前事实，不复制历史人工选版。
- `operation_result` 只按成功计划 ID/hash 提供同请求幂等，不作未来输入身份映射。
- 与 `d5c60dd` 比较，`internal/lyrics`、`internal/lyricdoc`、歌词前端业务及旧 `confidence.go/searchcore.go` 无实现差异；歌词自动化随全量通过。恰因旧比较路径保留，修复 W1 必须使用元数据专用入口，不能破坏该隔离。
- 新库代际仍 `track-identity-v9`；未迁移、读取或清理用户应用库。

## 本轮实际重跑

Go 显式清除了 live/probe 开关，均使用 `-count=1`；浏览器使用刚构建的预览产物与 Wails 替身。

| 检查 | 结果 |
|---|---|
| `go test -json ./... -count=1` | 21 个有测试包，864 顶层 / **1812 测试及子测试通过**，0 失败；11 个测试/子测试 skip |
| 五包 `go test -json -race ... -count=1` | matching/importer/library/store/app，525 顶层 / **1107 测试及子测试通过**，0 失败；9 个测试/子测试 skip |
| `/Users/amadeus/.go/bin/wails generate module` | 通过 |
| frontend typecheck / Vitest | 通过；24 文件 / **120 测试** |
| frontend build | 通过；主 chunk 1,064.87 kB，gzip 271.35 kB；既有 >500 kB 警告仍在 |
| 完整 Playwright | Chromium/WebKit **320/320**，约 1.8 分钟 |
| 严格 delta 校验 / diff 检查 | 通过 |
| 独立版本语义探针 | 上述四对实际打印结果；运行成功代表探针执行完成，不代表规格满足 |
| overlay Runner 探针 | 两个诊断测试执行通过并打印 W1/W2 的反证；未写入正式测试集，未改生产代码 |

临时日志为 `/tmp/sonexa-verify-{go,race}.json`、`/tmp/sonexa-verify-{wails,e2e}.log`。本轮诊断探针曾放在忽略目录 `tmp/harness/identity-verify/`，验证完成后已清理；全量 Go 运行时其 main 包也被发现并计为无测试包，不改变上述有测试包/测试通过数。复现要点保留在下节，临时文件不是报告成立所依赖的持久交付物。

### 最小复现要点

W1 可在仓库内任意允许导入 internal 的临时 Go 包调用：

```go
c := provider.Candidate{Title: ptr("Song (off vocal)"), ArtistName: ptr("Artist"), AlbumTitle: ptr("Album")}
s := matching.DefaultWeights().ScoreCandidate(provider.Query{
    Title: "Song (Instrumental)", Artist: "Artist", Album: "Album",
}, c)
// 实际 s.Rejected == true；新 CompareMetadataText 对同一标题对返回 verified_alias。
```

完整 Engine 探针使用只返回该候选的离线 `Searcher`，设置 `engine.Weights = matching.DefaultWeights()`，`Engine.Run` 得到空 `Scored`。Runner 复现复用 `internal/importer` 的 `openStore`、`mbCandidate`、`enqueueOne`、`recordingSearcher`、`newRunner`、`runToCompletion`，以 `go test -overlay` 加入临时测试：候选 `Song (off vocal)`，输入 `Song (Instrumental)`，结果为上文新增0/问题1。

W2 复用同包 `seedLocalRunnerTrack(t, st, true, false)` 建立完整有效事实，导入 `Song（译名：歌曲） / Artist / Album`；先查看 `PrepareLocalExisting(...).Reasons`，再通过 `runner.processItem` 提交并读取 `GetItem`。观察偏好原因存在于 selection，但终态只有 `existing_track`，`ReviewEvidence` 为 nil，日志没有该偏好说明。

## 后续建议

1. 独立实施 W1 的元数据检索接线与端到端回归，保留歌词边界。
2. 实施 W2 的原因传递、终态诊断及中文说明；同时清理 S1 注释。
3. 更新 D10 验收矩阵并重跑受影响/全量检查，再核验本报告两项偏差。
4. 按用户请求同步主规格（包括 Purpose 等非 delta 正文），归档另行执行。本报告不改变任务勾选，不代表已修复这些问题。
