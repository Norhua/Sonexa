# 3：执行计划

- [x] 1. 阅读两张截图、共享变更记录和检索/评分/上游规格，追踪空候选链路。
- [x] 2. 用截图数据及通用版本差异建立失败回归，区分查询未召回、解析丢失与本地过滤。
- [x] 3. 分离人工候选可见性与自动身份置信；保留明确版本冲突保护，避免曲名/上游特判。
- [x] 4. 验证导入、搜索、复核改搜和编辑重匹配的共享行为，以及多发行位置、空旁证、不同版本等反例。
- [x] 5. 执行 Go 测试、race/vet 和必要前端验证；review 实现与回归风险。
- [x] 6. 更新共享 spec-diff、根因与验证记录，中文约定式提交。

## 验证记录

- 修复前截图回归失败，所有候选 `TitleMatch/CreditMatch/AlbumMatch` 均为不匹配；确认不仅是查询问题。
- 一次真实 MusicBrainz recording 短语查询返回 3 个 recording × 4 个发行位置，共 12 条 track。目标 CD track 为 `d2895728-1e49-42e6-81fd-75e1be292099`。原始响应保存在金样例 `testdata/golden/responses/musicbrainz/search_version_recall.json`，不是手造上游结果。
- `TestMusicBrainzVersionRecallAcrossEntrypoints` 用真实响应、HTTP 桩、临时 SQLite 运行真实 SubmitImport/Runner、SearchUpstream、ResearchReviewItem、SearchEditCandidates，4 个目标位置均保留（包括罗马字标题）；未自动入库，人工接受可精确绑定 CD 第 2 轨。缓存、快照重读、原始输入、警告及本地状态刷新断言通过。
- `go test ./internal/matching ./internal/lyrics ./internal/acceptance ./internal/textmatch` 通过。原 Tetra./世界中の雨 回归改为保留其他版本供人工确认，自动决策仍为原版/原版集合；歌词候选策略保持严格。
- `go test -race -count=1 ./internal/...`：全包通过；最终调整版本分类的配置表边界后，`go test -race -count=1 ./internal/matching ./internal/app ./internal/lyrics ./internal/textmatch` 再次通过。
- `go vet ./...`：通过。
- `/Users/amadeus/.go/bin/wails generate module`：通过。初次 PATH 中找不到 wails，改用本机既有安装绝对路径；未手工修改或提交生成物。
- `cd frontend && npm test`：20 文件、106 项通过；`npm run build`（vue-tsc + Vite）通过，原有主 bundle >500 kB 警告仍在。
- `SONEXA_E2E_PREVIEW=1 npm run test:e2e -- review-actions.spec.ts admission.spec.ts`：Chromium/WebKit **58/58** 通过；新增 4 个发行候选可见、人工确认说明保留且必须显式选择的回归。
- 修复后真实 `musicbrainz.Provider + matching.Engine` 使用截图三字段验证：1 req/s、3 次发送预算、关闭重试；实际 **1 次锚定查询** 返回 8 条 ReviewOnly 候选（4 个目标演唱位置 + 4 个同署名伴奏位置），目标 CD 第 2 轨可见，AutomaticCandidates 为空。连同前期抓取金样例，本轮真实 MB 总共 2 次请求。未为了测试增大限速或修改用户设置。
- 字典复用项目已有副本，仅迁移目录，不新增转换器依赖；SHA-256 和唯一映射/歧义反例通过，`go.mod/go.sum` 未变。

## 验证边界与更新方式

测试使用临时目录与 SQLite，不读取或修改用户应用数据库。浏览器测试使用 Wails 替身，未运行原生窗口人工测试。真实请求验证当时上游行为，不保证服务未来可用性或每次排序/数量完全一致；不保证任意翻译或完全不同名字可自动认出。

旧空候选是历史过滤后快照，不自动出网重做。运行新版后在该复核条目点击“重新搜索”；原始输入无需改写或重导。搜索出现伴奏等其他版本是有意保留供人工确认，不是自动判为同曲。
