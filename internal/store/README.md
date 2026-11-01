# 队列与复核问题存储

## 新库边界

当前空库初始化代际为 `track-identity-v9`，新增任务变更修订。此前 `track-identity-v8` 没有任务事件修订表，v7 没有网络待启动提示，v6 没有问题影响索引，v5 没有问题租约和在途保护，v4 没有本地匹配索引，v3 仍允许旧合并终态，v2 只有输入 profile；这些代际、`upstream-snapshot-v1` 及没有代际标记的旧业务库均明确拒绝。程序不转换历史输入、不修复旧收藏身份、不自动删除用户数据。同代际重开不重复初始化，高于本程序支持的 schema 版本仍拒绝。

收藏继续由 `track(match_source, match_ref)` 的部分唯一索引约束；Recording、ISRC 不是唯一键。终态的本地目标 ID 和来源身份属于当次处理诊断，没有指向收藏的外键，不会因删除/换绑抹掉历史结果，也不能作为新输入的选版映射。

## 原始输入

`import_item` 保留批次、行号、完整 `raw_row` 和解析列。数据库触发器拒绝改写这些原始字段；复核查询必须另外保存。`input_profile` 保存版本化 `matching.InputProfile`，含原文、限定、解析精度与失败状态；派生 profile 可重建，不取代原始行。

- `revision`：从 1 开始的正整数，队列状态、失败/重试、候选更新和准入结束时递增。`updated_at` 仅供展示，不作修订令牌。准入 baseline 读取该整数。
- `review_reason / review_evidence`：当前行的待处理原因与证据，仍保留既有逐行读取协议。
- `resolution_reason`：与状态分离，区分新建、无需匹配、精确已有、用户跳过及移出。
- `track_id / resolution_target`：本地 ID 与当次核验的来源 track/release 身份。
- `resolution_problem_id / resolution_problem_revision`：成对保存当次问题范围的诊断修订，不引用未来的活跃问题。

## 活跃问题基础结构

| 表 | 内容 |
|---|---|
| `review_problem` | 稳定 ID、active/closed、正整数 revision、规则版本、语义锚点和等价证明 JSON、查询与候选事实及各自修订、时间 |
| `review_problem_item` | 问题与来源行、是否活跃、成员修订、采用的输入修订与证明 JSON、时间 |

`review_problem_item(item_id) WHERE active=1` 是部分唯一索引，一行最多一个活跃归属。插入或更新活跃成员须引用当前仍未终结的行修订和 active 问题；关闭问题/结束行前必须先解除活跃成员关系。历史成员可保留但不占用活跃归属；批次删除级联移除其行和成员，不级联删除其他批次原文或共享问题。

整数列校验类型及正值，profile、目标、锚点、证明、查询和候选事实列校验 JSON。JSON 可解析不代表证明成立；语义证明仍由消费业务判断，不能由数据库唯一索引替代。

**已接入的生命周期：** `MarkReviewAtRevision` / `MarkReviewWithProblem` 和最终 `RecordFailure` 在行状态事务内创建或加入问题；只对当前原始 profile/事实重新证明，不信任 blocking key。SQLite immediate 写事务和单活跃成员约束避免跨批并发创建重复问题。每行仍保留独立状态、失败和候选，暂停批次不取消其他成员。`ItemProfile` 与 Runner 共用明确单位的 profile 构造。

重试、Claim、重启恢复同步成员输入修订；终态提交先解除对应成员并保存问题诊断范围，剩余成员重新构造 anchor，最后一个成员完成才关闭问题。所有步骤与行结果/准入同事务，失败整体回滚；关闭问题不供未来输入复用。重新证明时核验本地事实绑定修订，旧事实不产生精确身份证明。

**来源行拆出：** `SplitProblemItem(problemID, problemRevision, itemID, inputRevision)` 在事务内校验当前范围，将该行移入 `anchor.isolated` 的独立活跃问题。原问题 revision 和被拆行 revision 递增，其他行原文、候选和输入修订不变。旧单行准入批准因此失效；独立问题重试也不自动合回，不吸纳后续等价输入。隔离仅保存于活跃工作锚点，不作为未来输入的历史身份规则。

**阶段边界：** 5.1–5.4 已交付证明、检索、创建/加入/关闭、拆出、版本化问题操作和生产 Runner 接线。6.1–6.4 已加入单行/多行计划、原子结果、显式 API 和后端交互保护；自动重评和问题前端仍待后续任务。前端暂时仍逐行读取，不能把后台成员归并称为已交付多来源问题 UI。候选合并 API、旧本地候选快照与 merged 终态已移除，状态 CHECK 拒绝旧写法。

## 问题查询与范围协议（5.4）

`ProblemScope(version=1)` 携带问题 ID/revision、查询/候选修订以及稳定排序的全部活跃成员 ID、成员修订和输入修订。`BeginProblemQuery` 在出网前校验并递增查询修订、保存 `ReviewQuery(version=1)`，完成时校验完整 token；成员新增/拆出、其他查询、终态或无一匹配使迟到结果失效。查询只影响当前显式范围，不改原文。

`ReviewCandidateFacts(version=1)` 统一保存 `ScoredCandidate` 来源事实及变体，不保存 CandidateRow 的所有权、动作或本地封面 ID。问题的 `ProblemCandidateFacts(version=1)` 按成员分别保存事实，不能拼接不同来源行。Runner 与人工问题改搜使用同一形状；旧内部 scored fixture 可投影到来源形状，旧 UI 候选快照只在 App 兼容读取，不成为问题身份证明。

`NoneMatchedProblem` 清候选但保留活跃成员，失效在途查询；`RetryProblemMembers` 只处理范围中明确列出的 failed 成员；`DiscardProblemMembers` 仅原子结束列明成员。空/重复/非成员 ID、旧问题/成员/输入修订均拒绝。App 提供对应显式问题接口及拆出入口；旧单行改搜/无一匹配遇多行问题时要求先拆出，不能静默扩大范围。完整问题 UI 将在 8.x 接线。

## 准入输入范围（6.1）

`AdmissionSeed/Baseline.InputScope(version=1)` 统一单行与问题操作。`single` 只列选定一行，即使该行属于多行问题也不扩大操作范围；`problem` 必须列出全部当前活跃成员，稳定按行 ID 排序，子集、重复、乱序及空范围均拒绝。搜索/编辑没有导入范围；旧 `ImportItemID/ItemRevision` 仅是服务端单行适配，不能与 scope 不一致。

范围绑定问题/查询/候选修订、完整成员 ID/成员修订、输入修订/状态、批次/行号、profile/证明规则版本及证据摘要。证明改动共用问题/成员 revision；摘要同时涵盖保存的 anchor/proof、各行原始 profile 和候选，以及重新核验本地事实后产生的证明。不新增数据库代际。证明 JSON 可解析不授予权限，多行准备与提交必须重新证明全部成员等价；本地删除/换绑或事实修订即使未改队列，也不能沿用旧精确证明。

`ReadProblemInputScope` 仅将已展示且仍有效的 `ProblemScope` 转为准入范围。准备候选在 I/O 前校验并保留该范围，必要身份解析按不同位置要求复用，同一范围内所有行必须解析到同一真实位置；准备结束、确认和提交再次校验。单行计划也绑定所在完整问题的证据摘要，因此其他成员加入/拆出会失效旧批准，但绝不把其他行加入单行操作。`AdmissionHash` 和服务端签名涵盖 seed/baseline 的完整范围与选定目标，客户端重算 Hash 不能获得批准。

## 单行接受与精确跳过

- `library.PrepareCandidate` 在必要资料加载前记录输入修订，计划绑定本次候选的来源 track/release；提交先检查输入仍活跃且修订未变，再进入任何收藏写分支。
- `PrepareSkipExisting` 只接受来源已验证的精确候选、输入 ID/revision。准备与提交都不出网，不做成员新增确认；计划绑定本地 ID、track 与 release 修订，目标删除、换绑（含换回）或发行关系变化时拒绝。
- `CommitAdmission` 统一按实际结果记终态：新建为 imported，已存在为 `skipped/existing_track`。不接受客户端指定终态，不以同 ISRC/Recording 复用其他 track。已有目标只写输入结果及 `operation_result`，不修改任何收藏表、封面、歌词或补全状态。
- 同一成功 plan ID/hash 重试返回原结果；另一旧计划、旧改搜/失败响应不能恢复终态。Runner 与在途改搜使用起始整数修订 CAS，旧同步队列写接口也始终拒绝终态。
- App 与 Runner 对纯已有结果不发收藏变更通知、不启动补全；前端单行跳过已接入计划，独立任务/问题通知仍待后续任务，不把这一步称为完整事件交付。

## 原子结果与单行辅助操作（6.2–6.3）

`CommitAdmission` 在任何实体写入及已有目标快速返回前，重验整个 seed/baseline 输入范围。新目标只新增一个 track，按最小成员 ID 稳定指定唯一 `imported` 行，其他成员记 `skipped/existing_track`；目标已存在则全范围跳过。`AdmissionResult` 返回来源 track/release、实际目标及逐行/批次结果，`operation_result` 保存同一完整结果用于幂等，不能用于未来输入的身份判断。

多行结果、成员移除、问题关闭及归组决策均在同一事务，任一成员/结果写入失败整体回滚。未按既有文件暂存协议准备的文件字段仍拒绝，不留下暂建实体或终态。新增发行的确认规则不变；另一请求已收藏同一精确目标时只跳过、不修改旧收藏、不要求已无必要的新增确认。

App 提供 `GetReviewAdmissionScope`、`PrepareProblemAdmission` 和 `PrepareProblemSkipExisting`。`PrepareNoMatchNeeded` 先记录所选行范围，再读取原始解析值，不借问题查询草稿替换原文，不给其他行自动创建或跳过。多行无绑定 seed 被拒绝。移出支持单行/显式成员列表，失败重试仅列明 failed 成员；不将单行操作推广到其他成员。任务/问题事件及完整问题 UI 仍由 7.x/8.x 交付。

## 问题交互租约（6.4）

`AcquireProblemLease` 校验显示的完整 `ProblemScope`，发放 90 秒随机能力令牌并递增问题/查询修订。租约不修改原文、候选或音乐身份；续约只延长服务端期限，不轮询队列、不递增事实修订。旧令牌不能续约、释放或操作新会话。正在 matching 的已有成员在获取租约时退回复核、递增输入修订，淘汰旧后台响应，再返回可供编辑的稳定 scope。

- `UpdateProblemDraft` 保存独立查询并递增查询修订，旧搜索/批准失效，但不清空候选、不改原文。客户端必须携带租约与当前范围；新增成员仍使旧范围失效，不扩大确认。
- 改搜及候选解析在出网前创建独立在途 pin，网络期间不持有数据库事务或全局锁。pin 最长 10 分钟，慢请求超过 90 秒仍保护问题；明确释放会立即拒绝旧响应，但 pin 在请求返回或硬截止前继续阻止后台处理。正常完成、出错及超时均可清理。
- 自动计划的 `Automatic` 随计划签名保存；事务内在任何写入/已有目标快速返回前检查保护。当前 lease 或 pin 存在则返回 `problem_busy`，不新增、不结束问题、不改变补全。人工 lease 持有人仍可确认/提交，释放或过期后的旧批准拒绝；同一已成功计划仍返回原幂等结果。
- Runner 不领取受保护成员；已有后台工作遇保护则暂缓，不增加网络失败次数、不覆盖人工候选。自动决策在 I/O 前固定 InputScope，不能在交互结束后给旧证据刷新授权。底层旧队列状态写也拒绝受保护问题；显式问题移出、重试、拆出校验完整范围和持有人。
- 释放、在途结束和回收只向 `review_problem_recheck` 合并写入问题 ID/revision，不保存目标选择。App 生命周期启动每 15 秒、每次最多 100 个问题的本地回收；关闭前取消并等待。重启后剩余租约按原期限过期，迟到请求无权复用。实际消费提示并进行当前证据重评由 7.1–7.2 接入，不把此提示表称为已实现后台自动跳过。

前端使用 `Acquire/Renew/ReleaseReviewProblemLease`、`UpdateReviewProblemDraft`，从返回 scope 获取准入范围；完整聚焦/卸载/确认交互在 8.4 接线。本阶段不宣称 UI 租约已交付。假时钟与并发/真实 App、Runner 测试覆盖编辑/批准保护、续约不改事实、释放在途、硬截止、重启、CAS/回滚、旧响应及释放后新决策。

## 收藏变更驱动重评（7.1）

`review_problem_key` 是活跃输入/候选/来源事实的反向影响索引，使用与本地召回一致的规范化名称、截断前缀及来源 track/release 身份。问题候选重建时同步索引，关闭时清理；命中只表示需要重评，不是跳过证明。

收藏新增、删除、换绑、当前值/快照/署名关系及组事实变更，同事务写 `review_library_dirty`。变更前的旧名称、关系和来源身份临时保存在 `review_library_before`，因此删除与连续换绑不丢失影响范围；成功排入后消费清理，不用于输入选版。App 成功收藏通知唤醒单个后台 worker；重复唤醒合并，无事实改动的 UI 通知不重评整库。

每次最多处理 32 个变更 track、每 track 100 个问题（游标续页），重评每次最多 16 个成员。成员始终单独使用原始 profile 与当前收藏事实；已保存的来源决策事实、完整性不足与位置歧义不得被展示候选代替。复用既有 `CandidateFacts`、输入筛选、同组同内容偏好与精确所有权逻辑，重评本身不联网、不新增收藏，只对重新确定的已有目标准备 skip。无结果但本地完整资料充分的输入可跳过；跨组、显式其他版本、未知竞争者及部分清单保留待处理。

输入 scope、租约和收藏事实代次在提交事务内重验；不仅删除目标，新增或修改竞争者也使旧计划失效。纯 skip 不发 library 通知，不写收藏事实，从而不自激循环。提示消费使用 revision CAS；即使重评无操作，成员/候选或竞争事实在途变化也不会误消费提示。错误保存于提示并延后一分钟，不改来源行错误。任务/问题 UI 事件仍在 7.4。

### 重评恢复与必要网络工作（7.2）

释放租约、结束在途 pin 和超时回收在事务提交后发送非阻塞唤醒；worker 的容量一信号合并重复事件，数据库提示保存实际工作。启动恢复活跃问题并重置遍历游标，不读取历史终态映射；重评与停止使用生命周期 context，等待 worker 退出后关闭存储。无待办时不轮询复核页；只按错误重试/租约/pin 的下一截止时间设置后台定时器。

需要更多来源资料时，重评不直接请求 provider，而用相同 scope/事实 CAS 把列明 review 行排回既有 Runner。`review_problem_network` 保留尚待启动的批次提示，出网门禁关闭时不启动，设置恢复或启动恢复再续跑；请求仍使用既有共享配额/重试及动态门禁。failed 行维持手动重试政策，暂停批次不自动恢复，不触及其他行的原始值、错误或终态。普通歧义/明确新版本不排网，仅实际判定需资料的路径排队。

纯 skip 不发送 library 通知、不修改收藏事实代次；无操作的提示消费也不自发新建重评。App 读取复核页只读本地状态，不会启动匹配。测试覆盖释放续评、保留失败、no-op CAS、终态不恢复、重启、门禁关闭/恢复及实际 Runner 调用。

## 行数与独立问题数（7.3）

`BatchStats` 的 Total/Queued/Matching/Imported/Review/Failed/Skipped/Discarded 以原始入队行为单位，互斥状态之和等于 Total。`Unmatched` 是 Review 子集，`ExistingSkipped` 是 Skipped 子集，不再次相加；Processed 仍包括 review/failed，表示本轮已处理而非全部处置完。Merged 统计及新写路径已移除。

`ProblemStats` 分别返回 ReviewProblems/PendingProblems/FailedProblems/SourceRows。活跃成员全部 failed 才归失败问题，含 review/queued/matching 成员的归待选；SourceRows 是活跃来源行数。批次统计只计与该批有关的问题和来源行，但问题分项按完整跨批范围判定，因此全局问题数不能由各批相加。行数、子集和批次关联问题在同一只读事务读取。

TaskSummary.ReviewPending 现在为全局独立问题数，并返回完整 Problems；`GetReviewProblemStats` 提供无候选加载的本地统计。旧 GetReviewQueue 仍返回逐行列表和行计数，8.x 将替换界面，不能将其旧 Total 混作问题数。

## 任务与问题事件（7.4）

`task_change_state.revision` 在批次、输入、问题及成员关系更新的同一事务递增。Store.withTx 仅在成功提交且修订实际变化后调用通知；App 发出 `review:changed` 与 `tasks:changed`，载荷含 revision。不要求计数变化：改搜、草稿、拆出、成员变化、失败重试及纯跳过都可刷新；回滚、只读、同一成功计划重试不伪报变更。新修订供 8.x 前端丢弃迟到响应，不把问题行数混为收藏变更。

`library:changed` 继续只由真实收藏操作发送；纯 skip 和后台已有目标重评只发 review/tasks，不启动补全、不反馈成新的收藏变更事件。App 初始化装配唯一回调，Runner 和所有事务准入路径共用。通知发生在数据库提交后，回调可立即读取完整终态。既有 import:progress 继续反映运行集合及批次进度，不由数据库提前宣称 Runner 已结束。

## 本地匹配召回

`Store.RecallLocal(ctx, profiles, limit)` 实现 matching 声明的事实召回接口。一次最多 128 个输入，返回顺序与输入一致；每个输入默认 100、最多 1000 个目标，读取 limit+1 判断 `Truncated`。它不调用有 2000 条上限的浏览接口、不出网、不读历史导入或选版记录。空输入不触发重建，查询错误/取消返回错误而非空结果。

- `local_match_key` 按曲目、文本字段、事实出处保存规范化名称/异写键及完整文本 profile、规则版本；版本和未知注释留在 profile 中，宽松召回键不是身份或等价证明。
- `local_match_dirty` 由同事务触发器记录当前值、有效快照、绑定、署名/角色及一跳艺术家关系变化。关系迁移同时标记 OLD/NEW 归属，删除艺术家先标记再级联移除关系。插入标记前排除已有行，不依赖可能被外层 UPSERT 冲突策略覆盖的 `INSERT OR IGNORE`。
- 读取前重建失效项；规则版本变化或 `RebuildLocalMatch` 显式调用会全量重建。重建与读取在同一事务中，无中间状态；失败保留旧索引与 dirty 标记供重试。派生索引不是收藏事实，不发收藏变更事件。
- 通过名称/异写精确键和已声明截断的前缀范围召回，三字段取并集，保留竞争目标；批量键使用 JSON 表参数，不因 SQLite 表达式深度或占位符上限丢弃异写。
- 结果分别保留当前文本/位置/字段来源、当前署名和角色关系、有效 track/release 快照、完整性及各层修订。快照必须符合当前身份，track 快照明确声明的发行也须一致；旧绑定快照不提供索引正证据。清单只用当前 track 的精确位置召回，但返回完整有效发行清单供后续消歧。快照时长不填入不存在的“当前时长”字段。

`ReadLocalFacts(ctx, identities)` 另按已给出的精确来源 track 读取完整本地事实，匹配业务通过 `LocalFactLookup` 消费。它不依赖名称召回，去重输入身份、跨来源不合并；同事务读绑定及事实，仍不授予 owned。用于远程候选完全改名时补读当前收藏的证据，避免文本池漏掉事实冲突。

未绑定收藏仍可召回，但不会得到虚构的来源身份或 owned。即使未截断且只有一个本地候选，也不说明上游目标唯一；事实合一和纯本地充分事实跳过已由 4.2–4.3 接入；远程输入优先自动选择与统一生产接线仍由 4.4–4.6 完成。

`local_recall_test.go` 覆盖真实快照写入的嵌套 UPSERT、当前/来源事实分离、异写与限定保留、角色/关系及两端失效、删除/换绑、2000 条之后的目标、截断、大量异写键、规则重建、重启、错误回滚、取消与并发同一事务读。新库拒绝测试包含 v4，不修改旧数据。

## 验证

`queue_schema_test.go` 使用临时文件 SQLite 验证整数修订、原始行不变、JSON/正整数约束、跨批成员重启保留、并发单活跃归属、终态/旧修订拒绝归入、历史目标保留、同 ISRC/Recording 多 track 共存及旧代际无改写拒绝。`problems_test.go` 和 importer 的真实 Runner 用例验证跨批等价归并、独立失败/原文保留、单行结束/最终关闭、重启/重试修订、非传递别名拒绝与事务回滚；`library/input_scope_test.go`、`library/multi_input_test.go`、`store/input_scope_test.go` 及 `app/multi_input_test.go` 覆盖范围 Hash/签名、当前证明失效、跨批原子结果、并发一次新建、整体回滚及单行辅助操作；UI 验收仍待后续。`queue_cas_test.go`、`library/single_input_test.go`、`app/single_input_test.go` 验证单行跳过、并发一次新建、重复计划幂等、删除/换绑/错误回滚、迟到响应拒绝和真实 App 离线跳过。无写入测试读取全部收藏表（包括 FTS 与封面目录）并在 Go 中排序比较，不依赖所有表都有 `rowid`。
