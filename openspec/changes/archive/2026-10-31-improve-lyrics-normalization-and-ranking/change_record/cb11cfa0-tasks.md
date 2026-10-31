# cb11cfa0 同步与归档任务

依据：[缘由与用户确认](cb11cfa0-why.md)、累计 [spec-diff.md](spec-diff.md)。不修改原任务 9.3/9.4。

- [x] 1. 加载 archive/sync skills 和 CLI 上下文，核对原任务、全部 change_record 决策及验证；取得带警告归档确认。
- [x] 2. 同步原 delta 与 D01–D10 至五份主 specs，逐项核对最终行为及未改动内容。
- [x] 3. 更新当前架构、已知缺口、README 引用及归档相关链接，保留历史证据和未完成验收。
- [x] 4. 执行 specs 校验、针对性离线回归、结构/链接/差异核对，记录实际结果。
- [x] 5. 同步成功后归档整个 change，核对任务、目录及历史保留，完成中文约定式提交。

## 验证与归档记录

日期：2026-10-31。本轮仅文档变更，未修改业务代码。

- CLI 产物 proposal/specs/design/tasks 均 done，原任务仍 **44/46**；此前 8 轮 change_record 共 **46/46** 已完成。本轮用户确认带警告归档，原 9.3/9.4 未勾选。
- `openspec validate --specs --json`：**27/27 通过**。保留与修改前相同的 **8 条**现有需求描述超过 500 字符的警告（editing 2、app-shell 1、cover-art 2、lyrics-search 1、provider-registry 1、qqmusic 1），无新增警告。不宣称全库 strict 无警告通过，也不为长度警告重构无关需求。
- 显式关闭全部 live/probe 开关、`GOPROXY=off` 后执行 `go test -count=1 -race ./internal/lyricdoc ./internal/lyrics ./internal/provider/qqmusic ./internal/provider/netease ./internal/provider/kugou ./internal/app ./internal/store ./internal/acceptance`：**8 个包全部通过**。
- `npm --prefix frontend test -- lyrics-presentation.test.ts lyrics-search.test.ts lyrics-ranking.test.ts lyrics.test.ts search-sources.test.ts`：**5 文件、45 项全部通过**。
- 逐项复核原三份 delta：非酷我需求与场景全部存在；D07 明确废止的 3 个酷我需求及异常时间场景不进入现行主规格。D01–D10 对照见 spec-diff 新增归档索引，冲突条款采用最后确认的规则。
- 自动结构核对通过：五份主规格标题及 Purpose 原样保留、单一 Requirements 区、无 delta 操作标题；旧主规格场景均保留，未涉及的需求块保持原文。
- 原 proposal/design/tasks、阶段实施/验证记录及 `.openspec.yaml` 与起始提交逐字节一致。原 delta 仅机械修正 Markdown 路径与一个指向缺失 delta 场景的锚点，行为/场景/决策原文未变；历史 why 只修截图路径。
- 按归档后的目录模拟校验：**143 个相对路径/锚点全部有效**（含历史本地截图引用，截图仍是本地忽略资源，不新增提交）。`git diff --check` 通过。
- 中间核对发现新文本引入两处长度警告，已精简本轮新增措辞而不改变约束，最终回到原有 8 条；发现历史 delta 的来源生命周期锚点指向不含该需求的 delta，已改为主规格链接。
- 未运行新真实来源请求、Wails 原生窗口、浏览器 E2E 或全量构建；此前证据不改写，待补验收继续列于 G17。

同步与结构核对完成后，已移动整个 change 到 `openspec/changes/archive/2026-10-31-improve-lyrics-normalization-and-ranking/`，保留全部 27 个历史跟踪文件及本轮 2 个记录文件。归档后无剩余 active change，主 specs 再验仍 27/27，实际路径/锚点再验 143/143。原任务仍为 44/46，不让原始 delta 再次机械覆盖最终主规格；归档不代表 9.3/9.4 已完成。本轮以 `docs(openspec): 同步歌词最终规格并归档变更` 提交，提交正文记录范围、原因、验证及剩余风险。
