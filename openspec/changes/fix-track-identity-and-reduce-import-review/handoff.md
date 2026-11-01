# 实施交接：3.1–3.5 完成，3.6 检查点后暂停

## 当前状态与续接

- 变更：`fix-track-identity-and-reduce-import-review`，repo-local，schema `spec-driven`。
- `tasks.md` 已完成 1.1–1.7、2.1–2.5、3.1–3.5，共 **17/48**。**3.6 有代码和测试，但未完成，保持未勾选。**
- 用户明确要求本轮收尾后暂停，将在新会话继续；**没有开始 4.1**。
- 本轮以 WIP 检查点提交 3.6 的当前工作，不把测试未通过描述为完成。以 `git log` 查检查点提交 ID。
- 规划文件和 10 个增量规格仍是实施依据；本文件只记录交接事实，不替代规格、CLI 指令或任务完成标准。
- 未同步主规格、未归档、未访问真实来源，未修改或清理用户应用数据。测试使用临时 SQLite、fixture 和浏览器替身。

下一会话建议：

```text
/opsx-apply fix-track-identity-and-reduce-import-review
请先读本变更 handoff.md，从未完成的任务 3.6 继续，先处理候选刷新竞态测试。
```

## 3.6 当前阻塞：浏览测试桩 DataCloneError

最近一次：

```sh
SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- \
  candidate-ownership.spec.ts review-actions.spec.ts admission.spec.ts \
  identity-resolution.spec.ts visual-controls.spec.ts live-updates.spec.ts \
  cover-updates.spec.ts screenshot-review.spec.ts
```

结果 **96/98 通过**。两个失败是同一用例的 Chromium / WebKit：

- 文件：`frontend/tests/e2e/candidate-ownership.spec.ts`
- 名称：`review: late local refresh cannot overwrite another item or a newer search`
- 当前约第 95 行：测试桩在 `RefreshReviewCandidates(rows)` 内调用 `structuredClone(rows)`。
- 当前约第 101 行：等待 `window.__finishLocal` 成为函数失败，实际为 undefined。
- 错误上下文显示 **`DataCloneError: Failed to execute 'structuredClone' on 'Window': [object Object] could not be cloned.`**。传入桩件的是 Vue 响应式候选；抛错发生在创建挂起 Promise 之前，因此未设置完成函数。不是已证明的事件未触发问题。
- 错误被生产刷新捕获，候选正文保留，动作变为不可操作，页面显示错误；不是 pageerror 泄漏。
- 产物（忽略文件，后续测试会覆盖）：
  - `frontend/test-results/candidate-ownership-review-7d8a7-ther-item-or-a-newer-search-chromium/error-context.md`
  - 同目录 `trace.zip`；WebKit 对应 `...-webkit/`。

**下一步先修测试桩而不是放松竞态断言：** 应按 Wails IPC 的 JSON 边界复制候选（通用 stub 已用 `JSON.parse(JSON.stringify(rows))`），或显式提取协议字段，不直接 structuredClone 响应式 Proxy。本轮读取错误上下文后停止，没有实施或重跑这个修正。

后续竞态断言尚未真正执行，不保证修完第一处就全绿。还需检查：

1. 测试 `setup` 只导航，挂起事件前应以页面可见状态确认队列已就绪，避免依赖加载时机。
2. 该用例的 `ResearchReviewItem` 替身只返回新候选，`GetReviewQueue` 仍从最初 candidates 生成响应。生产改搜会持久化候选并递增行修订；测试应忠实表达持久化，不靠过期静态队列测试错误协议。修桩时仍保留真正的旧本地刷新响应。
3. 应实证迟到刷新不能覆盖新查询、切换条目/卸载，以及队列刷新与收藏事件交错时仍使用当前身份；不以候选数量或总通过数替代这些断言。
4. 全部指定行为验证通过且结构说明准确后才勾选 3.6，重跑 apply instructions 确认进度，再考虑 4.1。

## 已完成内容概览

### 第 1–2 组（此前已完成）

- 版本化 `InputProfile`，保留原文、可靠异写、录音/发行完整限定、角色/CV/合作署名及数值解析精度/失败状态。
- 独立元数据自动证据检查，不改变旧展示评分和歌词比较语义；分数不等于身份概率。
- matching 消费侧声明 `CandidateResolver` / `CandidateSources`，App 集中装配并注入；来源差异留在 provider 适配器，不在业务层 switch 来源。
- MusicBrainz 在指定发行内解析合成候选，检查完整清单和真实位置，重复录音不选首项；同决策请求复用发行响应并遵守缓存、配额、取消。
- iTunes/QQ 精确身份检查不出网，不支持的补取明确表达。
- `PrepareCandidate`、`CandidateSeed`、直接 `PrepareAdmission` 不允许合成/无效身份绕过；App 多位置返回 resolution 而非可提交计划。
- `Selection.ResolvedCandidates` 接到 Runner，但不是完整的输入优先自动选版交付。

### 3.1：队列与问题存储基础

- `import_item` 新增正整数 revision、终态原因、当次精确目标/问题修订诊断；原始行与解析列由触发器保护。
- 新增 `review_problem` / `review_problem_item`，保留规则/锚点/证明/查询/候选修订；部分唯一索引约束一行最多一个活跃归属。
- 仅基础结构，语义归并与创建/加入/拆出/关闭业务事务仍待 5.x。
- **当前新库代际是 `track-identity-v4`**：3.1 时从 v2 推进到 v3，3.4 移除合并列并收紧状态后推进到 v4。旧结构拒绝打开，不迁移、不自动删库。
- source+track 唯一保持；同 ISRC/Recording 不同 track 可共存。

### 3.2：精确所有权

- `ResolveLocal` 只经来源精确身份检查与 `FindExactBinding` 当前绑定查询返回 owned，校验发行关系并返回绑定修订。
- 明确 `unowned / owned / identity_conflict / lookup_error`；ISRC、Recording、无绑定文本不生成已有目标。
- 旧文本重复检查按用户确认拆成独立 `LocalDuplicateLookup`，未在本轮强行提前重写完整本地召回。
- `SetProvenance` 递增修订；同来源+track 发行冲突明确报错。

### 3.3：单行接受与跳过

- `AdmissionSeed.Operation` 区分 admit / skip_existing；输入整数修订纳入计划，在实体写入前重验。
- `PrepareSkipExisting` 只消费精确候选和本地绑定，不解析合成、不出网；提交检查目标 ID 与 track/release 修订，不需要新增发行批准。
- 重复接受记 `skipped/existing_track`，最多一个并发创建者；跳过不写任何收藏表/字段/时间/图片/歌词/补全状态。
- 同一成功计划幂等，另一旧计划拒绝；旧改搜、失败、重试不能恢复终态。Runner 及在途改搜使用起始修订 CAS。
- App/Runner 纯已有结果不通知 library、不启动补全；完整 review/task 事件仍待 7.x。
- 单行终态依真实结果生成，删除了由调用者指定终态的 `AdmissionSeed.ItemStatus`。

### 3.4：删除候选合并

- `MergeCandidate` / `MarkMerged` / merged 状态 / merged_into 列 / 简化本地候选快照已移除。
- 旧无绑定文本短路仍会进入复核，但仅保存独立 related evidence，不制造可接受/跳过的本地候选；4.3 再替换整个短路。
- 编辑器的专辑组/成员合并、专辑合并展示未删除。
- 前端旧合并调用改走精确 skip 计划；状态 CHECK 拒绝旧写法。

### 3.5：候选动作协议与界面

- `CandidateRow` 返回结构化 ownership 和 `accept / skip / add / owned / unavailable` 枚举。中文文案只显示，不用相关 ID/徽章判权限。
- `frontend/src/lib/candidateActions.ts` 集中动作标签、状态检查和本地跳转目标。
- 共享候选表格、SearchView、ReviewView 接入；搜索加入后读服务端拥有状态，不在浏览器用结果 ID 伪造绑定修订。
- 真 Go App 和 Playwright 验证同 ISRC 双版本可分别跳过/接受或已在库中/加入，明确第二版提交其真实 track。

## 3.6 当前代码（未完成验收）

- `internal/store/cover_identity.go`：`CandidateCoverTarget` 单次只读查询候选自己的来源发行及封面修订，不从相关 track 推导。
- `internal/app/bindings_review.go`：每次本地判定先清空旧 releaseId / coverRevision，重新计算，封面查询错误另记 `coverError`。
- `internal/app/bindings_upstream.go`：增加 `RefreshReviewCandidates`，与搜索共用只读刷新；`BuildCoverRefs` 不信任客户端旧 releaseId。
- `internal/covers/resolver.go`：本地 ID 加来源断言的请求若关系冲突则拒绝，纯本地调用仍可只给 ID。
- `frontend/src/api/index.ts`：封面 URL 在有本地 ID 时也携带已知 provider/ref，便于请求时重验。
- ReviewView 订阅 library 变化，单独合并刷新当前候选，不回到旧池、不重搜；添加手动“刷新本地状态”、响应代次和卸载保护。搜索页也有本地刷新按钮。
- 查询整次失败时保留正文，清掉旧拥有目标/封面并禁用动作；封面错误单独显示。`useCandidateCovers` 不预取 coverError 行。
- Wails 类型问题已解决：`invalidateCandidateOwnership` 使用生成的 `app.CandidateRow` 构造器保留 `convertValues`，新增单测证明不修改原候选及原来源身份，无强制类型断言绕过。
- 新后端测试 `internal/app/candidate_refresh_test.go`，覆盖删除、曲目/发行换绑、相关封面 ID、封面查询错误及拥有查询错误恢复；covers 增加冲突目标测试。

## 本对话已处理的测试阻塞

1. 3.3 的全表不变辅助函数原来假定所有表都有 rowid；已改为读出后 Go 稳定排序。不是删表或缩减检查范围。
2. 3.5 的旧 `screenshot-review.spec.ts` 断言编辑器没有“用户”徽标过时：编辑器从 `86f8717` 起就显示用户值，stub 有 11 个用户字段。用 `ecff4cb` 临时基线复现，等待编辑器加载后 Chromium/WebKit 都失败，原通过来自未渲染空集合。
3. 经用户明确确认，测试改为等待编辑器加载并检查 11 个用户徽标；保留详情抽屉不展示字段来源的断言。新增 filter 初次写错作用域，已修成相对查找，单项重复 **6/6**、随后相关 **92/92** 通过。
4. 3.6 的对象展开缺少 Wails `convertValues`：本轮使用生成类构造器修复，typecheck 与新增单测通过。
5. 最后阻塞为上文的 Proxy `structuredClone`，未修正、未勾选。

## 实际验证结果及边界

3.3 阶段通过：

```sh
go test ./internal/library ./internal/store ./internal/importer ./internal/app ./internal/acceptance
go test -race ./internal/library ./internal/store ./internal/app ./internal/importer
```

3.5 阶段通过：后端定向包、前端 typecheck、110 单测、构建；相关双浏览器 **92/92**，旧编辑器测试重复 **6/6**。

**3.6 当前代码**已执行并通过（后端在上一轮验证，最后一轮只再修改前端 helper/单测）：

```sh
go test ./internal/app ./internal/store ./internal/covers ./internal/library ./internal/importer ./internal/matching
/Users/amadeus/.go/bin/wails generate module
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run build
openspec validate fix-track-identity-and-reduce-import-review --strict
git diff --check
```

- 最新前端单测 **22 文件、112 测试通过**。
- 最新上述 8 个 Playwright 文件 **96/98 通过**，失败的两个浏览器变体未修，不能称全绿。
- 构建有 >500 kB chunk 警告，成功构建；生成模型运行时导入后主 chunk 约 1,044 kB，未为此扩展优化范围。
- 3.6 当前修改未重跑 race；不把 3.3 的 race 通过当作最终代码 race 结果。
- 本对话没有运行 `go test ./...`、最终全量 race、全量 Playwright、原生 Wails 人工验收或真实来源网络验收。9.4/9.5 保持未勾选。
- 浏览器使用 Wails 替身，不能替代真实 Go API、数据库、图片字节或真实网络证据。

## 后续未完成范围

- 先完成 3.6。4.x 仍有旧文本重复短路、HighConf/跨组与已有发行逻辑，尚未完整接入本地召回、输入优先约束和自动精确跳过。
- 5.x 问题等价证明与业务生命周期未实现。基础问题表存在不代表问题队列已交付。
- 6.x 多行 scope/hash、原子多行结果、租约未实现；当前准备入口兼容单行，并非所有交互动作均已绑定客户端范围修订。
- 7.x 有界重评、独立问题统计、review/task 事件未实现；纯 skip 现在不发 library 事件，完整通知仍待接入。
- 8.x 多来源问题 UI、成员范围、拆出及租约未实现；当前复核仍逐行。
- 当前 resolution 展开仍是本次交互候选，完整持久化与迟到响应问题范围保护需随后验证。

## 阶段提交与工具

已完成提交：

- `5ba0b3c`：3.1 输入修订与问题存储基础。
- `981ba20`：3.2 精确所有权，分离旧文本接口。
- `8cea84e`：3.3 单行 skip/接受与修订。
- `ecff4cb`：3.4 删除候选合并协议，代际 v4。
- `4e912d5`：3.5 结构化动作与界面、旧编辑器测试修正。
- 3.6 检查点与本 handoff 同次保存，查最新 `git log`。

`wails` 不在 PATH，使用 `/Users/amadeus/.go/bin/wails`。Wails 绑定、dist、node_modules、测试产物均被忽略；新会话修改导出 DTO 后先生成绑定。用于定位旧编辑器测试的基线导出位于 `tmp/harness/identity-35-baseline-YM4EZk`（忽略目录，不是工作树）；没有切换或重置当前仓库。
