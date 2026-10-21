# 歌词搜索条件、来源反馈与候选展示修正：任务

起始提交：`3fccd7af3fe08593adba5a2b37c7fc1c203642ca`。需求见 [3fccd7af-why.md](3fccd7af-why.md)，累计差异见 [spec-diff.md](spec-diff.md)。本轮独立追踪，不改原 change 已有的 44/46 任务状态。

- [x] 1. 补充艺术家后的时长条件与输入校验，覆盖默认曲目/元信息回退、未知时长、修改提交、单源重试冻结快照和重开重置。
- [x] 2. 增加来源圆形总数/异常图标，覆盖搜索中、零结果、超过 Top 3、搜索失败、预取部分失败/停止、重试和迟到事件；无原始错误泄漏。
- [x] 3. 修复歌词艺术家繁简异写比较，保持默认权重、其他字段/元数据流程和真实名称不变；补充繁简/同名/不同人/缺失/团体署名、四来源适配到 App 的回归。
- [x] 4. 卡片右上角显示综合得分/150，悬浮和键盘提供分数及匹配说明，保留展开分项和未评分语义；阅读三列文字顶部对齐，验证长行不溢出。
- [x] 5. 执行相关 Go/race、前端单测/类型检查/构建、Chromium/WebKit E2E，更新累计差异与真实验证结论，检查并提交中文约定式 commit。

## 验证记录

- `go test -count=1 -race ./internal/...`：最终移除试用转换器依赖、改用官方字典后全量通过，包含架构边界、歌词业务、四来源适配与 App 测试。`go build ./...` 通过。
- 新增 `TestLyricsArtistScriptEquivalence`、`TestLyricsArtistOnlyScriptEquivalence`、`TestLyricsCreditDictionary`：覆盖周杰倫/周杰伦双向、陳奕迅/陈奕迅、張學友/张学友、孫燕姿/孙燕姿、真正不同人/缺失/团体署名、假名保护、歧义映射/繁体归并拒绝、并发及原始值不变；固定字典 SHA-256 防未审阅替换。
- 新增 `TestLyricsFourSourceArtistComparison`：四个真实适配器消费离线响应，经 Service→App 返回原始「周杰伦」，冻结查询仍为「周杰倫」；艺术家 20、匹配 100、高置信及提示一致。既有四源预取/采用完整管线仍通过。
- `npm --prefix frontend test`：19 文件、96/96 通过；`npm --prefix frontend run typecheck` 和最终 `npm --prefix frontend run build` 通过。保留已有 Vite 单块大于 500 kB 提示。
- `SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- lyrics.spec.ts provider-capabilities.spec.ts`：最终 Chromium/WebKit 22/22 通过。验证艺术家后时长条件、毫秒提交/重试冻结、候选总数 4（不是 Top 3）、0/1/异常/停止状态、重试恢复、未评分 `—/150`、悬浮/键盘/Escape、动态排名焦点、三列文字顶部和长行不溢出。
- 已查看 WebKit 的 `lyrics-search.png` 和 `lyrics-score-tooltip.png`（位于 `frontend/test-results/lyrics-compact-lyric-searc-4fc28-lies-the-previewed-document-webkit/`）：圆圈数字/感叹号、右上角分数、独立浮层提示与三列顶部对齐符合本轮目标；均为合成数据截图。
- 初次浏览器对齐断言对第三列使用包含 `<p>` 的整个 Range，测到了块级盒顶而非文字顶，导致两浏览器失败；改为比较首个非空文本节点的 Range 后通过，仍同时断言字号/行高一致，没有放宽对齐容差。
- 试用的 `longbridgeapp/opencc` 虽自身 Apache-2.0，传递依赖 `liuzl/cedar-go` 为 GPL-2.0，已从代码与模块图完全移除，`go.mod/go.sum` 与起始提交一致。最终仅保留 BYVoid/OpenCC `ver.1.1.9` 官方 34,627 字节的未修改字符字典及 LICENSE/AUTHORS，独立实现唯一映射比较，无新增运行时依赖或网络请求。上游标签与文件哈希见 `internal/lyrics/data/opencc/README.md`。
- 无 Go 导出 API/模型变动，无需重新生成 Wails 绑定。
- `openspec validate improve-lyrics-normalization-and-ranking --strict`、`git diff --check`、本目录全部 Markdown 相对链接检查通过；原规划与 9.3/9.4 未变。最后补充映射存在性检查、去除提示内重复「需确认」后，相关 Go/race、构建、前端全量单测/构建及双浏览器 22 项再跑通过。

## 验证边界

本轮默认离线回归与浏览器替身验证，不代表真实上游在线可用或 Wails 原生窗口人工验收，不勾选原任务 9.3/9.4。
