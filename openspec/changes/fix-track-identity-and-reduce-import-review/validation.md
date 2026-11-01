# 验证记录（进行中）

变更：fix-track-identity-and-reduce-import-review。当前完成 **44/48**（1.1–9.2）。9.3–9.6 未完成，禁止用下列局部通过代替最终全量通过。

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

## 已通过的阶段验证

- 8.1：Wails 生成、typecheck、117 前端单测、build、store/app/importer/library 回归、store/app race。
- 8.2–8.4：新增问题/租约/动作双浏览器测试 20 项、候选/准入/封面/实时事件等合计 82 项；旧复核动作 38 项全部通过。修复本地刷新失败被队列覆盖及租约重开事件竞态。
- 8.5：计数、截图、控件与实时更新双浏览器 30 项通过，保存跨批问题截图。日志回归恢复失败手动重试说明后，全量 Go 与五包 race 当时通过（最慢约 223 秒）。
- 最终前端 typecheck、**24 文件 / 120 单测**、build 通过。主 chunk 约 **1,065 kB**，仍有 >500 kB 警告。
- strict 规格与 diff 检查通过；9.6 的全代码一致性核对尚未完成。

## 最终全量运行失败（未掩盖）

### Go

执行 `go test -json ./...`：1796 个 test/subtest 通过，`internal/covers.TestCancelOutOfView` 失败：

```text
TempDir RemoveAll cleanup: unlinkat .../TestCancelOutOfView.../001/tmp: directory not empty
```

检查发现该测试启动 `go pool.Fetch(...)`，测试退出前释放 HTTP handler，但没有等待 visible Fetch 写盘结束。尚未修改/复验该封面测试；不声称已排除生产问题。

后续 `&& go test -json -race ...` **未执行**。第八组末的 race 通过不能代替加入 9.1/9.2 后的最终运行。日志在 `/tmp/sonexa-go-final.json`（临时诊断，不作为持久交付依赖）。

### Playwright

执行构建预览模式的全量 Playwright：**314/320 通过，6 失败**。

- 本次新增来源 Select 导致旧 `list-edges` 与 `provider-capabilities` 各双浏览器定位不唯一。已改按“上游/地区”可访问名称定位，相关两文件 **14/14** 重跑通过；尚未重跑完整 320 项，不报告全量已绿。
- `cover-grid` 双浏览器失败：原断言 `/variantId=32$/`（另有 11/12）不允许 URL 后带 `&searchId=...`，而当前 CoverPreview 为搜索会话携带该参数。本轮未改该封面模块/测试；须确认是否纳入修复。不能删掉 searchId 掩盖断言。

日志在 `/tmp/sonexa-e2e-final.log`；trace/截图在忽略目录 `frontend/test-results/`，后续重跑可能覆盖。

## 验证边界与后续

- 不运行原生 Wails 人工验收或真实网络；浏览器用 Wails 替身，Go 用临时 SQLite/httptest/固定事实。未清理、迁移或读取用户应用数据。
- 数据代际仍 `track-identity-v9`；未同步主规格、未归档。
- 下一步需用户确认是否修复两处封面测试基础设施/断言，再执行 D10 汇总、全量 Go、五包 race、完整浏览套件及一致性核对。9.3–9.6 保持未完成。
