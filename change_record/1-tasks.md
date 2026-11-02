# 1：执行计划

- [x] 1. Review 状态机、实际准入决策、截图和相关主 spec；确认候选行操作范围，记录原始提示词。
- [x] 2. 建立严格的输入等价规则与自动候选证据优先级；修复 3253212 根因，保护版本/位置/跨组歧义。
- [x] 3. 实现事务级活动条目合并、原始输入聚合与已入库跳过；移除持久化文件路径/源行号，更新统计。
- [x] 4. 本地候选只用精确 track 绑定判断；候选动作改接受/跳过并校验目标，阻止迟到请求复活终态。
- [x] 5. 复核页显示去重原始输入，按条目恢复上游/地区/草稿，按实际结果显示列，增加搜索动画。
- [x] 6. 补充 Go 单元/绑定/并发和前端浏览器回归；生成绑定，执行构建及测试。
- [x] 7. 更新共享 spec-diff、记录验证结果与限制，检查 diff 并以中文约定式提交。

## 验证记录

- `go test -race -count=1 ./internal/...`：全部包通过；含并发提交仅一个活动条目、同 track 不同署名聚合、版本/专辑/位置不误合并、现存 track 核对、自动提交竞态重验、迟到搜索与终态保护。
- 最终补充未绑定本地收藏的严格完整文本去重，并重跑 `go test -race -count=1 ./internal/store ./internal/importer ./internal/app ./internal/library ./internal/acceptance`：全部通过；随后 Store 搜索来源持久化断言也通过。
- `go vet ./...`：通过。
- Go 定向：`matching`、`library`、`store`、`importer`、`app`、`acceptance` 全通过；实际 SubmitImport 在不同临时文件提交后删除源文件，复核仍展示两种原始输入且无路径。
- `/Users/amadeus/.go/bin/wails generate module`：成功。初次 PATH 中找不到 wails，`go run` 又因 CLI 可选依赖不在项目 go.sum 失败；找到既有 GOPATH 二进制后生成成功，未为 CLI 修改项目依赖。
- `cd frontend && npm test`：20 个文件、106 项通过。
- `npm run build`（包含 vue-tsc）：通过；现有主 bundle >500 kB 警告仍保留。
- `SONEXA_E2E_PREVIEW=1 npm run test:e2e -- review-actions.spec.ts provider-capabilities.spec.ts live-updates.spec.ts admission.spec.ts enrichment.spec.ts`：Chromium/WebKit **72/72** 通过，覆盖原始输入、接受/跳过、保留其他动作、各条目来源/地区/草稿、实际列能力、旋转动画、取消准入和搜索/刷新竞争。
- `SONEXA_E2E_PREVIEW=1 npm run test:e2e -- screenshot-review.spec.ts -g 'review removes|late review research'`：Chromium/WebKit **4/4** 通过；更新旧“来自文件”断言为原始输入，不再要求保留路径。
- 扩大跑 `screenshot-review.spec.ts admission.spec.ts enrichment.spec.ts` 曾为 19/22：两项旧文件位置断言已更新并重跑通过；另一个是 WebKit 编辑器“用户”徽标旧断言（`screenshot-review.spec.ts:107`），与 `openspec/verification.md` 记录的既有失败一致，未修改/弱化该断言，也未声称全量 Playwright 通过。
- `git diff --check`：通过。生成绑定、dist、浏览器报告均为忽略产物，不提交。

## 人工验证边界

本轮未运行真实 MusicBrainz/QQ 网络请求或原生 Wails 人工测试；浏览器为 Wails 替身，Go 使用空临时库与离线候选。主 spec 保持未修改，待用户清空历史数据、原生测试确认后再同步。大批量导入性能尚未专项测量；无法凭证据判定的任意别名仍保留复核，不能承诺猜出所有跨上游同曲关系。
