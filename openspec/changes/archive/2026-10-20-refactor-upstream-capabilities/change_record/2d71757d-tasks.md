# 后续修复任务：2d71757d

起始提交：`2d71757d48f84dfae83eb0e6e720eaaa0fb9dd1d`。动机与用户原文见同目录 `2d71757d-why.md`。本文件只记录本次后续修复，不替代已有 change 的 tasks。

## 实施

- [x] 1. 将歌词占位文案解释迁入相关适配器，公共层只处理通用文本；测试网易云原文/附加通道占位、普通文本和非网易云同文案。
- [x] 2. 用来源声明的有序目录目标替代 `GroupFallback`；保留 CAA 回退及本地缓存行为，增加不同对象类型、多级目标和错误/取消停止的回归。
- [x] 3. 封面地区 UI 使用已选来源 `needsCountry`，泛化文案，并覆盖非 iTunes 地区来源。
- [x] 4. 两个前端搜索 store 从登记集合初始化/过滤选择，与页面共用列表；拒绝失效来源和未就绪请求，覆盖来源替换、加载失败、空集合、迟到响应及重试。
- [x] 5. 增强边界检查，防止已修复的占位规则、特定目录回退和前端固定名单/平台分支回归。

## 验收与记录

- [x] 6. 执行相关 Go race 测试、前端单测、类型检查与可用的定向浏览器测试，记录实际结果与未验证边界。
- [x] 7. 完成代码自审与 `git diff --check`；维护共享 `spec-diff.md` 的实际差异、冲突及同步建议，不改主 spec。
- [x] 8. 按项目要求提交中文约定式 commit，包含简介、修改详情及验证结果。

## 执行记录

- 修改前：工作区只有用户已创建的空 `change_record/spec-diff.md` 未跟踪文件；保留并作为共享差异记录使用。
- 后端：移除 `parseNetworkText` 和 `GroupFallback`；网易云负责占位归一化，CAA/iTunes/QQ 的 `CatalogKeys` 负责有效目录目标。自动解析、本地候选及入库图片复用共用该身份声明。
- 前端：新增 `lib/sourceSelection.ts` 维护来源列表、选择交集及重试校验；两个 store 在打开时加载来源并阻止未就绪请求；页面直接使用 store 列表，地区输入读取能力。
- 首次定向 Go 回归发现两处测试数据不符合新契约：新增 found 场景缺少数据库要求的图片规格、原本复用测试用 `mb-release` 假装合法 MBID。已分别改为非首选合法规格和有效 MBID，保留原断言目的；未把失败标成历史问题。
- 自审检索确认：`lyrics`/`covers` 生产代码无 `parseNetworkText`、`GroupFallback`、`release-group` 及网易云占位判断；前端两个业务 store/view 无固定上游 ID 字面量，新增边界测试守护。
- 主 spec、主架构、已有 change 的 proposal/design/tasks/specs、known gaps 与历史 verification 均未修改；差异记录集中在本目录。

## 验证结果

1. 定向前端：`npm --prefix frontend test -- tests/unit/cover-search.test.ts tests/unit/lyrics-search.test.ts tests/unit/search-sources.test.ts`，20/20 通过；`npm --prefix frontend run typecheck` 通过。
2. 全量 Go：显式关闭在线开关后执行 `go test -count=1 -race ./internal/...`，全部通过。实际环境覆盖：`SONEXA_LYRICS_LIVE=0 SONEXA_LIVE_COVERS=0 SONEXA_QQ_METADATA_LIVE=0 SONEXA_QQ_FALLBACK_DISCOVERY=0 SONEXA_QQ_RATE_LIVE=0 SONEXA_ITUNES_LIVE=0 SONEXA_QQ_PROBE=`。
3. 全量前端单测：`npm --prefix frontend test`，17 个文件、79/79 通过（含新来源选择及边界测试）。
4. 构建：`npm --prefix frontend run build`，类型检查和 Vite 构建通过；存在大于 500 kB 的 bundle 提示，不影响成功，本轮不扩展到拆包优化。
5. 浏览器：`SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- tests/e2e/cover-search.spec.ts tests/e2e/lyrics.spec.ts`，Chromium/WebKit 合计 24/24 通过；包含仅歌词登记来源和非 iTunes 地区封面来源。
6. `go vet ./...`、`git diff --check` 通过。公共 App API/DTO 未改，因此未重新生成绑定；构建、报告和绑定产物不提交。

## 未验证边界

- 未执行真实来源出网、在线速率或原生窗口验收，不关闭原 change 的在线未完成任务，不修改主 verification。
- 浏览器使用现有 Wails 测试替身验证 UI/会话/请求契约，不代表上游接口长期可用。
- 本文件随本轮修复 commit 提交；实际提交 hash 可由 `git log -- <本文件路径>` 查询，避免在提交内容内自引用 hash。
