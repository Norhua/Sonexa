# 003b55c3 执行计划

依据：[缘由、调查与用户选择](003b55c3-why.md)。起始提交 `003b55c34ebc6ec77793256f43670dc5bf63e858`。本轮任务在此同步，不修改原 tasks.md。

## 任务

- [x] 1. 阅读截图、当前状态/歌词投影及原规格和累计差异；核对本地 QQ 响应，确认来源状态选择，实施前建立 why/tasks。
- [x] 2. 分离歌曲搜索状态与后续预取状态，来源仅搜索失败时显示感叹号；补候选失败/通道失败/来源停止、零结果、预取启动/快照失败和重试隔离单测。
- [x] 3. 阅读投影隐藏空白独立附加行，保留原文空行及有效独立文本；补 QQ 空计时行 XLRC 往返及前端不可变投影测试。
- [x] 4. 浏览器回归覆盖一成功一失败 QQ 候选仍显示数量、翻译/罗马音切换、空行不出现说明、原始文本和采用保真；执行定向后端、前端全量单测/类型检查/构建及歌词 E2E。
- [x] 5. 自审、更新共用 spec-diff 和本次验证记录，检查差异并完成中文约定式提交。

## 验证与限制

- 首轮定向 `npm --prefix frontend test -- lyrics-presentation.test.ts lyrics-search.test.ts`：2 文件 / 31 项通过。
- `go test ./internal/provider/qqmusic -run 'TestQQEmptyTranslationMarkersSurviveXLRC|TestQQPrecisionAlignmentBeforeDelivery' -count=1`：通过；空标签 63300/74120 ms 在来源交付、XLRC 和回读中均保留，罗马音正文仍关联原文。
- 本地历史 QQ 响应只读重放通过，摘要见 why；临时诊断未纳入版本库。

### 完整回归

- `npm --prefix frontend test`：20 文件 / 105 项通过。
- `npm --prefix frontend run build`：Vue/TypeScript 类型检查及生产构建通过；保留既有超过 500 kB chunk 提示（非失败）。
- `go test -count=1 -race ./internal/lyricdoc ./internal/lyrics ./internal/provider/qqmusic ./internal/app ./internal/acceptance`：全部通过，覆盖文档、歌词业务、QQ、App 与架构边界。
- `go vet ./...`：通过。
- `SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- tests/e2e/lyrics.spec.ts`：Chromium/WebKit 共 22 项通过。
- `openspec validate improve-lyrics-normalization-and-ranking --strict`、`git diff --check`：通过。

### 自审与浏览器核对

- 来源函数不再读取候选错误或预取失败数量；歌曲搜索成功状态不被后续阶段错误覆盖。实际搜索失败、取消迟到结果、零候选、单源重试、旧事件保护仍有回归。
- 阅读过滤只排除独立附加空白，不 trim 非空正文、不删原文空行、不修改传入行/通道/时间。真实独立译文、多语种、ruby/声部、通道切换和同版本批准/采用测试继续通过。
- QQ 合成适配测试验证空标签在来源直接返回及 XLRC 回读时均存在；没有后端生产代码或协议改动，不新增获取请求。
- 浏览器测试验证成功/失败两张 QQ 卡片时来源显示 `2`、没有来源感叹号，失败卡片仍显示来源限制和通道失败且不能采用；阅读没有空行说明，原始文本和采用仍包含空标签，切换不新增获取/批准。
- 已打开检查 WebKit 截图：`frontend/test-results/lyrics-successful-song-sea-efe23-ly-empty-independent-extras-webkit/lyrics-search-count-and-empty-markers.png`；Chromium 同名截图亦由通过测试生成。截图为本地测试产物，不纳入版本库。
- 累计 D01/D03 与本轮实现一致，历史 why/tasks、原规划/主规格未修改；无生成绑定变更。

### 限制

本轮没有访问真实上游，没有 Wails 原生窗口人工验收；保留原任务 9.3/9.4 未完成状态。未改写用户歌词、未清库、未同步主 spec、未归档。历史响应仅作本地调查证据，测试使用短合成文本，不将整首歌词或原始响应纳入版本库。
