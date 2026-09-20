# 文档与实现核对

日期：2026-09-20。代码基线：`8efba1a`。本次只修改文档与 OpenSpec 配置，不修改业务代码或用户数据库。

## 结果

| 维度 | 结论 |
| --- | --- |
| 完整性 | 重写全部 22 份主规格，合并为 106 条需求、133 个场景；每条需求至少一个场景。正文从 2321 行减为 1044 行，约精简 55%。 |
| 正确性 | 对照 Go 业务入口、存储、上游实现和 Vue 页面核对主要行为，将未实现承诺改为明确的现状与待完成目标，详见 [已知问题](known-gaps.md)。 |
| 一致性 | 规格、设计、项目上下文和根 README 使用同一术语与当前技术栈；已失效说明从现行文档删除。原始归档规划保持不变，旧核验报告移入归档。 |

活动变更为空，因此直接维护主规格，不重新开启已归档变更。核对采用 OpenSpec verify skill 的完整性、正确性、一致性三个维度，格式验证使用 OpenSpec CLI。

## 核对依据

| 范围 | 主要实现与回归 |
| --- | --- |
| 数据模型、入库、字段归属 | `internal/store/{migrations,create,admit,enrich}.go`；`internal/app/admit_test.go` |
| 文本、评分、入库判定 | `internal/matching/{variants,classify,confidence,dispatch,provenance}.go`；`internal/acceptance/csv_import_test.go`，十行样例期望 4 入库 / 3 歧义复核 / 3 未匹配 |
| 详情身份与后台任务 | `internal/app/{bindings_enrich,enrich_worker}.go`；`enrich_identity_test.go`、`enrich_worker_test.go` |
| 署名与浏览 | `internal/store/{artist,browse,lookup}.go`；`internal/app/{artist_credit,participants_missing,view_tag_filter}_test.go` |
| 清单与封面 | `internal/app/{bindings_albums,bindings_covers}.go`；`internal/provider/{musicbrainz,itunes}/`、`internal/covers/` 及对应测试 |
| 设置、编辑、导入导出 | `internal/app/bindings_{settings,edit,import,export}.go`；`frontend/src/views/{SettingsView,EditDialog,ImportWizard,ReviewView}.vue` |
| 列表与交互 | `frontend/src/{components,stores,lib}/`；`frontend/tests/unit/`、`frontend/tests/e2e/` |

核对中特别纠正：iTunes lookup 固定 200 条、清单重取可能命中缓存、编辑只取最高分候选、部分设置没有实际使用、标签默认仍按集合位置排序，以及完整清单与发行组收藏计数的差异。未完成的用户锁、编辑事务等目标保留在问题清单，不当作正常功能保证。

## 本次执行的检查

| 检查 | 结果 |
| --- | --- |
| `openspec validate --specs --strict --no-interactive` | 22/22 通过 |
| `openspec validate --archived --strict --no-interactive` | 1/1 通过 |
| `go test -count=1 -race ./internal/...` | 11 个包通过 |
| `go vet ./...` | 通过 |
| `npm --prefix frontend test` | 5 个文件、34 项通过 |
| `npm --prefix frontend run build` | 类型检查和 Vite 构建通过；主包约 912 kB，仍有超过 500 kB 告警 |
| `cd frontend && SONEXA_E2E_PREVIEW=1 npm run test:e2e` | Chromium/WebKit 共 78 项通过 |
| 需求结构、现行术语、本地 Markdown 链接与锚点 | 通过 |
| 归档原文对比 | proposal、design、tasks、配置及 22 份增量规格，共 26 个文件与基线一致 |
| `git diff --check` | 通过 |

## 边界

本报告不是逐场景动态验收。自动化回归不证明已知问题已修复，也不覆盖外部条款合规、原生 Wails 人工操作或大曲库性能。本次未重新打包或启动原生应用；浏览器回归基于本次构建及 Wails 测试替身。
