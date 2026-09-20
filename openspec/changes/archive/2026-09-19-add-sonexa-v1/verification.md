# v1 归档与主规格同步核验

> 历史报告：以下结果属于 2026-09-19，不表示当前验证状态。当前报告见 [文档与实现核对](../../../verification.md)。

日期：2026-09-19。起始分支 `fix-v1-shell-and-import`，业务实现基线 `5f69398`；归档基线提交 `f882824`（哈希已按后续生成文件历史清理映射更新）。本次仅修改文档与 OpenSpec 项目上下文，不修改业务实现。

## 执行依据与用户确认

按 `openspec-archive-change` 与内联 `openspec-sync-specs` 流程执行：读取 archive 指令与 status，确认 schema 为 spec-driven、四个规划产物完成、任务 182/182；从 status 的具体 specs 路径读取全部 22 份增量，获取 specs 指令快照后写主规格。

发现主 specs 原为空，且偏差记录仍列未修复链路，已向用户列明新增范围与风险。用户明确选择「同步后归档，保留已知缺口」。因此归档是经确认的历史封存，**不是无缺陷的发布验收**。

原始 174 条需求、305 个场景先全部同步；导出与设置 Purpose 因少于 50 字符未通过主规格严格校验，补全 Purpose 后 22/22 通过。归档前逐份比较全部需求正文，确认无遗失，再移动到 `changes/archive/2026-09-19-add-sonexa-v1/`。归档中的原产物随后逐字节对照业务基线，全部未变；只新增归档说明。

归档后的核验采用完整性、正确性、一致性三个维度，直接面向主 specs、现行设计和已知缺口，不重新打开归档变更，也不声称对已归档名运行活动变更 apply/status 得到了成功结果。

## 核验结论

| 维度 | 结论 |
| --- | --- |
| 完整性 | 22 份主规格均已修订；现为 201 条需求、374 个场景。每条需求至少一个场景，没有残留增量操作标题或重复需求名。偏差第一至二十七节的最终裁决映射见 README。 |
| 正确性 | 已按生产代码及现有回归核对主要调整，Go race、前端单元、双浏览器生产构建回归通过；没有逐个动态执行全部 374 个场景。G01–G11 明确保留，不能给出「所有要求已实现」结论。 |
| 一致性 | 主 specs 与现行设计采用 Volt、曲目/歌集实体、完整 CV 身份、单选搜索、严格分流及最新交互；归档旧方案保持历史且有明确指向。config 注入当前技术栈和缺口边界。 |

### 高风险未完成项

1. **G01 用户锁**：`WriteDetailColumns` 未全面检查曲目/歌集锁，`GetMetadata` 未带出歌集来源/锁；需生产详情写入路径回归。
2. **G02 灰色行归属**：加入入口未传完整发行与位置，不能仅以 track 标识可命中清单认定归属正确；需歌集计数与补全工作集回归。
3. **G03 编辑持久化**：year 未映射日期、重匹配仍可能返回旧身份、清空立即落盘、保存非原子；需取消与失败场景回归。

它们已由历史偏差确认并经本轮静态核对保留，不因为文档同步降低要求。G04–G11 的参数、封面、字段映射、导出/规模、歌集配套、禁用态、iTunes 生命周期与人工处置见 [known-gaps.md](../../../known-gaps.md)。

## 本次实际执行的验证

| 命令/检查 | 结果 |
| --- | --- |
| `openspec validate add-sonexa-v1 --strict --no-interactive`（归档前） | 通过；仅验证原规划结构 |
| `openspec validate --specs --strict --no-interactive` | 22/22 通过（基线同步后及偏差修订后均执行） |
| `openspec validate --archived --strict --no-interactive` | 1/1 通过，归档任务无未勾选项 |
| `openspec list --json` | 活动变更为空 |
| `go test -count=1 -race ./...` | 11 个测试包通过；原有 scratch 包无测试，未执行其 main |
| `go vet ./...` | 通过，单独复跑确认 |
| `cd frontend && npm test` | 5 个测试文件、34 项通过 |
| `cd frontend && npm run build` | vue-tsc 与 Vite 成功；保留 >500 kB chunk warning |
| `cd frontend && SONEXA_E2E_PREVIEW=1 npm run test:e2e` | 39 场景 × Chromium/WebKit = 78 项通过 |
| `~/.go/bin/wails build` | Wails v2.10.2，darwin/arm64 打包成功，无 time.Time 绑定告警 |
| 需求结构/归档原文/本地文档链接检查 | 通过；归档原产物逐字节等于起始业务基线，主规格没有增量操作标题 |
| 文档差异空白检查 | 通过；原有生成文件尾随空白不在本次提交范围 |

Vite 主包为 911.76 kB，gzip 229.97 kB；CSS 102.12 kB。不能写零告警，也未测原生启动性能。Wails 产物为 `build/bin/sonexa.app`，没有为测试启动读取用户数据库的原生应用。

## 代码/回归证据索引

以下文件路径已随后续目录整理更新为 internal/app；本报告的测试结果与工作区描述仍为归档时的历史证据，不冒充目录整理后的新验证。

| 核对范围 | 生产入口及回归 |
| --- | --- |
| 入库与缺失值 | `internal/store/admit.go`、`internal/app/bindings_upstream.go`、`internal/app/bindings_review.go`、`internal/app/admit_test.go`；完整相等测试不扩张至灰色行未传入字段 |
| 严格分流、三级阶梯、两道展示闸门 | `internal/matching/{variants,pipeline,confidence,dispatch}.go` 及对应单测，`internal/acceptance/csv_import_test.go`（4/3/3） |
| 两层重试与交互优先 | `internal/provider/{client,retry,limiter,priority}.go` 及对应测试 |
| 零同步请求接受、补全调度 | `internal/app/enrich_worker.go`、`internal/app/enrich_worker_test.go`、`internal/store/enrichstate.go` |
| 非首曲身份、同发行缓存、完整/截断清单 | `internal/app/bindings_enrich.go`、`internal/app/enrich_identity_test.go`、`internal/provider/musicbrainz/detail_target_test.go` |
| 结构化署名、完整 CV、真实角色与空态 | `internal/app/artist_credit_test.go`、`internal/app/participants_missing_test.go`、`internal/app/vocaloid_view_test.go`、`internal/matching/cv_test.go`、真实 JSON 金样例 |
| 标签名单交集、代表发行、首次加入时间 | `internal/app/view_tag_filter_test.go`、`internal/app/added_time_test.go`、`internal/store/browse_test.go` |
| 封面 Front/索引与错误区分 | `internal/app/bindings_cover_album_test.go`、`internal/app/wiring_test.go`、`internal/covers/*_test.go`、CAA 金样例 |
| 选择、排序、列偏好、文本划选 | `frontend/tests/unit/`、`frontend/tests/e2e/{shell,list-edges,table-refinements,view-options}.spec.ts` |
| 标题行按钮、子菜单、280px 封面、真实 lazy 图片 | `frontend/tests/e2e/image11-refinements.spec.ts` |
| 请求乱序、角色空态、模态及样式 | `frontend/tests/e2e/{artist-loading,screenshot-review,visual-controls}.spec.ts` |

上述为领域级证据索引，不是声称每个 requirement 均被独立测试。

## 工作区与合并策略

- 已有 `frontend/wailsjs/go/models.ts` 两处尾随空白和未跟踪 `scratch/test_cover.go` 原样保留，不纳入提交；Wails 构建后 SHA-256 与任务开始一致。
- 本次提交仅限 openspec 文档/配置与偏差记录的同步状态说明，不倒改历史代码、不重置用户改动。
- 归档基线单独提交，偏差同步与核验另行提交，随后以非快进合并提交合入本地 `master`，保留审查/修复分支历史。
- 仓库未配置远程，本任务只做本地合并，不执行推送。

**最终评估**：已完成经用户确认的归档与规格裁决同步，可按该授权合并文档与本分支历史；仍不满足「全部规格均已实现/零缺陷可发布」的验收条件。
