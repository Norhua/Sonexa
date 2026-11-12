# 4 — 执行计划与验证

> 状态：阶段 0、A、B、C、D 与收尾全部完成（2026-11-13）。逐项结果见下；没有执行的验证在“验证记录”里单列，没有标成通过。
>
> 依据：[4-why.md](4-why.md)（问题、证据、用户选择）与 [4-design.md](4-design.md)（实施设计、判定表、测试矩阵）。最终行为见 [spec-diff.md](spec-diff.md) 的“修改 4”。4-design.md 第 7 节的实现推论没有经用户逐条确认，落地情况见本文件“实现推论的落地情况”。

## 执行计划

### 阶段 0：排查、记录与决策

- [x] 4.1 按用户要求只读排查匹配状态机、同发行/同发行组判定、发行与发行组合并三个问题，以代码而非文档为依据。
- [x] 4.2 在仓库外临时副本用测试核实关键判断；源码与输出保存到忽略目录，工作区代码未改动。
- [x] 4.3 分析“曲目集合相同即同一发行”的定义变更，建立 `4-why.md` 与本文件。
- [x] 4.4 用户复核并答复 D1–D11；回填 `4-why.md`，新增 `4-design.md`，按答复修订本计划。未改 `spec-diff.md`、未提交。
- [x] 4.4a 执行前复核：把 4-design.md 的规则逐条对照导入状态机与发行组/发行/曲目的实际代码，发现 7 处漏洞并修订文档（4-why.md“执行前复核”，4-design.md 2.1、2.1.1、2.2、2.3、2.4、3.3、4.2、第 6 节、第 7 节的 12–17）。

### 阶段 A：状态机低风险修复（不依赖发行定义）

- [x] 4.5 P3：`ResolveImport` 的佐证集合改为自动候选与选中的候选。回归 `TestOwnedCorroborationIgnoresPoolSize`（池内 1 条与 2 条都跳过）。
- [x] 4.6 P2：带确认的准入提交成功后重排同发行/同目标组、未手搜的复核条目，应用层为受影响批次启动驱动器。回归 `TestConfirmedReleaseRequeuesSiblings`（确认 1 首后其余 2 首免确认入库，无关条目不动）。输入确认的提交同样触发重排。
- [x] 4.7 P1：检索阶梯只在候选池里有可自动入库的候选时停止推进。回归 `TestLadderContinuesPastReviewOnlyPool`；既有 `version_review_test.go` 里“只有仅供人工查看的候选”一例的查询次数由 1 变为 2，其余不变。
- [x] 4.8 P6：手搜过的复核条目仍按原始输入与当前库事实离线收敛，不读写手搜草稿。回归 `TestManualSearchItemStillReconcilesOffline`。
- [x] 4.9 P10：删除 `release_group_match_key` 及其全量重建与清理语句，更新受影响的测试。**验收只达成一半**：写侧的全量重建已去掉，但单次准入耗时仍随库规模近似线性增长（判组每次读取全库名称证据），实测与结论见“验证记录”。
- [x] 4.10 P11：`plan_stale` 重排加退避与连续上限（1s、2s、3s，第 4 次转普通失败）；`GroupByReleaseGroup` 的键加入来源；移除 `Engine.Run` 未使用的参数。

### 阶段 B：置信度（需重跑既有金样例）

- [x] 4.11 P4：`AutomaticCandidates` 要求曲名精确。回归 `TestAutomaticCandidatesRequireExactTitle`、`TestFuzzySiblingIsNotAutoImported`。受影响的既有测试只有手工构造评分的夹具（`internal/library/selection_test.go` 与 `internal/app/review_release_test.go` 里只写了“高置信”、没有写曲名匹配度），已补上“曲名精确”；经真实评分的 CSV 验收金样例（`internal/acceptance`）预期没有变化。

### 阶段 C：判组（D11）

- [x] 4.12 同来源原生组否决，输出已否决证据。回归 `TestSameNameDifferentNativeGroupStaysSeparate`。
- [x] 4.13 判组否定决定（按组对保存、以锚定组为条件过滤）；“保持独立成组”“加入其中一组且不合并”；组合并改为只在计划带合并目标时执行。回归 `TestCrossSourceSameNameConflictActions`、`TestKeepSeparateCreatesIndependentGroup`、`TestExplicitMergeClearsSeparationDecisions`；编辑器保存路径见 `TestEditorSaveSupportsKeepSeparate`。
- [x] 4.14 P8：名称近似时在弹窗与复核证据里给出提示，不改变判组结果。

### 阶段 D：发行新定义（D1–D10）

- [x] 4.15 歌曲身份：`matching.CompareSong` 与表驱动测试 `TestCompareSongRecordingLevelWins`。
- [x] 4.16 存储：准入基线的覆盖结论与两个闸门（`internal/store/coverage.go`），三张新表，模型代际 `streaming-playlists-v3`。提交事务内重算并校验。
- [x] 4.17 自动路径：`library.SelectCandidate` 增加组内歌曲解析（`internal/library/group_song.go`），新增复核原因 `song_unresolved`，多个已有发行的排序把曲目引用数放在总曲数之前。
- [x] 4.17a 替换后的收尾：`store.ResolveProvidedImport` 的两段证明，补全、已解析身份、日志与证据改用替换后的身份。
- [x] 4.18 服务与绑定：`ConfirmTypedAdmission`（服务端校验 `Yes`，独立用途凭证）、`UseSongProvider`、`RefreshAdmissionCoverage`；`CommitAdmission` 与 `CommitEdit` 拒绝缺少输入确认凭证的提交；编辑器迁移时种子携带歌曲事实；决定留痕与并存记录（含 `unknown` 情形）。
- [x] 4.19 前端：确认弹窗的提供者详情、“使用发行 A / 创建发行 B”、输入 `Yes`、不可判定时自动获取清单、判组三种动作；复核原因文案；亮/暗主题与窄窗口检查（自动化检查元素不越界、无横向溢出，并人工查看了截图）。
- [x] 4.20 浏览与清单：复核了专辑视图的文案。现有文案只有“相同歌曲优先显示收藏，拆分后可查看各版本”，在新定义下仍然准确，**没有改动**；代码里不存在设计里提到的“其他版本已收藏”这类灰色行提示。灰色行加入曲库时由确认弹窗提示。
- [x] 4.21 归并提议：清单完整后生成“发行曲目相同”提议，排除有并存记录的一对，提供列表与忽略（`internal/store/release_merge.go`，归组提议面板）。
- [x] 4.22 按 4-design.md 第 6 节的测试矩阵补齐后端场景测试与前端回归，逐行结果见“测试矩阵结果”。
- [x] 4.23 评估“执行归并”，结论见“执行归并的影响评估”：需要另立修改记录，本次不实现。
- [x] 4.24 D1 说明已写入 `spec-diff.md` 修改 4 的 `matching/pipeline` 条目、`matching.CompareSong` 与 `library.resolveGroupSong` 的注释、internal/importer/README.md。

### 收尾

- [x] 4.25 回归，结果见“验证记录”。
- [x] 4.26 `spec-diff.md` 增加“修改 4”，更新 internal/importer/README.md；以中文约定式提交，排除非本次的 go.mod、AGENTS.md 改动。

## 依赖与建议顺序

- 阶段 A、B 不依赖其他阶段，可以先做并单独提交。
- 4.17 依赖 4.5：解析到的曲目若已拥有，要靠单候选佐证才能直接跳过。
- 4.7 与 4.11 相互影响：自动候选要求曲名精确后，P1 判断“可自动入库的候选”的口径随之变化，建议相邻完成并一起回归。
- 4.16 是 4.17–4.21 的前置；4.18 是 4.19 的前置。
- 阶段 C 先于阶段 D 的前端部分（4.19 的弹窗同时承载判组动作）。4.12 生效后同名而独立的组会并存，4.13 必须在同一批交付，否则没有原生组标识的候选只能合并这些组。
- 4.6 在 4.17 之后触发面会缩小，但仍然需要。

## 不在本轮计划内

- P5（总数未知时的兜底）：实施 4.17 后只有首次建组才比较总数，收益很小，暂不实施。
- P9（跨来源身份桥）与跨来源自动解析（D9）。
- 发行别名映射（D8）、把已有曲目改绑到其他发行、旧数据迁移。
- 文本级相同用于自动决策与多来源自动导入：只按 4.24 留下说明，届时另立修改记录。

## 实现推论的落地情况

4-design.md 第 7 节的推论都没有经用户逐条确认。除下列两点外均按原文实现：

- 推论 5（交互预览遇到 `unknown` 时自动获取一次清单）：按原文实现，由前端在弹窗打开时触发一次，服务端不在预览里隐式发请求。
- 设计 5.1 的说明文字“发行 B 与发行 A 曲目完全相同 / 部分相同”**没有实现**：预览时候选所在发行通常还没有清单，给不出结论。

执行中另外做出、设计里没有写的决定：

1. 编辑器迁移时，被编辑的曲目与同一次保存里一起迁走的曲目不计入原发行的曲目引用，也不作为本地发行的清单条目参与比较。否则这首歌永远“能由原发行提供”，而一个迁走后就变空的原发行会把结论拖成不可判定。
2. 候选所在发行已在库、而组内另一发行也含同一录音时，先补齐候选所在发行自己的清单再比较主发行，避免因为“一侧清单没加载、总曲数为零”而偏向另一侧。
3. “使用发行 A”在编辑器保存与整发行换组的计划上不提供（服务端拒绝，弹窗不显示按钮并说明）。
4. 提供者是没有上游身份的本地曲目时，“使用发行 A”通过种子里的复用曲目标识直接复用该曲目。
5. 精确曲目已在库的计划覆盖结论为 `not_applicable`：这次提交不会让任何歌进入发行。
6. 输入确认的提交同样触发 P2 的重排。

## 执行归并的影响评估（4.23，只评估）

“执行归并”指把发行 B 的曲目改绑到发行 A 的对应清单条目，然后清掉变空的 B。依据代码得出的影响：

- **曲目身份会被改写。** 曲目的上游身份是发行内的曲目标识，A、B 对同一录音的曲目标识不同。改绑要更新 `track.match_ref` 与 `track.release_id`，并用 A 的清单条目重建曲目快照与标识；编辑器保存里已有这套写法（`store/editor_save.go` 的迁移分支）。
- **已记住的输入确认关系会失效。** `import_confirmation.binding_key` 由曲目来源、曲目标识、发行 id、发行来源、发行标识、碟号、音轨号组成，`confirmationBindingCompatible` 要求前五项逐项相等。改绑后这些确认不再匹配，同一输入再次导入时不会被“已确认过”跳过，只能靠元数据重新判定。要保留就得在同一事务里重写 `binding_key` 与 `target_context`，而 `target_context` 记录的是当初用户批准时的专辑与位置，改写它等于替用户追认一个新的上下文。
- **位置会变。** A、B 的碟号、音轨号可能不同；`track_current` 的位置更新会触发 `invalidate_import_confirmation_position`，直接删除该曲目的输入确认。
- **封面采用关系按发行保存**（`release_cover_selection`），B 上用户采用的封面不会跟到 A；要么丢弃，要么需要用户选择保留哪一个。
- **歌词按曲目保存**，曲目行本身不变时歌词文件与采用关系保留；但上游歌词快照随曲目快照重建会被清掉。
- **同步歌单条目按曲目 id 引用**，曲目行保留则引用不受影响；若两侧都已拥有同一首歌（各一条曲目），则需要合并两条曲目，同步歌单条目、歌单成员、标签都要改指向，并处理 `ON DELETE RESTRICT`。
- **用户改过的字段**（`states_json` 里标为用户值的）在换绑后应保留，与编辑器迁移的口径一致。

结论：**需要另立修改记录**。它不是归并提议的一个按钮，而是一次带预览和逐曲确认的迁移，至少要决定四件事：两侧都有同一首歌时保留哪条曲目；输入确认是失效还是改写；封面取哪一侧；位置冲突怎么处理。本次只提供查看与忽略。

## 测试矩阵结果

对应 4-design.md 第 6 节，按行记录。全部为离线测试，结果均为通过。

| 场景 | 测试 |
|---|---|
| 组内 A 完整清单含该录音，候选在未入库的 B | `importer.TestGroupSongResolvesToExistingRelease` |
| 同上，A 的该曲目已拥有 | `importer.TestGroupSongOwnedOnExistingReleaseIsSkipped/input-describes-candidate` |
| A 清单缺失：取一次详情，第二首不再请求 | `importer.TestGroupSongResolvesToExistingRelease`（详情请求 1 次）；`library.TestAutomaticSelectionPrioritizesOwnedReleaseAndKnownTotal` |
| A 清单取回仍不完整 / 请求失败 | `importer.TestGroupSongTracklistUnavailable` 两个子用例 |
| 同一录音在 A 中两个位置 | `importer.TestGroupSongDuplicatePositions` |
| 全部发行录音级可判定且不含该录音，原生组已映射 | `importer.TestGroupSongUniqueToCandidateRelease`；`library.TestCoverageConclusions` |
| 同上但组只经名称证据命中 | `importer.TestConfirmedReleaseRequeuesSiblings`（原因 `new_release`）；`library.TestCoverageConclusions` |
| 组内有其他来源的发行，文本级命中 / 未命中 | `library.TestCrossSourceReleaseIsTextLevel`（基线层；Runner 只查询单一来源，没有 Runner 层用例） |
| 组内有本地发行，其曲目文本级命中 | `library.TestLocalReleaseParticipatesAtTextLevel`（交互路径与复用）；自动路径由基线的 `possible` 统一导向 `song_unresolved`，没有单独的 Runner 用例 |
| B 已在库、A 引用更多且清单含该录音 | `importer.TestGroupSongPrefersReleaseWithMoreReferences` |
| 两个已有发行都含该录音，目标是 / 不是主发行 | `library.TestPrimaryReleaseOrderingGate` |
| 交互选择 B，`provided`：各种凭证与确认词 | `library.TestTypedConfirmationGate`；`app.TestAppAdmissionCannotBypassExtraReleaseConfirmation` |
| 交互“使用发行 A” | `library.TestUseSongProvider` |
| 计划生成后 A 的清单变化 | `library.TestCoverageChangeInvalidatesPlan` |
| 编辑器新建/改绑发行触发同样的闸门 | `library.TestEditorMigrationIsGated` |
| 清单后到，两发行录音多重集相同 | `library.TestReleaseMergeProposals` |
| 同名、同来源原生组不同 | `library.TestSameNameDifferentNativeGroupStaysSeparate`（基线层：不询问、独立成组；没有 Runner 层用例） |
| 同名、跨来源；命中两组；显式合并后清除否定决定 | `library.TestCrossSourceSameNameConflictActions`、`TestKeepSeparateCreatesIndependentGroup`、`TestExplicitMergeClearsSeparationDecisions` |
| 替换到 A 且已拥有，输入写的是 B 的专辑与位置 | `importer.TestGroupSongOwnedOnExistingReleaseIsSkipped/input-describes-candidate` |
| 同上，但输入与候选对不上 | 同一测试的 `input-cannot-prove-candidate` |
| 替换后触发补全 | `importer.TestGroupSongPrefersReleaseWithMoreReferences`（断言补全收到的是 A 的发行标识；没有走真实的补全事务） |
| B 已在库、组内另一发行清单未加载 | `importer.TestExistingCandidateReleaseFetchesSiblingTracklist` |
| 组内另有没有曲目引用的空发行 | `importer.TestEmptyReleaseDoesNotBlockGroup` |
| 编辑器把曲目改到组内另一发行 / 只改字段 | `library.TestEditorMigrationIsGated` |
| 归组提议整发行换组 | `library.TestGroupProposalMoveIsNotGated` |
| `unknown` 下输入 `Yes` 创建 B，之后清单到达 | `library.TestUnknownCoverageRefreshAndCoexistence` |
| “保持独立”后向另一同名组的已有发行加歌、刷新其快照 | `library.TestCrossSourceSameNameConflictActions`（以另一组的原生组为锚重新判组，只命中自己的组）；“刷新快照不再生成归组提议”经同一判组函数，没有单独的用例 |
| P1–P4、P6 | `matching/change4_test.go`、`importer/change4_test.go`、`store/change4_test.go` |
| P10 | 既有测试通过；耗时见“验证记录” |

前端：`frontend/tests/unit/admission-plan.test.ts`（弹窗规则与 store，15 项）；`frontend/tests/e2e/admission-coverage.spec.ts`（复核接受、搜索加入、编辑器保存三条路径的输入确认，疑似本地提供者，不可判定时的自动获取与重试，判组三动作，归并提议的查看与忽略，亮/暗/窄窗口）。

## 修改前排查实验记录

环境：macOS 27.2.1 arm64，Go 1.26.10，代码基于提交 `35ada5e`。方法：把 go.mod、go.sum、internal/ 复制到仓库外临时目录，在副本里加入实验测试并运行；这些测试只打印观察结果，不含断言，结论取自输出。源码、复跑说明与完整输出在忽略目录 `tmp/_scratch/change-4-experiments/`（`README.md`、`run.log`）。

`go test -count=1 ./internal/matching/ ./internal/importer/ ./internal/store/ -run TestExp -v`：10 个实验函数全部运行完成。

| 对应问题 | 实验 | 观察到的结果 |
|---|---|---|
| P1 | `TestExpLadderStopsOnReviewOnly` | 输入 `雪（Yuki）`：查询 1 次，已尝试变体 `[雪]`，分流 `review_no_match`；候选池仅 1 条 ReviewOnly，自动候选 0。 |
| P2 | `TestExpNewReleaseInGroupReviewMultiplication` | 首次运行：导入 0 / 复核 3（均 `new_release`）。确认第 1 首后：导入 1 / 复核 2。执行离线整理后不变。其余两条重新排队重跑：导入 3 / 复核 0。 |
| P3 | `TestExpOwnedTrackCorroborationPool` | 候选池 2 条：复核 1（`ambiguous`）。候选池 1 条：跳过 1。输入与已拥有曲目相同，仅署名多 `feat. Guest`。 |
| P4 | `TestExpFuzzyTitleHighConfidence` | `Track 1`/`Track 2`、`Interlude`/`Interlude 2`、`Song II`/`Song III`、`Episode 1`/`Episode 2`：曲名模糊、75 分、高置信。`雪`/`雪 2`：拒绝。`Theme`/`Theme (Reprise)`：ReviewOnly。 |
| P4 | `TestExpFuzzyDilutesExact` | 精确与模糊候选并存时自动候选数为 2。 |
| P4 | `TestExpFuzzyTitleAutoImportsSibling` | 输入 `Interlude`、上游只有 `Interlude 2`：导入 1，错误候选已入库。`Track 1`/`Episode 1` 两例在桩件里因查询不是子短语而无结果，进 `no_match`。 |
| P11 | `TestExpGroupKeyIgnoresProvider` | 两个不同来源、组标识相同的候选被分为 1 组。 |
| P7 | `TestExpSameNameDistinctNativeGroups` | 同名专辑与单曲（原生组不同）：命中 1 组、需确认、新原生映射为真。把库内发行改名后仍命中，剩余证据类型为 `release-snapshot`。 |
| P8 等 | `TestExpCrossSourceGroupResolution` | (a) 跨来源同名同署名：命中 1 组、需确认。(b) `BEST（豪华版）`：命中 0 组、无需确认。(c) 第三个同名：命中 2 组、冲突。(d) 署名多 `feat. X`：命中 0 组。(e) 全角与空白差异：仍命中。(f) 无任何证据：资料不足。 |

仅凭代码阅读、未做实验的结论：P5、P6、P9、P10、P12，以及 P11 中除分组键以外的各项。

## 验证记录

环境：macOS 27.2.0 arm64，Go 1.26.10，Node 24.21.0，Playwright 1.63.0。2026-11-13 在提交 `fd1fd76` 的代码上执行。

已执行并通过：

- `go vet ./...`：无输出。
- `go test ./...`：24 个包全部通过。
- `go test -race ./internal/store/ ./internal/library/ ./internal/importer/ ./internal/matching/ ./internal/app/`：通过。
- 前端 `vue-tsc --noEmit`、`npm run build`：通过（构建只有既有的分块体积提示）。
- 前端 `vitest run`：22 个文件、126 项通过。
- 前端 `playwright test`（Chromium + WebKit，桩件后端）：334 项通过。
- `openspec validate add-streaming-playlist-sync --strict`：通过。
- 前端绑定用 `wails generate module`（v2.10.2）重新生成；生成物在忽略目录，不入库。

P10 的耗时实测（本机合成数据，每个发行各自成组、各一首歌；20 次取平均）：

| 库内发行数 | 判组 | 预览基线 | 一次预览 + 提交 |
|---|---|---|---|
| 200 | 1.2 ms | 1.2 ms | 4.3 ms |
| 2000 | 12.8 ms | 12.2 ms | 28.9 ms |

库扩大 10 倍，单次准入耗时约为 6.9 倍，**仍随库规模近似线性增长**。耗时集中在判组：`groupNameEvidenceTx` 每次读取全库的名称证据（预览一次、提交事务内重算一次）。本次只去掉了写侧每次准入的全量重建，没有处理读侧；4.9 原定的验收“耗时不随库规模线性增长”没有达成。测量脚本保存在忽略目录 `tmp/_scratch/change-4-experiments/after/`，没有作为测试入库（它是量级检查，放进测试集会不稳定）。要解决需要给名称证据建索引或物化表，属于新的设计，建议另立修改记录。

没有执行：

- 没有调用真实上游；补取清单、详情请求全部由测试桩提供。
- 没有读写用户的应用数据库。
- 没有在原生 WKWebView 里人工验收，没有在 Windows/Linux 上构建或运行。浏览器自动化不能代替原生验收。
- 没有执行完整构建入口（`build.sh`）。
- 设计第 6 节里“前端：弹窗各分支的单测”按规则函数与 store 实现，没有写组件挂载级的单测；组件行为由浏览器用例覆盖。

修改前在设计里列为“依据代码阅读、需用测试确认”的四点判断，结果如下：

- 发行摘要加载即整张发行详情，补取已有发行的清单只多一次请求：测试桩层面成立（每个发行一次）；真实上游未验证。
- 灰色行准入的构造可以复用于提供者替换：成立，已抽成共用函数。
- 覆盖结论可以完全由已保存的清单与计划内事实在提交事务里重算：成立；编辑器路径需要种子额外携带歌曲事实。
- 所有交互入口共用同一确认弹窗，编辑器保存走同一套基线校验：成立。
