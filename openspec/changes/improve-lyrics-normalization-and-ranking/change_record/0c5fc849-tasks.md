# 预览下拉位置修正与时间统一规则调查：任务

起始提交：`0c5fc8497a7107c6a0f9caf9b24e88c7d3ce4a4e`。需求见 [0c5fc849-why.md](0c5fc849-why.md)，累计差异见 [spec-diff.md](spec-diff.md)。

- [x] 1. 将下拉移到右侧预览工具栏靠右，选项修正为「翻译 / 罗马音」，保留切换状态与文档/批准边界。
- [x] 2. 核对并记录当前规范化标准、精确时间关联条件和截图差值原因；添加毫秒精度差的合成回归，明确它验证现状而不是修复。
- [x] 3. 更新双浏览器 E2E，验证控件位于预览工具栏、文案正确、切换/关闭重开及无重复获取等行为。
- [x] 4. 更新累计 `spec-diff.md`，记录已实现的控件修正与尚未实施的时间对齐建议，执行测试、构建与差异检查后提交中文约定式 commit。

## 验证记录

- `go test ./internal/lyricdoc`：通过，含新增精确匹配/3 ms 差/6 ms 差三个合成场景；未改变后端生产规则。
- `npm --prefix frontend test`：19 文件、92/92 通过。
- `npm --prefix frontend run build`：类型检查和生产构建通过；保留既有的产物超过 500 kB 警告。
- `SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- lyrics.spec.ts`：最终 Chromium/WebKit 共 16/16 通过。首次因新工具栏的可访问名称包含「歌词预览」，旧模糊定位同时匹配容器与工具栏而出现 6 项失败；将预览容器定位改为精确匹配后重跑通过，没有删除原有行为断言。
- 检查 WebKit 截图 `frontend/test-results/lyrics-extra-channel-selec-9575a--every-channel-for-adoption-webkit/lyrics-three-columns.png`：下拉位于右侧预览工具栏靠右，与模式按钮同排；不再占搜索来源区。
- `git diff --check`、本目录 Markdown 相对链接检查：通过。

## 未完成范围

时间容差对齐**尚未实现**，已在 `spec-diff.md` 的 U01 记录现行规则、确定的错行原因、上游精度推测与后续建议。待与用户确认容差/唯一关联和保存规则后实施，不把合成测试当成缺口已解决。不改原 change 的在线/原生验收任务；本次未进行真实上游请求、Wails 原生窗口或全量 Go/race 验收。
