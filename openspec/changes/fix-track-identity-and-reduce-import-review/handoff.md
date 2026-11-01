# 实施交接：完成任务 2.5 后停止

## 当前状态

- 变更：`fix-track-identity-and-reduce-import-review`，repo-local，schema `spec-driven`。
- `tasks.md` 已完成 1.1–1.7、2.1–2.5，共 **12/48**；下一项为 **3.1**。
- 用户要求本轮收尾后停止，在新对话继续；本轮没有开始 3.1。
- 规划、任务及 10 个增量规格仍是实施依据。本文件只记录交接事实，不替代规格或完成标准。
- 未同步主规格、未归档、未访问真实来源、未改动或清理用户应用数据。

下一轮可使用：

```text
/opsx-apply fix-track-identity-and-reduce-import-review
请先读本变更 handoff.md，从任务 3.1 继续。
```

## 已落地内容

### 第 1 组：输入证据

- `internal/matching/profile.go`、`metadata_*.go`：版本化输入 profile、原文、字段等级、可靠异写、完整录音/发行限定、角色/CV/合作署名及独立自动证据检查。
- 旧 `CompareTitle`、`CompareCredit`、展示评分和歌词策略未改动；新规则不能当成概率。
- `internal/importer/profile.go`、`runner_profile.go`：年份、位置、时长的值/缺失/失败与精度，明确单位优先，不凭数量级猜身份时长。
- App 入队保存 profile，Store 读取后传至 Runner/library。
- 任务 1.5 提前落地了 3.1 的两个依赖：`import_item.input_profile` 列与 `track-identity-v2` 新库代际。**问题表、输入整数修订等 3.1 其余工作未完成**。旧代际拒绝打开，不自动转换或删库。

### 第 2 组：来源身份与接受准备

- `matching.CandidateResolver` / `CandidateSources`：无网络身份检查与有界资料加载；精确、未解析、无效、不支持、位置歧义和资料不足分别表达，来源差异留在适配器。
- `internal/app/upstreams.go` 集中装配并发布；library 消费注入。非 App 调用可按 registry 中的能力接口发现来源，无来源 switch。
- `provider/musicbrainz/identity.go`：只在指定发行内解析 `recording@release`；检查完整清单、真实 track、碟序及位置，重复录音、多发行、缺失位置、部分清单不选首项。
- MusicBrainz 决策请求内按发行复用同份原生响应；`fetchRelease` 与原详情共用 HTTP 缓存、配额、门禁、重试和取消。
- iTunes/QQ 精确身份检查不出网；不足或未实现的决策清单返回明确状态。
- `internal/library/identity.go`：`PrepareCandidate` 解析目标；`CandidateSeed` 和直接 `PrepareAdmission` 拒绝未解析/无效身份或发行关系不一致。自动选择前按来源批量解析非精确候选，对解析目标再核验输入。
- `Selection.ResolvedCandidates` 交给 Runner 保存展开后的候选。尚未完成后续完整的输入优先自动选版。
- `App.PrepareAdmission` 返回准入计划，或仅带 `resolution` 的候选展开结果；展开不是计划，不能提交。搜索及复核 store/view 已接收展开并允许明确选位置；候选 Note 标注第几碟/轨。
- 直接 `AddToLibrary` 与 `AcceptCandidate` 共用准备入口和动态出网门禁；精确候选仍无需额外出网，合成候选可能补取必要资料。
- 说明位于 `internal/matching/README.md`；旧“接受绝不出网”注释已修正。

## 本轮测试修正

- App 的旧 MusicBrainz 假标识改用测试专用 `fixtureMBID` 生成合法、稳定 UUID，不放宽生产校验。
- library/importer 的测试来源显式实现身份能力，不让测试依赖生产绕过路径；App 原生来源 fixture 仍走实际适配器。
- `TestAdmissionNeverBorrowsImportValues`：完整发行身份允许发行实体存在，但未知标题/署名不可借输入填充。检查底层 `*_current` 为 NULL，来源快照 `Field.IsKnown()` 为 false；读取视图的 `COALESCE` 空字符串与快照 Go 零值都不是有值事实。
- 本轮首次加的快照断言仅接受字面 `unknown`，遇模型零值失败；随后依用户已确认的缺失语义调整为现有 `IsKnown()`，重新验证通过。没有改生产缺失值语义。

## 实际验证结果

已执行并通过：

```sh
go test ./internal/library ./internal/importer ./internal/app ./internal/acceptance \
  ./internal/matching ./internal/lyrics ./internal/store \
  ./internal/provider/musicbrainz ./internal/provider/itunes ./internal/provider/qqmusic
/Users/amadeus/.go/bin/wails generate module
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run build
SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- identity-resolution.spec.ts admission.spec.ts
openspec validate fix-track-identity-and-reduce-import-review --strict
git diff --check
```

- 前端单测：**21 个文件、107 个测试通过**。
- Playwright：**14/14 通过**，含 Chromium/WebKit 的搜索/复核多位置展开、仅明确第二位置提交，以及既有准入确认/取消回归。
- Go 新 App 用例覆盖：解析后命中已有 track、多位置无计划、拒绝直接合成 seed、查询文本不进入来源事实、旧加入入口不绕过出网门禁。
- 构建有大于 500 kB 的 chunk 提醒，构建成功；未作为本变更扩展范围处理。
- 本轮**没有执行 `go test ./...`、全量 race、全量 Playwright、原生 Wails 人工验收或真实来源网络验收**。浏览器使用 Wails 替身；不能替代 Go 入口与真实网络证据。9.4/9.5 保持未勾选。

## 下一轮重点

1. 从 3.1 新库结构继续，考虑已提前加入的 profile 列和代际，不重复设计历史升级。
2. 3.2–3.6 仍未实现：`ResolveLocal` 仍有旧 ISRC/文本拥有判定，`MergeCandidate`/`MarkMerged`、merged 状态仍存在；候选动作、精确 skip 事务及刷新封面保护需按计划替换。当前身份解析通过不等于这些已修复。
3. 4.x 仍保留旧本地重复短路、HighConf 选版与跨组逻辑，尚无完整本地召回/输入优先选版/自动精确跳过。不要将 `resolveSelectionIdentities` 的接入误称为完整流水线交付。
4. 5.x–8.x 的独立问题、成员证明、整数修订、CAS、多行批准、租约、重评、事件与 UI 尚未实现。当前展开只供本次交互，持久化及迟到响应保护须随这些任务验证。
5. 任务仍按 CLI 返回的 sourcePath/line 核对、逐项勾选并重跑 apply instructions；阶段提交使用中文约定式说明。

## 本机工具提示

- `wails` 不在当前 PATH；可执行文件为 **`/Users/amadeus/.go/bin/wails`**，无需重新安装。
- Wails 绑定、frontend/dist、测试报告为忽略的生成产物，不提交。修改 Go 导出 DTO 后先生成绑定再做前端验证。
- 既有阶段提交：`02fb3fb`（1.1–1.3）、`8f11c1a`（1.4–1.7）、`a5ac487`（2.1–2.4）。本文件与 2.5 收尾一起提交；以 git log 查看最新提交。
