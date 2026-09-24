# iTunes 候选与入库封面修复计划

日期：2026-09-24
关联：`revise-album-cover-workflow`；问题截图：`tmp/image/image14.png`、`tmp/image/image15.png`。
约束：不修改 `openspec/`，规格差异统一补充到 `change_record/cover-workflow-fixes-spec-diff.md`。

## Review 与根因

1. `covers.Resolve` 只允许 MusicBrainz/CAA 自动下载，iTunes 候选只有发行身份，`localFiles` 也只查 CAA 候选缓存；没有本地图时恒为 empty。手动封面搜索是另一条可工作的链路，因此出现「搜得到封面但歌曲显示 nil」。这不仅是接线缺失，也与现行“无 MB 只读本地”的规格直接冲突。
2. `itunes.candidateFrom` 把 artwork 地址改为 250 档；`ArtworkURL` 只识别 100/60/30，之后生成的 3000/1200/600/100 档全部仍是 250 地址。image14 的重复 250×250 标签与此吻合。
3. `ParseLookup` 没有保留专辑 artwork，封面服务无法从持久化的 iTunes 身份恢复封面。搜索候选的 artwork 也不会保存成歌集封面目录。
4. iTunes Search/Lookup 不提供演唱、作词、作曲等角色关系；截图已有年份、风格、位置和可点击署名，不能据角色为空认定详情补全没执行。不新增虚构角色或隐式跨来源匹配。

## 修改计划

- [x] 修复任意已构造 artwork 尺寸的再次转换，仅修改 URL 最后一个图片规格段，保留路径和查询参数；增加明确的 250 缩略图档。
- [x] 统一 iTunes 图片目录生成，Lookup 保留真实 artwork；封面服务按带地区的 collection 身份精确 Lookup（不按文字搜图、不绑定 MB），通过现有客户端复用响应缓存、配额与出网 gate。
- [x] 接通候选、入库歌集的 iTunes 自动解析：本地优先，缩略图 250、详情 1200，有限同图退档；共享目录/图片请求、持久化目录与空结果，错误不伪装无图。候选自动预览入库后复用，不创建额外收藏。
- [x] 保留显式采用及清除优先级、手动搜索预览不改变自动封面、地区/专辑隔离；无需前端传任意远程 URL。既有 iTunes 歌集只有身份也可恢复。
- [x] 回放真实「ツバサ / 4円 / side-C」搜索与 Lookup，验证可用详情入库、署名可点击、角色不臆造；前端补充 iTunes 角色能力说明。
- [x] 添加 URL、来源适配、解析器与实际 App/AssetServer 回归，覆盖并发、数据库重开/清缓存、故障/无图/禁用、本地采用与清除。运行 Go race/vet、前端测试/构建及浏览器测试。
- [x] 更新本计划的实施和验证结果，维护 spec-diff；检查 `openspec/` 零改动，按中文约定式提交。

## 实施结果

### 文件与行为

- `internal/provider/itunes/parse.go`：修正只识别 100/60/30 的尺寸替换，支持 250/1200 等尺寸再次转换；有限档位为 3000/1200/600/250/100。Search 和 Lookup 均优先 artworkUrl100，缺失时使用 artworkUrl60；Lookup 也可读取同专辑曲目提供的 artwork，不借其他专辑的图。
- `internal/provider/{types.go,itunes/itunes.go}`：详情保留 ArtworkURL；Lookup 校验 results 和 collection 身份。有效空响应与字段缺失、错误专辑严格区分。
- `internal/store/{itunes_artwork.go,admit.go}`：候选入库统一登记已知 artwork 目录；搜索/Lookup 复用同一图片身份和档位构造。不会同步下载，不给自动图写显式采用关系。已有 found 目录不反复重置规格状态。
- `internal/app/{bindings_upstream.go,app.go}`：搜索及复核行输出前登记已知图片，避免为每个候选额外 Lookup；启动封面服务注入精确 Lookup，读取当前注册表，设置变化后仍使用现有配置/配额。
- `internal/covers/{itunes.go,resolver.go,covers.go}`：新增 iTunes 目录入口；只有带地区的有效数字 collection 身份才能自动恢复。目录与图片分别合并请求，Lookup 共用元数据客户端和 gate，图片继续走封面池与 URL/私网限制。候选自动预览可复制到入库歌集目录。
- iTunes 自动缩略图按 250→100→600→1200→3000，详情按 1200→3000→600→250→100；无论标称档位为何，都只按解码实际尺寸保存和展示。CAA 顺序不变。
- `frontend/src/components/DetailDrawer.vue`：无 iTunes 参与角色时增加能力说明，保留可点击署名，绝不生成虚构 vocal/composer 等角色。

### 根因分类与边界

- 封面 nil：现有自动解析策略与此次期望冲突，已按用户请求新增 iTunes 精确身份例外，详见 spec-diff 第 8 节。
- 各档实际仍是 250px：真实代码缺陷，已修复。
- 参与角色缺失：实际 API 能力限制，不是本轮可以从 iTunes 补出的数据。实测详情已正确返回日期、风格、时长、位置、清单、署名；未增加登录 API、网页抓取或隐式跨来源匹配。
- 自动图片仍是可清理缓存；显式采用、本地导入和清除优先级不变。不迁移此前已有错误档位目录；如保留旧测试数据且已存在这样的目录，可显式重新搜索刷新。只有歌集身份但没有目录的旧记录支持自动恢复。

## 验证结果

- `go test -count=1 -race ./internal/...`：全部通过。
- `go vet ./...`、`go build ./...`：通过。
- `/Users/amadeus/.go/bin/wails generate module` 后 `npm run typecheck`：通过；生成绑定未提交。
- `npm test`：37 项通过；`npm run build`：通过（保留既有大 chunk 提示，未扩展为拆包任务）。
- `SONEXA_E2E_PREVIEW=1 npm run test:e2e`：首轮 Chromium/WebKit 106 项全通过。最终重新生成绑定并构建后的复跑 105 通过、1 项既有 WebKit 侧栏键盘新建标签测试瞬时失败（`image11-refinements.spec.ts:53`，未出现对话框），该项单独重复 3 次均通过；本轮 iTunes 用例两轮均通过。没有修改或放宽该既有测试。
- 真实网络：`SONEXA_LIVE_ITUNES=1 go test ./internal/app -run TestLiveITunesImportCovers -count=1 -v` 两次通过。通过真实 App/Search/Admit/AssetServer/RefreshTrackDetail，而不是仅 curl 或浏览器 stub：
  - 候选 `jp:1475065334`，歌集 `jp:1475065332`，日本 storefront。
  - 搜索候选：250×250 JPEG；入库列表：复用 250×250；详情：1200×1200 JPEG。
  - Lookup：`ツバサ / 4円 / Side-C / J-Pop`，2019、音轨 2、碟号 1、267720 ms、4 首完整清单；1 个可点击署名、0 个角色关系。
- 真实响应最小摘录：`testdata/golden/responses/itunes/{search_side_c.json,lookup_side_c.json,side_c.md}`。
- 运行日志保存在忽略目录 `tmp/itunes-cover-review/`；原生 Wails 窗口的人工点击/文件选择器验收未执行，不宣称完成原变更 tasks 8.4。
- 本轮只修改代码、测试及 `change_record/`，未修改任何 `openspec/` 文档。
