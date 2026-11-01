# 实施交接：8.2 已完成

## 当前状态

- 变更 `fix-track-identity-and-reduce-import-review`，repo-local，schema `spec-driven`。
- 已完成 1.1–8.2，**39/48**；CLI 已确认 8.2。下一任务 8.3：问题级动作、明确单行范围及浏览验收。
- 未同步主规格、未归档、未修改或清理用户应用数据。测试使用临时 SQLite、fixture 和 httptest；本轮未执行浏览器、真实网络或原生验收。

## 8.1–8.2 已完成

- 8.1 测试辅助函数返回旧行问题已修复：新测试直接 CreateBatch→EnqueueItems→ClaimNextItem。真实 App 问题读队列、scope、来源原文/数值/profile、混合失败、重试/草稿/空态与无网络副作用通过。
- Wails 生成、typecheck、117 项前端单测、build、store/app/importer/library 回归、store/app race、strict 与 diff 检查通过。8.1 完成提交 `894ae02`，之前检查点 `f94ddc3`。
- 8.2 将 ReviewView 接入问题 store，按稳定问题 ID 浏览；来源展开保留批次、数据行序号、原文、整行 JSON（含未映射列）、数值解析状态、各行原因及失败信息。独立 ReviewReasons 保留新发行组/成员及结构化原因，不把检索分称概率。
- 展示全问题/选定单行/失败成员范围；按来源切换候选事实，不拼接多来源快照。范围变化禁用旧操作，显式重开保留草稿。
- 新 `review-problems.spec.ts` Chromium/WebKit **4/4** 通过：两表异写一问题、不同专辑另一个问题、来源/原文/失败/原因可查、同计数来源变化与稳定选择。浏览使用 Wails 替身；后端归并由真实 App/Store 测试验证。
- 8.2 之后 typecheck、117 项单测、build 通过；主 chunk 约 1,059 kB 警告保留。未跑完整旧浏览套件，旧单行协议 fixture/断言仍须适配并回归。
- 当前问题动作已有初步接线但 **8.3 未完成**：需明确 scope 的无需匹配入口、动作范围/取消/成员变化浏览验证；**8.4 租约生命周期尚未接线**。不要将这些视为已交付。
- 数据代际仍 v9，不修改用户应用数据，未同步或归档规格，未执行原生/真实网络验收。

## 8.1 历史未完成检查点（现已修复）

本轮已写入但尚未验证完成：

- `internal/store/problem_queue.go` 在同一只读事务读取任务 revision、独立问题、完整 scope、查询草稿、来源行与统计；包含正在 queued/matching 重试的活跃成员，不获取租约、不启动网络。批次文件名按批次复用读取。
- `GetReviewProblemQueue` 返回稳定问题 ID、pending/failed 分类、完整来源及范围；各行候选仍归各自来源，不把冲突快照拼成一行。`ReviewItem` 增加原始整行、行索引、数值输入和 profile；候选所有权继续本地重算。
- `frontend/src/stores/review.ts` 新增问题读状态、稳定选择、串行合并刷新、任务 revision 下限及卸载响应失效；订阅 review/tasks/import/library 事件。尚未连接 `ReviewView.vue`，界面仍逐行；8.2–8.4 才完成显示、操作和租约接线。
- tasks store 订阅 review/tasks/library，保留全局 Problems 与 SourceRows，不用单批行数覆盖问题数；事件数据形状引用生成 DTO。前端桩新增问题读 API，通过显式 problemId 分组，不在浏览器按文本判等价。
- 新增问题 store 单测、扩展任务 store 单测、新增真实 App 问题队列测试。

实际执行：

```sh
gofmt -w internal/store/problem_queue.go internal/app/bindings_review.go internal/app/bindings_review_problem.go internal/app/review_queue_test.go
go test ./internal/app -run 'TestReviewProblemQueue' -count=1
```

**失败：** `TestReviewProblemQueueSourcesScopeAndReadOnly` 预期两个问题，但得到一个。定位到既有 `internal/app/enrich_worker_test.go` 的 `enqueueOneItem`：只要全局已有 review 行，它就返回第一条旧行，而不是新批次的行。新测试调用该 helper 后 MarkReview 作用在旧行，不同专辑新行仍 queued，因此不出现在问题队列。尚未修复；不能将这次失败解释为已证明生产问题归并错误。

建议下一步：新测试自行 CreateBatch→EnqueueItems→ClaimNextItem，明确取得新行 ID（或单独评估修复共享 helper 的影响），重跑定向测试，再执行 `/Users/amadeus/.go/bin/wails generate module`、typecheck、前端单测和后端回归。

**未执行：** 同一 `&&` 命令链中的 Wails 生成、typecheck、store 单测均因 Go 失败未运行；新 DTO 尚未生成到被忽略的 `frontend/wailsjs/`。本轮也未跑全量 Go/race/build/Playwright/严格规格校验。8.1 仍未完成，不把新增代码当作交付。

## 7.4 已完成

- `task_change_state` 在批次/输入/问题/成员事务内递增；Store.withTx 成功提交且修订变化才通知，App 装配 `review:changed` / `tasks:changed`（带 revision）。回调可读已提交终态，不靠计数变化或导入运行状态。
- 纯跳过和后台重评只通知 review/tasks，不发 library、不启动补全；回滚与相同成功计划重试无伪通知。库表不变断言只排除新增任务修订诊断表，仍覆盖全部收藏表。
- 补网络队列边界：批次刚结束但仍有 queued 重评成员时可重新启动；暂停批次仍不恢复。
- **当前新库代际 `track-identity-v9`**，v8 及更早无改写拒绝。未操作用户数据库，未同步/归档主规格。
- 最终 `go test ./...`、五包 race 全通过（600 秒上限，最慢约 223 秒）；Wails 绑定、typecheck、前端 112 项单测、build、strict、diff 检查通过。仍有约 1,050 kB 主 chunk 警告；未执行浏览器、原生或真实网络验收。
- 7.3 提交 `bc4071e`。前端仍使用旧逐行 GetReviewQueue：8.1 需新增/接入问题读模型及 store，8.2–8.5 完成来源范围与租约 UI。后端新事件尚未由旧 UI 完整消费，不把本阶段称为问题界面已交付。

## 7.3 已完成

- BatchStats 新增 ExistingSkipped 子集和 Problems；状态总和不重复加子集，同一事务读取原始行及问题统计。
- ProblemStats 返回独立总数/待选/失败/SourceRows；跨批问题全局不重复，批次按涉及行计算，混合状态按完整问题判定。TaskSummary 和独立统计 API 已接入；旧逐行 GetReviewQueue 尚留给 8.x 切换。
- store/library/importer/app 定向回归、Wails 生成、typecheck、strict/diff 通过；跨批混合、状态求和、实际多行接受的新增/跳过计数测试通过。7.2 提交 `62fe520`。

## 7.2 已完成

- 租约释放/pin 结束/回收在提交后唤醒有界 worker，错误/租约下一截止时间驱动定时续评；启动恢复活跃提示游标，不恢复终态，不改来源行失败信息。
- no-op 结果也校验输入/候选/事实 CAS，错误保留一分钟退避。实际缺少来源资料才排回现有 Runner；failed 仍需手动重试，暂停批次不自动恢复。
- `review_problem_network` 持久化待启动批次，出网门禁关闭时保留，设置恢复可续跑；Runner 保持共享配额/重试/动态门禁，复核读取不触发网络。**当前新库代际 `track-identity-v8`**，旧代际拒绝、不迁移数据。
- 真实 App 测试覆盖无新收藏事件的释放续评、读取零请求、关闭门禁不启动、恢复后真实 Runner；store 测试覆盖错误、no-op CAS、重启和终态保留。
- 全量 Go、五包 race（本次 600 秒上限、最慢约 221 秒）通过；Wails 绑定、typecheck、112 项前端单测、build、strict 和 diff 检查通过。最后启动幂等锁调整后定向 App 通过。未执行浏览器、原生或真实网络验收，主 chunk 仍约 1,050 kB 警告。
- 7.1 提交 `a2c6dd0`。7.3 行/问题统计与 7.4 review/task 通知仍待完成，前端问题 UI 仍待 8.x。

## 7.1 已完成

- `review_problem_key` 按输入、候选及来源事实的名称/前缀/source+track/release 反向索引影响问题；收藏/快照/署名/组事实变更同事务记录 dirty 和变更前事实，删除/换绑不会丢失旧目标影响范围。
- App 收藏成功通知唤醒单个后台 worker；32 track/每 track 100 问题分页，16 成员重评。每行重新消费 profile、当前收藏与保留的来源决策事实，不复用人工选版或批准，不联网、不创建收藏。
- 复用 CandidateFacts/输入约束/同组同内容偏好/精确 skip；跨组、显式其他专辑、未知竞争者、部分决策资料及重复位置仍复核。输入 scope、租约和全收藏事实代次一起重验，竞争者变化也拒绝旧计划。
- **新库代际 `track-identity-v7`**，v6 及更早拒绝。schema/store/library/app 测试覆盖反向索引、105 问题分页、删除/换绑、回滚、真实事件自动跳过、反例和竞争者竞态。
- 全量 Go 通过。五包 race 首次 240 秒超时仅 matching/importer 有结果；随后 library/store/app 使用 600 秒上限全部通过（约 203–220 秒）。Wails 绑定生成、typecheck、strict 和 diff 检查通过；未执行本阶段浏览器/原生/真实网络验收。
- 7.2 尚未完成：租约释放/错误退避的 worker 续评，启动恢复完整性和必要出网的既有 Runner 衔接；7.4 尚须 review/task 事件。不能把当前提示消费视为这些阶段已交付。

## 6.4 已完成

- 持久化问题 lease（90 秒）、独立在途 pin（10 分钟硬截止）和合并 recheck 提示；**当前新库代际 `track-identity-v6`**，v5 及更早明确拒绝，不迁移或删除用户数据。
- 获取租约校验完整范围、递增问题/查询修订，并淘汰已有 matching claim；续约只改服务端期限，不读取队列/联网、不使批准失效。草稿独立保存并递增查询修订，不修改原始输入/候选。
- 改搜和候选解析出网前持有独立 pin，空闲租约过期仍保护在途操作；明确释放立即拒绝迟到结果，pin 待请求返回或硬截止回收。查询 pin 绑定完整 scope hash，不能借新查询修订提交旧响应。
- 自动计划的 Automatic 和起始 InputScope 纳入签名，提交事务检查保护，不能结束编辑/确认中的问题；人工持有人可提交，释放/过期后旧批准拒绝。Runner 不领取受保护成员，旧后台处理暂缓，不污染候选或增加网络失败。
- 显式问题移出/重试/拆出校验持有人和完整范围；旧无 token 状态写拒绝受保护问题。释放/超时只排入当前问题重评提示，不继承目标。App 生命周期接入 15 秒/100 问题有界回收，退出等待停止。
- 新 store/library/importer/app 测试验证假时钟、续约/释放、在途/硬截止、重启、并发获取/回滚、编辑/确认保护、原始行/候选保留、释放后重新决策及迟到查询拒绝。
- 最终全量 Go、五包 race、Wails 绑定生成、前端 typecheck、112 项单测、build、strict 和 diff 检查通过；query scope hash 强化及释放幂等后再次全量/race 通过。build 仍有约 1,050 kB 主 chunk 警告。未执行浏览器/原生/真实网络验收。
- **后续边界**：7.1–7.2 仍须消费 recheck 提示、按影响索引调度并完成当前证据自动跳过；本轮只交付保护与可恢复提示。完整前端 lease/焦点/卸载生命周期在 8.4，当前 UI 未接入新 API。

## 6.1–6.3 已完成

- `domain.InputScope(version=1)` 统一单行/问题范围，绑定问题/查询/候选、成员/输入修订、profile/证明版本和完整证据摘要，纳入计划 Hash/签名。单行不扩展成员，多行必须完整且当前重新证明等价；保存的本地事实失效也拒绝旧批准。
- 准备前后、确认和提交重验范围；身份解析复用相同位置要求，I/O 期间新成员使旧范围失效。新增/拆出/候选/查询/规则/证明变化及客户端篡改测试通过。
- 原子结果最多新增一个 track，按最小成员 ID 计唯一新增，其他来源行 `skipped/existing_track`；已有目标全跳过且不改收藏，保留新增发行确认与同计划幂等。返回来源身份、各行和批次结果。
- App 增加显式问题 scope/接受/跳过准备 API；无需匹配先固定单行范围，再读原始解析值，多行无绑定 seed 拒绝；移出和失败重试不推广到其他成员。
- 新 library/store/app 测试覆盖当前证明、本地换绑、跨批计数、并发、成员/结果/未暂存文件字段失败整体回滚、只处理所选原文。旧迟到搜索用例改为先拒绝查询前旧 skip 计划，再准备当前计划结束输入，保留迟到响应拒绝断言。
- 最终 `go test ./...`、五包 race（matching/importer/library/store/app）全部通过；Wails 绑定生成、前端 typecheck、112/112 单测、build、strict 和 diff 检查通过。构建仍有约 1,048 kB 主 chunk 大于 500 kB 的既有警告。未执行浏览/原生/真实网络验收。无数据库结构/代际变更。
- 6.1–6.3 提交 `a233dae`。随后补测无需匹配保存年份/碟号/轨号、保留输入时长；时长按主规格只存在于上游只读快照，不新增本地当前字段、不伪造来源快照。该补测的定向 App 回归通过。
- 6.4 租约、7.x 重评/统计/事件、8.x 问题 UI 尚未完成。当前前端仍逐行；本阶段 API 不代表多来源界面已交付。

## 5.4 已完成

- 问题 scope/version 包含问题、查询、候选及全部成员/输入修订；改搜出网前保留 query revision，提交校验完整范围，成员变化/拆出/其他查询/终态拒绝迟到结果。
- Runner 与人工问题改搜统一来源候选 envelope（含显式 version、ScoredCandidate、变体），问题候选按成员保存，不持久化 UI 的所有权/动作。App 仍兼容旧单行候选读取。
- 新 App 接口提供问题范围读取、改搜、无一匹配、列明 failed 成员重试、列明成员移出及拆出。旧单行改搜/无一匹配对多行问题明确要求拆出，不静默扩大范围。
- Store/App 测试验证查询竞态、终态不恢复、成员变化、草稿保留、空候选仍活跃、只重试失败成员、范围错误拒绝、协议版本必需。旧迟到搜索测试改为真实 review 状态（不是仅 matching 桩）。
- `go test ./...`、五包 race（matching/store/importer/library/app）通过；生成 Wails 绑定，frontend typecheck 通过。最后协议解码强化后重跑 store/importer/app 定向通过。
- 5.3 提交 `5d95738`。未执行前端单测/浏览/原生/真实网络验收；完整问题 UI 在 8.x，接受/跳过多行计划在 6.x。

## 5.3 已完成

- `SplitProblemItem` 同事务校验问题/输入修订，拆出单个成员到 isolated 活跃问题。问题与被拆行 revision 递增；其他行原文、候选、修订不变。
- 隔离锚点在重试/重建中保留，既不自动合回也不吸纳后续等价输入；完成后不成为历史身份规则。失败原子回滚。
- store 测试覆盖范围失效、迟到响应、重试隔离、后续输入、原文/候选不变及回滚；library 真实批准测试验证拆出使旧新增发行批准失效，无收藏写入。
- store/matching/library/importer/app 定向包及 store/library/matching race 通过。5.2 提交 `4245749`。
- 尚未增加 App/前端拆出入口，按 5.4/8.3 接线；多行批准仍待 6.1。

## 5.2 已完成

- 复核/最终失败事务同步创建或加入活跃问题；Runner 传入实际 selection 的完整事实（含必要资料不足标记），不重新由展示候选伪造精确证明。
- 单行准入/跳过/移出先解除自身成员并记录问题诊断修订，剩余成员重建 anchor，末项才关闭；全部与原行和准入事务原子回滚。不会结束同问题其他来源行。
- 重试、Claim 和重启恢复同步成员输入修订；暂停批次不取消其他批次工作。重新证明时校验本地绑定及事实修订，失效证据不能吸引新成员；可处置范围保留。
- 真实 Runner 测试验证无结果异写两批一问题；store 测试验证并发、重启、独立错误/原文、精确路径、终态不恢复、后续输入不复用关闭问题、非传递链和回滚。
- `go test ./...`、五包 race（matching/store/importer/library/app）、strict 校验和 diff 检查通过。5.1 提交 `9751a42`。
- 无数据库结构变更、无前端/原生/真实网络验收。拆出、版本化问题查询及多行操作尚待 5.3+；当前 UI 仍逐行。

## 5.1 已完成

- matching 新增强文本/精确身份问题证明和版本化 anchor。所有成员共享名称证据，不把别名局部相容做传递闭包；缺字段、不同版本/角色/合作、不同数值要求及未知注释保持独立。
- 精确路径重查完整当前候选范围及所有输入的来源事实冲突，不接受默认偏好、候选集合重叠、同 Recording/ISRC 或时长容差作等价；共同位置检查移到 matching 供 library 与问题证明复用。
- store 新增只读活跃候选问题检索，JSON blocking key 不授予归并，支持游标与显式截断；不读取历史终态或改动数据库结构。
- 表驱动、性质测试及 store 回归覆盖异写、缺字段、等值时长、非传递 A/B/C/三角反例、序列化、来源冲突、重复位置、规则过期、分页/多别名、取消/读取错误。
- 七个后端包及 matching/store/library/importer race 通过。尚未接入自动问题生命周期，5.2 未完成。

## 4.7 已完成

- 移除旧 `TestCrossGroupNeverAutoSelects` 无条件守卫，保留真正跨组反例；真实 Runner 增加 3253212 显式专辑选择/无专辑不同内容复核成对用例，与 you 两版/未知竞争者、重复位置测试共同覆盖生产链路。
- 旧 `Engine.Decision` 仅是展示分类，注释已说明不授予自动身份或准入；历史十行 fixture 改名 `TestCSVSearchDisplayRegression`，不把缺少发行/位置的旧 Engine 测试称为导入端到端。不改变歌词或旧展示评分。
- 定向 matching/importer/library/acceptance/lyrics 通过；本阶段末 **`go test ./...` 通过**。严格规格与 diff 检查通过。
- 4.6 提交 `00eeada`。9.4 最终验收仍未勾选：后续 5–8 组完成后须重跑全量和 race。未运行前端/原生/真实网络验收。

## 4.6 已完成

- 本地充分和远程选版共用 `prepareAutomaticTarget`，已收藏目标采用轻量 skip 计划并绑定决策读取的 track/release 修订；输入修订在补资料前读取，不给迟到决策刷新授权。
- Runner 共用单一提交、日志和通知分支；远程 Existing 同样记录已存在跳过，不发 library 事件、不触发补全、不改已有元数据。
- 新测试验证真实 skip operation、事实修订改变拒绝、远程已有目标通知/补全计数为零、人工选择同组第二发行不被偏好替换且仍要求成员确认。
- 七个后端定向包及 library/importer/app race 通过。4.5 提交 `ca6119e`。

## 4.5 已完成

- 在输入收敛后先检查同组、同录音及标题/署名相容、同发行无重复位置，再采用已收藏精确 track > 已有发行 > 总曲数 > 非世界国家 > 稳定身份顺序。
- 未知总数只对真正优先子集补取；一个已收藏目标不为其他候选取摘要，跨组/内容/位置歧义在总数 I/O 前复核。
- 新 selection 用例经真实服务/存储验证：已有 track 优于仅已有且曲数更多的发行、显式 Deluxe 不被吞掉、多个已收藏目标不新增、未知总数保持复核、国家/稳定身份顺序、重复位置/跨组/不同录音无偏好。
- 七个后端定向包通过；library/importer race 通过。4.4 提交为 `19f1ab7`。

## 4.4 已完成

- 为普通 App 测试安装外部 HTTP 拦截，覆盖 provider 重装及封面默认 transport；只允许数字回环地址及明确 replay。意外请求使整包失败。保护启用后发现三个旧准入测试启动后台补全，现已暂停与测试无关的补全。显式 live 环境开关仍保留。
- 修复远程无结果后本地候选重入丢失专辑门槛/清单位置歧义；即使远程返回已有精确 track，完整清单中未收藏重复位置仍阻止自动操作。
- 完整 profile 先限定输入再判组；明确不符的合成候选不补取、不参与竞争；未知候选不被当作冲突排除。人工解析测试与自动输入约束分开。
- 保留原精确搜索事实及新资料视图，摘要名称/组等冲突重新核验，不以新摘要覆盖旧证据。展开后相同精确目标在人工列表合一。删除旧未使用的 `resolveSelectionIdentities`。
- 旧 fixture 补真实位置及发行署名。`God knows...` 按现有规则仍视为可能截断；自动通路 fixture 改用完整标题，同时增加该截断标题不得自动入库的反例，没有放宽规则。
- 新 selection 用例覆盖非 HighConf 充分证据、只补未排除子集、批量请求、unsupported/partial/失败/取消、完整无结果、资料冲突和未收藏重复位置。
- 真实 Runner + MusicBrainz 适配器 + httptest 覆盖第二碟 track、重复录音、部分清单、请求失败、旧输入修订；同发行请求一次，被排除发行零请求，无预览实体写入。

通过：

```sh
go test ./internal/matching ./internal/importer ./internal/library ./internal/store ./internal/app ./internal/acceptance ./internal/lyrics ./internal/provider/musicbrainz -count=1
go test -race ./internal/matching ./internal/importer ./internal/library ./internal/store ./internal/app
openspec validate fix-track-identity-and-reduce-import-review --strict
git diff --check
```

第四组已完成，但不能由本轮通过推定整个变更最终验收完成。

## 4.3 已完成

`22f5a8a` 的测试类型错误和 `47eea41` 的发行 UPDATE 零行测试错误均已修复。发行竞态测试现在按计划来源身份读取真实发行 ID，断言 RowsAffected=1 后，旧 skip 计划确实被拒绝。

- Runner 删除无绑定文本直接复核短路及其废弃 matching 接口/日志，改走 `PrepareLocalExisting`。完整本地证据可精确 skip，资料不足继续检索；本地查询失败不伪装未匹配。
- 本地分支消费完整 profile、同一实际本地组与当前所有权，检查完整发行清单及其他可能位置；未知总数、不同内容、重复位置保持上游分支，不靠已收藏吞掉显式专辑/版本限定。
- 当前用户名称只能关联到已验证位置，不复制进候选快照；用户专辑改名不能误排除同发行其他位置，同 Recording 的文本别名不能消除位置歧义。
- skip 准备结束后对照证据采用的 track/release revision，不能默默刷新旧证据。提交沿用单行事务 CAS、删除/换绑拒绝、失败回滚和幂等；纯 skip 不写收藏表、不启动补全、不发 library 通知。
- 新 Runner 用例核对实际请求次数、终态和 track：完整资料无请求跳过、删历史批次仍同结果、稀疏已有先请求一次再按既有分支结束、无绑定文本可正常新建精确上游 track、资料失败保留错误。library 用例覆盖当前改名、多个兼容版本、未知/冲突、准备与提交竞态及无收藏写入。
- 远程分支仍是旧 `SelectCandidate`；完整本地/远程输入优先选版在 4.4–4.6 接线。无原生或真实网络验收，本轮未改前端。

本轮最终通过：

```sh
go test ./internal/matching ./internal/importer ./internal/library ./internal/store ./internal/app ./internal/acceptance ./internal/lyrics
go test -race ./internal/matching ./internal/importer ./internal/library ./internal/store ./internal/app
openspec validate fix-track-identity-and-reduce-import-review --strict
git diff --check
```

## 4.2 已完成

`da0cea4` 的测试辅助函数编译阻塞已修复，随后完成事实池边界检查：

- `BuildCandidatePool` 按来源检查的精确 track 聚合同一目标，remote/current/snapshot/tracklist 分别保留完整视图与出处、修订。无绑定记录在 Related，不授予 owned。
- 每份视图单独比较输入；当前改名不会覆盖快照，完整发行清单位置可以独立支持匹配，不向稀疏快照拼接字段。部分清单不提供本地位置完整性。
- 事实冲突保留双方来源和值；包括版本/CV/合作/位置/时长/录音/发行及明确空值，自动证据保持 unresolved。当前显示值不能覆盖来源关键限定。
- 本地快照在消费入口再次核验绑定；重新读拥有状态后核对本地证据修订，删除/换绑/读取错误不伪装成可自动处理。
- `library.CandidateFacts` 连接文本召回与事实池，新增 matching 消费侧 `LocalFactLookup` / store `ReadLocalFacts` 补读远程精确身份。完全无文本交集的已有目标仍加载快照，不能因文本召回漏掉事实冲突。
- 不出网，不写收藏或历史输入选择；Runner 自动分流接线仍待 4.3–4.6。

通过：

```sh
go test ./internal/matching ./internal/library ./internal/store ./internal/importer ./internal/app ./internal/acceptance ./internal/lyrics
go test -race ./internal/matching ./internal/library ./internal/store ./internal/importer ./internal/app
openspec validate fix-track-identity-and-reduce-import-review --strict
git diff --check
```

新增纯 matching 及真实 library/store 用例覆盖：用户改名、可靠异写、同来源精确目标合一、不同来源同 ID/同录音不同 track、无绑定提示、无文本交集精确补读、冲突返回顺序、明确空值、部分清单、旧快照/修订、错误和输入对象不变。本轮未改前端、未重跑浏览器，也未执行最终全量 Go/原生或真实网络验收。

## 4.1 已完成

检查点 `6e09c02` 的 dirty 主键冲突已隔离复现：外层 SQLite UPSERT 可覆盖触发器内部的 `INSERT OR IGNORE` 策略。触发器现在以 `NOT EXISTS` 排除已有标记后插入，不吞掉其他约束错误。新增真实快照重复写入/UPSERT 回归通过。

- matching 声明 `LocalRecall` 批量事实契约，store 实现；当前值、有效 track/release 快照、结构化关系与修订分别保留，不拼成上游事实。
- 独立 `local_match_key / local_match_dirty / local_match_state` 支持增量失效、规则版本重建和显式 `RebuildLocalMatch`。**当前新库代际为 `track-identity-v5`**，v4 及更早代际无改写拒绝，不迁移用户数据。
- 关系移动同时使 OLD/NEW 归属失效，艺术家删除在级联前标记相关收藏。只索引当前绑定有效快照，track 明确的发行关系冲突也拒绝。
- 批量召回不使用浏览上限：一次最多 128 输入，每输入默认 100/最多 1000 目标，显式返回 `Truncated`。JSON 表参数保留大量异写键，不撞动态 OR 表达式深度上限。
- 空批次不重建；读取/索引更新同事务，失败保留旧键及 dirty 标记；错误/取消不伪装空结果。无历史导入选择依赖，不出网。
- 当前只交付事实召回与索引，不代表 Runner 自动跳过或输入优先选版已接通。

本轮通过：

```sh
go test ./internal/store -run 'TestLocalRecall|TestMigrate|TestSchemaTablesExist|TestPriorGenerations' -count=1
go test ./internal/store ./internal/matching ./internal/library ./internal/importer ./internal/app ./internal/acceptance ./internal/lyrics
go test -race ./internal/store ./internal/matching ./internal/library ./internal/importer ./internal/app
openspec validate fix-track-identity-and-reduce-import-review --strict
git diff --check
```

测试覆盖旧失败回归、当前/快照分离、异写/限定、未绑定关系、关系两端移动及删除、换绑和发行冲突、2000 条之后目标、截断、1200+ 异写键、规则重建/重启、事务回滚及并发一致读。说明见 `internal/store/README.md`。4.1 未改前端，本轮未重跑浏览器；下述 3.6 浏览结果不能代替未来前端改动验收。最终全量验收仍待 9.x。

## 3.6 竞态测试已修复并验证

上一检查点 `7f7a5c6` 的失败来自测试桩在 `RefreshReviewCandidates(rows)` 中对 Vue Proxy 调用 `structuredClone`，并非已经证实的生产事件问题。本轮在 `frontend/tests/e2e/candidate-ownership.spec.ts`：

- 按 Wails IPC JSON 边界复制候选，不克隆响应式 Proxy。
- setup 等待候选按钮与本地刷新就绪后再制造竞态。
- 改搜桩保存新候选、递增行修订，后续队列读取真实的当前桩状态；提交只结束选定输入。
- 挂起一次本地响应而非所有后续刷新，保留真正的旧响应。
- 验证迟到刷新不能覆盖新查询；提交断言核对新 track。
- 验证切换条目后迟到成功/错误不能污染当前候选、草稿、动作；提交断言核对第二项 track、输入 ID 与 revision。
- 验证卸载后不继续订阅，新页面打开后旧页面响应不能污染当前动作。
- 验证队列与收藏事件两种返回顺序下拥有状态和候选自己的封面仍正确。
- 验证改搜在途发生收藏变化后使用新目标及新输入修订。
- 验证整次本地刷新拒绝时保留候选正文、清除旧拥有状态/封面并禁用动作，手动刷新可恢复。

本轮没有改生产代码；上一检查点的刷新和封面保护实现经这些测试验证通过。候选操作说明已更新于 `internal/matching/README.md`。

## 本轮实际验证

通过：

```sh
go test ./internal/app ./internal/store ./internal/covers ./internal/library ./internal/importer ./internal/matching
go test -race ./internal/app ./internal/store ./internal/covers
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run build
SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- \
  candidate-ownership.spec.ts review-actions.spec.ts admission.spec.ts \
  identity-resolution.spec.ts visual-controls.spec.ts live-updates.spec.ts \
  cover-updates.spec.ts screenshot-review.spec.ts
SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- candidate-ownership.spec.ts --repeat-each=3
```

- 前端单测 **22 文件、112 测试**通过。
- 重新构建后的 8 个 Playwright 文件 Chromium/WebKit **116/116** 通过。
- 候选所有权测试重复三次，双浏览器 **90/90** 通过。
- 构建成功，仍有主 chunk 大于 500 kB 的警告（约 1,044 kB）；未扩展到打包优化。
- 未执行最终全量 Go、全量 race、全量 Playwright、原生 Wails 人工验收或真实来源网络验收。9.4/9.5 仍未完成。
- 浏览器使用 Wails 替身，不能替代真实 Go API、数据库、图片字节或真实网络证据。

## 已完成实现与后续边界

### 1–2：输入证据和身份解析

- 版本化 `InputProfile` 保留原文、可靠异写、录音/发行完整限定、角色/CV/合作署名及数值解析精度/失败状态。
- 独立元数据自动证据检查，不改变旧展示评分和歌词语义；分数不等于身份概率。
- matching 声明 `CandidateResolver` / `CandidateSources`，App 装配注入；来源差异留在 provider 适配器，不在业务层 switch 来源。
- MusicBrainz 在指定发行内解析合成候选，检查完整清单和真实位置，重复录音不选首项；同决策请求复用发行响应并遵守缓存、配额、取消。
- iTunes/QQ 精确身份检查不出网，不支持的补取明确表达。
- `PrepareCandidate`、`CandidateSeed`、直接 `PrepareAdmission` 拒绝合成/无效身份绕过；App 多位置返回 resolution 而非可提交计划。
- `Selection.ResolvedCandidates` 接到 Runner，但不代表完整输入优先选版已经交付。

### 3：精确候选与单行事务

- `import_item` 有正整数 revision、终态原因、当次精确目标/问题修订诊断；原始行和解析列由触发器保护。
- `review_problem` / `review_problem_item` 基础表保留规则/锚点/证明/查询/候选修订，一行最多一个活跃归属；语义归并和生命周期业务仍待 5.x。
- **当前新库代际为 `track-identity-v5`**（4.1 增加匹配索引）。旧结构拒绝打开，不迁移、不自动删库。source+track 唯一；同 ISRC/Recording 不同 track 可共存。
- `ResolveLocal` 仅来源精确身份与 `FindExactBinding` 当前绑定可以返回 owned，核验发行关系、绑定修订；错误/冲突独立表达。
- 旧文本重复前置短路及 `LocalDuplicateLookup` 已移除；相关 ID 不生成拥有目标。
- `AdmissionSeed.Operation` 区分 admit / skip_existing；写实体前重验输入整数修订。`PrepareSkipExisting` 不出网，提交检查目标及 track/release 修订，不需要新增发行批准。
- 重复接受记 `skipped/existing_track`；精确跳过不写收藏字段、时间、图片、歌词或补全状态。同成功计划幂等，其他旧计划拒绝，Runner/在途改搜使用起始修订 CAS。
- 纯已有结果不发 library 事件、不启动补全；完整 review/task 事件仍待 7.x。
- `MergeCandidate` / `MarkMerged` / merged 状态 / merged_into 列 / 简化本地候选快照已移除；保留编辑器归组/成员合并。
- `CandidateRow` 的 ownership 与 `accept / skip / add / owned / unavailable` 枚举由共享表格、SearchView、ReviewView 使用；中文文案、相关 ID 不判权限。
- `CandidateCoverTarget` 只查候选自己的来源发行与封面修订；刷新、快照重载、`BuildCoverRefs` 不信任旧 releaseId。封面 URL 带已知 provider/ref，服务端拒绝不一致目标。
- ReviewView 订阅 library 事件只刷新当前候选，不重搜；有响应代次、卸载保护和手动刷新。搜索页同样支持本地刷新。封面错误独立表达，不预取错误行。

### 未完成范围

- 4.1–7.4 已完成；当前从 8.1 开始。
- 5.x 已交付后端证明、生命周期、拆出和问题范围协议；6.x 已交付多行准入、单行辅助操作和后端交互租约。
- 7.x 已交付有界重评、恢复/网络队列、行/问题统计及 review/task 事件；当前新库代际为 v9。
- 8.x 多来源问题 UI、成员范围、拆出和租约；当前复核仍逐行。
- 9.x 跨模块 fixture 和最终全量验证。

## 历史测试与工具注意

- 全表不变测试须兼容无 rowid 表：读出后 Go 稳定排序，不能缩减检查范围。
- `screenshot-review.spec.ts` 编辑器原有 11 个用户徽标；旧“没有用户徽标”断言曾在旧基线复现失败，已按用户确认改成等待编辑器加载并检查 11 个徽标，仍保留详情抽屉不展示字段来源的断言。
- `invalidateCandidateOwnership` 使用生成的 Wails `app.CandidateRow` 构造器，保留 `convertValues`，不靠强制类型断言绕过。
- `wails` 不在 PATH，使用 `/Users/amadeus/.go/bin/wails`；修改导出 DTO 后生成绑定。
- Wails 绑定、dist、node_modules、测试产物均被忽略。
- 旧编辑器测试基线位于 `tmp/harness/identity-35-baseline-YM4EZk`（忽略目录，不是工作树），没有切换或重置当前仓库。

阶段提交：`5ba0b3c`（3.1）、`981ba20`（3.2）、`8cea84e`（3.3）、`ecff4cb`（3.4）、`4e912d5`（3.5）、`7f7a5c6`（3.6 未完成检查点）、`ca32fcf`（3.6 完成）、`6e09c02`（4.1 未完成检查点）、`47ed362`（4.1 完成）、`da0cea4`（4.2 未完成检查点）、`1eaaaf9`（4.2 完成）、`22f5a8a` / `47eea41`（4.3 未完成检查点）、`edbf935`（4.3 完成）。4.4 未完成检查点以 `git log` 为准。
