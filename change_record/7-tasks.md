# 7：执行计划

- [x] 1. 阅读截图、现有 change_record、主规格与 MB 查询/候选过滤路径，登记本轮修改目的。
- [x] 2. 有界获取真实 MB 响应，逐级区分查询漏召回、解析丢失与本地过滤；新增修复前失败回归。
- [x] 3. 修复通用根因，保持上游差异在 provider 包、人工召回与自动身份隔离，不添加样例特例。
- [x] 4. 覆盖通用正反例及真实 provider → Engine → 应用入口/快照，验证旧版本召回及选版规则不退化。
- [x] 5. 运行 Go 测试、race/vet，更新共享 spec-diff 与证据，检查后中文约定式提交。

## 验证记录

- 真实公开 MB 取证共两次，UA 携带项目 URL，间隔超过一秒：原始锚定 count=0；仅曲名 count=76、第一页 25 recordings、36 tracks。原始响应与校验和见 `testdata/golden/responses/musicbrainz/search_portamento.md`，未加工或裁剪响应内容。
- 修改前 `go test ./internal/app -run '^TestMusicBrainzAlbumRecallAcrossEntrypoints$' -count=1`：失败，实际 provider 已找到目标而 Engine 两级之后 `Scored:[]`。目标严格评分 TitleMatch=MatchExact、CreditMatch=AlbumMatch=MatchNone，证实本地旁证误过滤，不是分页或解析问题。
- 修改后相同回归通过：只留下两个不同发行、相同第 2 碟第 4 轨的位置，自动集合为空；原始输入及上游标题、艺术家、发行名、ID、载体保真。导入、复核改搜、上游搜索、编辑重新匹配、候选快照重读与刷新一致；人工接受目标绑定正确。
- 旧空快照回归：用 MarkReview 保存空结果后，GetReviewQueue 不自动出网；ResearchReviewItem 使用同一原始 HTTP 缓存，返回两条人工候选。整个四入口测试只有两次本地 HTTP 请求，无额外 artist/detail 请求。
- `go test ./internal/matching ./internal/app -run 'TestMetadataAlbum|TestMusicBrainzAlbumRecallAcrossEntrypoints|TestMusicBrainzVersionRecallAcrossEntrypoints|TestSekaijuuNoAme' -count=1`：通过。日/中/韩无空格长描述、全半角及拉丁词边界正例；短名、泛称、多词泛称、编号、拉丁词内子串、非连续文本、版本/豪华版、截断、空字段及多个弱旁证叠加反例通过。已有严格可展示候选 Score 完全不变。
- 第 3/6 轮版本候选与已确认重导、第 4 轮 11 发行稳定选版与对照均通过，本轮未修改自动选版或去重。
- `go test ./internal/...`、最终 `go test ./...`：全包通过。
- `go test -race -count=1 ./internal/...`：全包通过。
- `go vet ./...`、`git diff --check`：通过。

## 范围与使用说明

- 仅增加共享元数据人工召回旁证，未改变 provider API、数据库、前端或公开 Wails 绑定；不生成绑定、不运行浏览器/原生窗口测试。
- 未读取/修改用户运行中的应用数据库。取证后所有回归均用公开响应离线重放和空测试库。
- 已有空候选是过滤后的快照，安装新版后点击“重新搜索”；不需要清库，也不需要人为把艺术家改成日文。若上游原始缓存确实没有目标，则仍受原有查询范围和缓存策略约束；本轮不扩大分页或猜测完全不同的专辑名称。
- 三份记录保持在根目录 `change_record/`，未创建 OpenSpec change、未修改主 spec。
