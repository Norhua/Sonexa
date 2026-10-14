# 复核队列动作完成后即时刷新修复计划

- 起始提交：`ebde37a93f5f967c0625eccda71a7dad442ac4a8`
- 日期：2026-10-14
- 背景：测试发现复核队列点击“移出队列”后当前页面不更新，离开再进入才显示已移出。本次仅修改实现、测试及 `change_record`，不修改任何 OpenSpec 文档。

## 排查结论

- `frontend/src/views/ReviewView.vue` 共用的 `act` 将所有操作返回的 `null` 当成准入确认取消，跳过队列与任务计数刷新。
- `DiscardReviewItem`、`MergeCandidate`、`NoneMatched`、`RetryReviewItem` 均为仅返回 error 的 Go 方法；Wails 成功返回的空结果可为 `null`，不能据此判断用户取消。
- `admission.run` 的 `null` 则确实表示用户取消准入确认，需要保留不刷新、不重置当前候选与搜索草稿的行为。

## 修改计划

1. 为共用动作处理引入明确的取消信号：普通接口成功即重新加载队列和侧栏任务计数，仅准入确认显式取消时跳过刷新。
2. 保持移出后逐条浏览位置校正和最后一条移出后的空态；失败时显示错误并保留当前条目，不将失败当成成功。
3. 新增有状态的浏览器回归，模拟 Wails 成功返回 `null`，覆盖连续移出、移出末条、清空候选、合并、重试、操作失败及准入确认取消/完成。
4. 在共用的 `change_record/spec-diff.md` 中记录核对结果：本次修复恢复复核队列既有处理动作与浏览语义，不引入新的产品规格差异。

## 验证与交付

- 先运行新增用例证明原实现会遗漏刷新，再完成修复并运行 Chromium/WebKit 相关回归。
- 运行前端单元测试、类型检查及构建；检查 `git diff --check` 和 OpenSpec 零改动。
- 在本计划追加完成情况与真实验证结果，以含简介和正文的中文约定式提交提交本次修改。
- 测试使用隔离浏览器及 Wails 替身，不操作用户应用数据库或真实上游网络；原生窗口最终体验由用户继续验收。

## 完成记录

- 共用动作处理仅把显式 `false` 视为取消；接受与无需匹配将 `admission.run` 的取消结果映射为该信号。普通 Wails 接口成功返回 `null` 或 `undefined` 均刷新队列与侧栏计数，原有错误处理和位置校正不变。
- 核对项目所用 Wails v2.10.2 的回调序列化与前端 Promise 分发：无业务返回值成功时 JSON `result` 为 `null`，与排查原因一致。
- 新增 `frontend/tests/e2e/review-actions.spec.ts`，通过独立后端状态和克隆返回值模拟真实桥接，覆盖 10 个场景；避免共享对象修改导致测试虚假通过。
- 已在唯一的 `change_record/spec-diff.md` 追加本次核对记录，明确无新增规格差异；未修改后端、数据库结构或 OpenSpec 文档。

## 验证结果

- 修复前：`cd frontend && npm run test:e2e -- tests/e2e/review-actions.spec.ts --project=chromium --grep 'discard returning null'` 按预期失败，移出后页面仍显示“2 条待处置”，未更新为 1 条，复现用户问题。
- 修复后：`cd frontend && npm run test:e2e -- tests/e2e/review-actions.spec.ts tests/e2e/admission.spec.ts tests/e2e/provider-capabilities.spec.ts tests/e2e/list-edges.spec.ts tests/e2e/screenshot-review.spec.ts tests/e2e/cover-updates.spec.ts`：Chromium/WebKit 共 58 项通过，包括新增 20 项和相关既有回归。
- `cd frontend && npm test`：10 个文件、48 项单元测试通过。
- `cd frontend && npm run build`：TypeScript 检查和生产构建通过；仍有主包超过 500 kB 的既有提示。
- `cd frontend && SONEXA_E2E_PREVIEW=1 npm run test:e2e -- tests/e2e/review-actions.spec.ts`：最终生产构建的 Chromium/WebKit 共 20 项通过。
- `git diff --check`：通过；`git diff --name-only -- openspec` 无输出。
- 验证边界：未运行原生 Wails 窗口或接触用户数据；本次仅修改前端动作处理，未运行 Go 全量测试。
