# 7 — 执行计划与验证

## 任务

- [x] 7.1 调整复核页面为搜索区、可滚动的全量候选、返回数量、底部条目信息与右下角导航；保留长内容可访问性。
- [x] 7.2 移除候选截断/展开状态、“无一匹配”入口及专用后端方法，重新生成 Wails 绑定。
- [x] 7.3 增加会话内复核游标，按条目 ID 恢复位置，处理队列变化、条目消失、空队列与迟到响应。
- [x] 7.4 更新并补充布局、全量候选、导航恢复及竞争回归测试；运行前端检查/构建、Chromium/WebKit 测试及相关 Go 测试。
- [x] 7.5 更新共享 `spec-diff.md` 与验证记录，检查变更并中文约定式提交。

## 验证记录

### 实现

- `frontend/src/views/ReviewView.vue`：条目信息移到底部，候选与长条目详情分区滚动，返回数量和右下角导航不滚走。移除展开草稿与“无一匹配”。
- `frontend/src/components/CandidateTable.vue`：直接绑定全部 rows；移除截断/展开 props、model、事件和底栏动作插槽。封面预取仍跟随全部候选及组件卸载清理。搜索页不额外增加底栏。
- `frontend/src/stores/review.ts`：轻量会话游标，不保存队列快照；刷新按 ID 保留，缺失按旧位置校正，空队列归零。只有有效的当前页面读取或导航写入，读取失败不写、卸载后的旧响应不写。
- `internal/app/bindings_review.go`：删除 `NoneMatched`。检查调用方后同时移除 `internal/store/queue.go` 中仅供该旧入口使用的“原因不变”常量/分支与空快照清身份分支；既有回归转为覆盖真正使用中的手动搜索修订 API，保留开始手搜清旧身份和手搜不改原因。
- `/Users/amadeus/.go/bin/wails generate module` 成功；生成的 `App.js` / `App.d.ts` 已不含 `NoneMatched`，生成目录按仓库约定在 `.gitignore` 内。

### 通过的检查

- `cd frontend && npm run typecheck && npm test`：24 个测试文件、137 项单元测试通过；新增游标 4 项测试覆盖首次/重进、ID 定位、消失回退、空队列。
- `cd frontend && npm run build`：通过。Vite 仍提示主 JS chunk 超过 500 kB，非构建失败，本轮未调整分包。
- 先运行相关 8 个 E2E 文件：Chromium/WebKit 共 148 项通过。
- 最终运行 `cd frontend && SONEXA_E2E_PREVIEW=1 npm run test:e2e`：在生产构建上执行完整 Chromium/WebKit 套件，**350 项全部通过**（1.9 分钟）。本机日志 `tmp/review-layout-e2e.log`，不提交测试产物。
- `go test ./...`：全部通过；清理存储层分支后再次执行全部通过。
- `go vet ./internal/app ./internal/store`：通过；改动 Go 文件已 gofmt。
- `git diff --check`：通过。源码及生成绑定没有 `NoneMatched` / `ReviewReasonUnchanged` 残留。

### 新增/更新的浏览器回归

- 21 条候选默认全量挂载，无“显示全部”“无一匹配”及截断文案；返回数量在切换到 0 条、重新搜索到 1 条后更新。
- 1920×1080、960×640 布局：搜索上方无条目信息，信息位于返回数量下方；上一条/位置/下一条位于右下角。滚动到第 21 行，底栏和导航不移动。
- 40 行长原始输入和失败信息可独立滚动，候选保留可用高度，重试和导航均可访问。
- 离开再进入保持第二条；前方插入/删除不换条目；原条目消失选原位置，末条消失退到新末条；空队列后新导入从第一条开始。
- 首次重新读取失败不抹掉游标，卸载页面的延迟响应不能覆盖新页面里已改选的游标。
- 已有后台刷新、草稿隔离、搜索竞争、准入取消、候选着色、同步目标及封面清理测试全部保持通过。
- 已查看 WebKit 自动截图：`frontend/test-results/review-actions-item-inform-c58d0-nchored-at-the-bottom-right-webkit/review-bottom-{1920,960}.png`。

### 未验证/边界

- 浏览位置仅在同一本次应用会话里保留；不承诺关闭/重启应用后恢复，也不新增搜索草稿与候选滚动位置的跨页面保存。
- 未在原生 Wails 窗口人工验收，未运行完整发行构建，未请求真实上游；既有 change 的平台和在线验收状态不变。
