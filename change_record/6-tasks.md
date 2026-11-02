# 6：执行计划

- [x] 1. 阅读截图与生命周期实现，定位已确认关系被原始版本差异否定及未知位置补全误失效。
- [x] 2. 写失败回归：人工接受 ReviewOnly 具体 track 后立即重导，以及未知位置补全后重导。
- [x] 3. 修正确认复用的比较基线、上下文持久化和位置补全失效策略；不改自动选版。
- [x] 4. 补通用反例、真实 App/准入/重开/迁移回归，运行 Go/race/vet 和原选版测试。
- [x] 5. 更新 spec-diff 和验证记录，检查 diff 后中文约定式提交并说明适用边界。

## 验证记录

- 修改前回归：三条均失败。Store 立即重导 `handled=false`；未知位置补全后确认行数 0；真实 App/MB 金样例接受后重导 `review=1, skipped=0`。
- 修复后 `go test ./internal/store ./internal/app -run 'TestConfirmedVersionMappingReimport|TestConfirmationSurvivesPreviouslyUnknownPositionEnrichment|TestMusicBrainzVersionRecallAcrossEntrypoints' -count=1`：通过。
- 真实 MB 金样例：首次导入仍有 8 条 ReviewOnly、自动集合为空，人工选择 target=`d2895728-1e49-42e6-81fd-75e1be292099` 后重复 SubmitImport 和补全后重导均 skipped=1；清 HTTP 缓存后仍不新增请求，原始输入和收藏标题/绑定保持不变。
- Store/纯规则回归：完整版本描述 A→B（同为 Ver）、伴奏、当前值单独改版、快照单独改版、已知位置改变/清空、时长冲突、换绑/改发行身份/删除不能复用；新输入提出不同版本/艺术家/专辑/位置不能借旧关系跳过。未知位置补齐、无冲突非版本注释、普通显示改名可保留确认。
- 接受后的合并输入上下文都保存；同一输入确认至两首收藏时仍歧义；8 个并发重复处置只保留一次结果；注入提交失败不留下 skipped 或新确认行，重试可成功；重开数据库仍可复用。
- v3→v4 真实迁移测试：新增上下文保持 NULL，不伪造接受时事实；新 trigger 允许 NULL→1，1→2 删除关系；旧缺上下文记录不能据本轮规则扩展成已批准版本。
- `go test ./internal/matching ./internal/store ./internal/app ./internal/library ./internal/importer ./internal/acceptance`：通过。
- `go test ./internal/app -run '^TestSekaijuuNoAme' -v -count=1`：通过，空库仍选择 release `07088ba6-...` / track `5e5f141f-...`，已有发行及总数/国家对照结果不变。
- `go test -race -count=1 ./internal/...`：全包通过；`go vet ./...`、`git diff --check`：通过。

## 范围与升级说明

- 本轮无前端及公开绑定变更，不生成 Wails 绑定、不运行浏览器测试；只修改既有确认关系复用及编号迁移，不修改 AutomaticCandidates、选版顺序、高置信或 ReviewOnly 规则。
- 没有公网请求，不读取/修改用户运行中的应用数据；截图样例使用既有真实 MB 响应离线重放，未执行原生人工测试。
- 已有但缺 target_context 的历史确认、被旧补全触发器删除的确认或从搜索独立加入而无原始输入关系的收藏，不盲目生成授权；必要时在新版明确接受/跳过一次，以后重复导入复用新确认。无确认且输入未指定版本仍复核是保留的安全边界。
