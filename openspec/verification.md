# 文档与实现核对

日期：2026-09-23。代码基线：`edb175a` 加 `revise-album-cover-workflow` 的实现改动。本次将该变更的八个能力增量同步到主规格，并更新架构设计、功能索引、已知问题和本说明；历史归档不改。上一次全面核对（2026-09-20，基线 `8efba1a`）覆盖全部 22 份规格，记录见 Git 历史。

## 结果

| 维度 | 结论 |
| --- | --- |
| 完整性 | 主规格共 23 份，新增 [封面搜索](specs/upstream/cover-search/spec.md)；歌集清单、界面与交互、数据存储、设置、封面、iTunes、统一接口的增量需求全部进入主规格，原有场景未被删除。 |
| 正确性 | 封面需求对照用途解析、CAA 目录、封面工作池、专辑图片文件、四来源适配、绑定和 Vue 页面核对；iTunes 需求对照缓存迁移、清单状态和设置字段核对。未完成部分记入[已知问题](known-gaps.md)的 G05、G09、G12、G14、G15；G10 的 iTunes 期限目标取消。 |
| 一致性 | 现行规格和设计不再包含列表与详情共用 250px 缓存键、详情重新获取图标、按内容哈希命名采用文件或 iTunes 保存期限；保留其他来源的通用缓存有效期、来源署名和导出不含图片的规则。 |

## 核对依据

| 范围 | 主要实现与回归 |
| --- | --- |
| 封面解析与存储 | `internal/covers/{resolver,pool,library_images,library_cache,files,transport}.go`、`internal/store/{migration_covers,cover_catalog,cover_files,cover_cache,cover_legacy}.go` 及对应测试 |
| 封面来源与绑定 | `internal/covers/sources/`、`internal/app/{bindings_covers,bindings_cover_search,bindings_cover_targets}.go`；`internal/app/cover_*_test.go` |
| 封面界面 | `frontend/src/{components/CoverArt.vue,components/CoverPreview.vue,components/DetailDrawer.vue,views/CoverSearchView.vue,stores/coverSearch.ts,lib/useCandidateCovers.ts}`；`frontend/tests/e2e/cover-*.spec.ts` |
| iTunes 期限移除 | `internal/provider/itunes/`、`internal/store/migrate.go`（iTunes 缓存改为不过期）、`internal/config/settings.go`、`internal/app/{bindings_albums,bindings_settings}.go`；清单日期判定所在的 `internal/store/stale.go` 已删除 |

## 本次执行的检查

| 检查 | 结果 |
| --- | --- |
| `openspec validate --specs --strict --no-interactive` | 23/23 通过 |
| `openspec validate revise-album-cover-workflow --strict` | 通过 |
| `go test -count=1 -race ./internal/...` | 12 个包通过 |
| `go vet ./...` | 通过 |
| `npm --prefix frontend test` | 6 个文件、37 项通过 |
| `npm --prefix frontend run build` | 类型检查和 Vite 构建通过；主包约 925 kB，仍有超过 500 kB 告警 |
| `cd frontend && SONEXA_E2E_PREVIEW=1 npm run test:e2e` | Chromium/WebKit 共 102 项通过 |
| 旧数据库升级、中断续迁、目录变更与备份回滚演练 | 通过；无丢图、无额外收藏或歌集，旧版本拒绝打开新库 |
| 真实来源 | CAA、iTunes、163 完成搜索、预览和采用；QQ 搜索接口返回 HTTP 500，见 G14 |
| 本地 Markdown 链接与锚点 | 通过 |
| `git diff --check` | 通过 |

WebKit 下 `frontend/tests/e2e/image11-refinements.spec.ts:12`（侧栏按钮键盘激活）存在既有的间歇失败：变更前基线重复 20 次失败 3 次，当前失败 6 次，均为回车后新建对话框未出现；该测试不在本次改动范围内。

## 边界

本报告不是逐场景动态验收。原生 Wails 窗口中的系统文件选择器、右键与键盘操作、搜索弹窗和真实断网尚未人工验收（G14），也不覆盖外部条款合规和大曲库性能。浏览器回归基于 Wails 测试替身。命令输出、演练数据和真实来源样例见变更目录中的 `acceptance.md`。
