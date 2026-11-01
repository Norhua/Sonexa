# 实施交接：4.6 已完成

## 当前状态

- 变更 `fix-track-identity-and-reduce-import-review`，repo-local，schema `spec-driven`。
- 已完成 1.1–4.6，**24/48**；CLI 已确认 4.6 完成。下一任务 4.7。
- 未同步主规格、未归档、未修改或清理用户应用数据。测试使用临时 SQLite、fixture 和 httptest；本轮未执行真实网络/原生验收或前端测试。

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

4.7 旧 Engine 说明/测试整理仍未完成，不能由本轮通过推定最终变更全量验收完成。

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

- 4.7 旧测试整理。4.4–4.6 输入优先筛选、兼容已收藏偏好和统一自动提交已完成。
- 5.x 问题等价证明、归并/拆出/关闭业务生命周期与版本化候选持久化。
- 6.x 多行 scope/hash、原子结果和租约；当前准备入口仍主要支持单行。
- 7.x 有界重评、独立问题统计、review/task 事件。
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
