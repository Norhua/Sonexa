# 歌词搜索界面精简与三列预览：任务

起始提交：`5e6d41147c9f94d8306ac483bb92d8a8fd1ea98c`。需求见 [5e6d4114-why.md](5e6d4114-why.md)，累计规格差异见 [spec-diff.md](spec-diff.md)。本文件独立于原 change 的 `tasks.md`。

## 1. 界面精简

- [x] 1.1 移除目标提示和顶部来源诊断区域；保留紧凑来源重试和必要空态，不直接展示来源错误全文。
- [x] 1.2 移除预览顶部重复元信息、通道摘要与警告；技术错误不直出，用户操作失败保留简短、非占位反馈和重试路径。

## 2. 三列阅读

- [x] 2.1 在上方右侧增加「额外展示：歌词 / 罗马音」下拉，默认翻译，切换候选保留选择，重开默认复位。
- [x] 2.2 将阅读内容改为时间、原文、所选附加通道三列；保留独立/无时间附加行、未分类文本、空白、长行和文字安全边界，不改文档及采用内容。

## 3. 回归与记录

- [x] 3.1 补充显示投影单测及 Chromium/WebKit E2E：无重复信息/技术错误、三列位置、键盘切换、独立与缺失通道、原始文本保真、切换不重复获取/批准、重开复位、失败可恢复。
- [x] 3.2 执行前端单测、类型检查/构建及定向 E2E，检查差异，记录实际验证结果与未验证边界。
- [x] 3.3 完成累计 `spec-diff.md`（列明旧条款及归档同步要求），按项目要求提交中文约定式 commit。

## 验证记录

- `npm --prefix frontend run typecheck`：通过。
- `npm --prefix frontend test -- lyrics-presentation.test.ts lyrics-search.test.ts`：20/20 通过。
- `npm --prefix frontend test`：最终 19 文件、92/92 通过。首次全量执行时，`search-sources.test.ts` 两处旧断言仍要求原始技术错误文案而失败；只更新歌词搜索分支的文案断言后重跑通过，保留来源注册、禁用与重试的行为断言，封面分支不变。
- `npm --prefix frontend run build`：通过；Vite 仍提示部分产物超过 500 kB，未在本次界面范围内调整拆包。
- `npm --prefix frontend run test:e2e -- lyrics.spec.ts`：开发服务 Chromium/WebKit 共 16/16 通过。
- `SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- lyrics.spec.ts provider-capabilities.spec.ts`：生产构建预览 Chromium/WebKit 共 20/20 通过。
- `go test ./internal/acceptance`：通过，未增加来源专有前端/业务处理分支。
- `openspec validate improve-lyrics-normalization-and-ranking --strict`、`git diff --check`、本目录 Markdown 相对链接检查：通过。
- 人工查看浏览器截图：`frontend/test-results/lyrics-compact-lyric-searc-4fc28-lies-the-previewed-document-webkit/lyrics-search.png`、`frontend/test-results/lyrics-extra-channel-selec-9575a--every-channel-for-adoption-webkit/lyrics-three-columns.png`。确认预览元信息已移除、下拉位于上方右侧、三列独立及长行换行；截图由测试生成，可用上述命令重建，不作为真实歌词来源证据。

## 未验证边界

本次未进行真实上游请求、Wails 原生窗口人工验收，也未宣称完成原 change 的验收。原 `tasks.md` 的 9.3、9.4 保持原状；未修改主 spec、原 delta 或原规划文档，未执行归档。本次没有后端代码变更，未重跑全量 Go/race 测试。
