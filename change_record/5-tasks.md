# 5：执行计划

- [x] 1. 阅读截图、共享候选表格、复核页面及现有规格/测试，创建修改前记录。
- [x] 2. 来源列只保留徽章；入队原因移至标题同一行，使用无边框灰色小字；保留实际准入证据。
- [x] 3. 更新人工候选展示回归，增加原因布局、长文本、无原因及证据保留覆盖。
- [x] 4. 运行前端构建、单测及 Chromium/WebKit 相关回归，检查 diff。
- [x] 5. 更新共享 spec-diff 和测试记录，完成中文约定式提交。

## 验证记录

- `cd frontend && npm run build`：通过（含 `vue-tsc --noEmit`）；Vite 提示主包压缩后超过 500 kB，不影响构建，本轮未调整打包策略。
- `cd frontend && npm test`：20 个文件、106 项单测通过。
- `cd frontend && npm run test:e2e -- review-actions.spec.ts admission.spec.ts screenshot-review.spec.ts visual-controls.spec.ts provider-capabilities.spec.ts`：Chromium/WebKit 共 82 项通过，无页面运行时错误。
- 回归覆盖来源列只含上游/本地徽章、不显示 note，人工候选仍可逐条接受、本地候选仍跳过、上游搜索已在库候选仍可跳转；确认原始输入、搜索草稿、请求竞争和动作后刷新不回退。
- 布局回归覆盖亮/暗主题、1440/960 宽度、长短标题和原因；断言同行位置、12px 灰色字体、无原因边框、长文本省略及完整 title、状态标签不重叠。无原因、空/无效证据无空区域；新增发行的组、已有/拟加入发行与判组依据保留，确认取消行为不变。
- 已读取核对 WebKit 浅色标准布局、窄窗口长文本和人工候选来源列截图；截图由 Playwright 生成在 `frontend/test-results/`（不入库），相关用例重跑可复现。
- 首次构建/单测命令误在仓库根目录执行，因根目录无 `package.json` 返回 ENOENT；改为上述 `cd frontend` 后全部通过，与代码无关。
- `git diff --check`：通过。仅修改前端展示、相关测试及轻量记录；不修改后端、数据库结构、生成绑定或主 spec。不请求真实上游，不读取用户运行中的应用数据库，未运行原生 Wails 窗口人工验收。
