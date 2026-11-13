# 5 — 执行计划与验证

## 任务

- [x] 5.1 提取共用候选专辑名展示：绿色 release 优先、橙色 group 次之、无状态保留原色；保留完整文本/类型提示及非颜色状态说明。移除复核来源的两个徽章和专用开关，接入复核、上游搜索、编辑候选。
- [x] 5.2 编辑搜索补齐精确本地状态批量投影；监听本地变化刷新候选，不重搜上游、不覆盖编辑草稿/预览，隔离切源/新搜索/关闭后的迟到响应。保留现有复核/搜索行为。
- [x] 5.3 补充单元与绑定测试，覆盖状态组合、独立原生组身份、同步专属数据、刷新清除和动作不变；更新 Chromium/WebKit 回归，验证三处颜色、明暗主题、长文本与草稿保护。
- [x] 5.4 同步 API/导入说明与共享 spec-diff，记录验证结果、适用范围及未验收边界；检查差异并使用中文约定式提交。

## 验证记录

### 通过的检查

- `go test ./internal/app -run 'Test(SearchAndEditorProjectExactReleaseState|ReviewReleaseMarkers|ResearchReturnsReleaseMarkers|RefreshSearchCandidates|EditorOfflineAndUpstreamPreview)' -count=1`：通过。
- `go test -race -count=1 ./internal/app ./internal/store`：两包全量通过，约 117.5s / 104.4s。
- `go vet ./internal/app ./internal/store`：通过。
- `cd frontend && npm test`：23 个文件、133 个测试通过。
- `cd frontend && npm run build`：类型检查和生产构建通过；Vite 仍提示主包大于 500 kB，本轮没有做包拆分。
- `cd frontend && npm run test:e2e -- review-release-state.spec.ts review-actions.spec.ts editor.spec.ts live-updates.spec.ts provider-capabilities.spec.ts table-refinements.spec.ts`：Chromium/WebKit 共 132 个用例通过。
- `cd frontend && SONEXA_E2E_PREVIEW=1 npm run test:e2e -- review-release-state.spec.ts playlist-performance.spec.ts`：针对生产构建的 18 个用例通过（含与上一轮重复的 12 个状态用例），验证实际 CSS 着色、候选列拖动、全文提示、大列表原有行为。
- 手动查看自动化生成的复核浅色、搜索深色、编辑候选深色截图，确认专辑名着色而非整行着色，来源徽章移除、布局不变。截图仅留在忽略目录 `frontend/test-results/`。
- `git diff --check`：通过；无生成绑定、dist、依赖或测试截图纳入提交。

### 首轮验证问题及修正

- 首次前端测试命令误在仓库根目录运行，缺少 package.json；改为 `cd frontend` 后运行成功，未修改依赖。
- 首轮浏览器定向运行 6 通过、4 失败：测试替身未给复核声明 `PrimaryType` 能力，却要求出现类型提示；本地徽章用例默认按浅色比较，而应用默认主题为深色。补齐替身能力并显式切换主题，未放宽生产颜色或能力逻辑。后续 132 / 18 项均通过。

### 实现范围与边界

- `CandidateAlbumName.vue` + `candidateReleaseState.ts` 是三处候选的共用展示入口；具体 release 命中优先，未知原生组不影响绿色，均未知不着色。状态提示也提供屏幕阅读器文本。
- 编辑搜索在 `bindings_edit_preview.go` 中追加一次只读批量本地投影，复用既有索引及状态应用函数；新的绑定测试覆盖同步专属/已入库、不同状态组合、搜索最终/增量结果、编辑只读性和删除后离线清除。
- 编辑候选订阅本地变化，不碰编辑请求 owner/草稿/所选预览；补刷新搜索期间的变化，并覆盖新搜索、切源、关闭的迟到响应隔离。
- 已查看编辑版本浏览和上游预览详情：它们不带候选的精确存在状态，本轮不新增它们的 DTO/查询，也不根据选中的歌曲候选猜测版本是否已存在。普通曲库/同步列表继续使用原有归属展示。
- API/导入说明已更新；共享 spec-diff 的修改 3 展示描述已替换为最终规则，并增加修改 5，避免归档时重新恢复旧来源徽章。历史 `3-why.md` / `3-tasks.md` 不改写。
- 没有改自动匹配、选版、准入或数据库结构。没有真实上游访问、原生 WKWebView 人工测试或 Windows/Linux 运行，也未运行整个仓库的全量 Go/E2E；原 change 55/59 的平台与在线验收保持原状。
