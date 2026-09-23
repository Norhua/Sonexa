# 续作检查点：32/43，浏览器回归失败后暂停

本记录补充此前的 [implementation-checkpoint.md](implementation-checkpoint.md)，不覆盖历史检查点，也不代表全部功能已验收。

## 本次起点与完成项

- 起点：`tasks.md` 已完成 25/43，仓库已有大量未提交实现与测试；本次未重置这些改动。
- 本次完成：4.4、5.1、5.4、5.5、5.6、6.2、6.3，共 7 项。
- 当前：32/43。6.4–6.7、8.2–8.5、9.1–9.3 保持未完成。
- 使用本地 OpenSpec 根目录；机器无注册 store。未委派子代理。
- 仅使用临时数据库及 HTTP 替身；未启动原生应用或迁移日常数据库。

## 本次实现及证据

| 任务 | 实现/验证入口 |
| --- | --- |
| 4.4 | 统一 `checkCoverOutbound`；目录/图片/元数据搜索/QQ/163 重定向复查 gate；禁用不写入目录/规格故障；预览返回 403；预取保留本地读取与 outcome。`internal/app/cover_gate_test.go`、`internal/covers/gate_test.go`、`internal/provider/request_gate_test.go`。 |
| 5.1 | `internal/covers/sources/providers.go` 增加 CAA/iTunes Searcher，统一来源列表，CAA 固定目标优先且最多十个去重发行，iTunes 保留地区及四个构造规格；不同图片不再按同一专辑误合并。`providers_test.go`、`TestCoverSourceRegistryDoesNotExtendMetadataProviders`。 |
| 5.4 | QQ/163 保守配额跨实例共享但来源间独立；两源 Retry-After；元数据桶与 CAA 目录分离；预览、采用、自动请求合计并发。`sources/web_test.go`、`internal/app/cover_quota_test.go`。 |
| 5.5 | 删除未再调用的旧 SearchAllProviders 聚合/吞错实现，事件和请求携带 generation，拒绝重复会话 ID，最多 20 张卡片；单来源独立会话重试保留其他授权，关闭后迟到启动再次取消。`cover_search_test.go`、`frontend/tests/unit/cover-search.test.ts`。 |
| 5.6 | 采用前检查目标与 revision，事务继续防并发覆盖；仅 404 时退到同图规格；非法/失效 ID 拒绝；图片连接解析后拒绝私网，禁止图片传输使用环境代理。`cover_adoption_edges_test.go`、`transport_test.go`，以及既有同图原图降级测试。 |
| 6.2 | 候选/专辑名单补歌集 ID 与 revision；只有同一发行关联库内目标，不借文本模糊匹配串图；统一预取目标。`cover_dto_test.go`。Wails 绑定重新生成，生成目录仍被 Git 忽略。 |
| 6.3 | `CoverArt` 用途必填，列表/候选/专辑名单 thumbnail，抽屉/专辑头 detail；删除组件无效 size/adopted-path 参数；浏览器覆盖懒加载、布局、nil/错误/禁用、URL 改变后状态重置和迟到 outcome 丢弃。 |

## 已执行命令

下列针对性 Go 测试均通过，均带 `-count=1 -race`：

```sh
go test -count=1 -race ./internal/app ./internal/covers ./internal/covers/sources ./internal/provider -run 'Test(CoverOutbound|CoverSearchRechecks|CoverRedirect|RequestGate|ProductionCoverHandler|PickAndAdopt|WebSource|CoverSearchProgress|CoverAdoption|CoverSearchCancelled)'
go test -count=1 -race ./internal/covers/sources ./internal/app -run 'Test(CAASource|ITunesSource|CoverSourceRegistry|CoverSearch|CoverAdoption|CoverOutbound)'
go test -count=1 -race ./internal/covers/sources ./internal/app -run 'Test(WebSource|CoverSearchSharesMetadata|CoverPreviewAdoption)'
go test -count=1 -race ./internal/app ./internal/covers ./internal/covers/sources -run 'Test(CoverSearch|CoverSource|CoverAdoption|CAASource|ITunesSource|Adopt|ImageDimensions)'
go test -count=1 -race ./internal/app ./internal/covers -run 'Test(CoverAdoption|CoverPreview|ImageTransport|ImageURL|CoverRedirect|CoverOutbound|ProductionCoverHandler)'
go test -count=1 -race ./internal/app ./internal/store -run 'Test(CoverDTO|CoverSearch|CoverSource|Album|Browse)'
~/.go/bin/wails generate module
cd frontend
npm run typecheck
npm test
npm run build
SONEXA_E2E_PREVIEW=1 npm run test:e2e -- --project=chromium --project=webkit tests/e2e/image11-refinements.spec.ts tests/e2e/cover-search.spec.ts tests/e2e/table-refinements.spec.ts
```

- 绑定生成、类型检查：通过。
- 前端单元：6 文件，37 项通过。
- 前端构建：通过；主 chunk 约 923 kB，仍有超过 500 kB 的警告。
- 浏览器选定回归：28 项中 26 通过、2 失败。
- `git diff --check`：通过。
- 本次没有运行完整 `go test -count=1 -race ./internal/...`、`go vet ./...` 或完整 E2E，8.3 未完成。早期检查点的全量结果不是当前代码的全量证据。

## 暂停原因：出网禁用菜单测试失败

失败场景：`frontend/tests/e2e/cover-search.spec.ts:55`，Chromium/WebKit 均复现。

测试在页面载入后替换 `CheckOutbound` 为禁用，再发出 `settings:saved` 事件。随后期望“从网络搜索”菜单项 `aria-disabled=true`，但实际 `data-p-disabled=false`。

只读定位发现：`frontend/src/stores/settings.ts` 在 `load()` 中读取 gate，`save()` 调用保存后会主动 `load()`；目前没有订阅 `settings:saved`。因此该失败至少说明测试所用的事件模拟没有更新 store，尚不能仅凭这个场景断言真实设置保存流程也失败。下一轮应通过真实设置保存路径验证，并明确是否需要支持事件驱动刷新；不应只放宽断言。

未继续修改代码以绕过失败，6.4、8.2 仍未勾选。失败证据（本机生成，不提交）：

- `frontend/test-results/cover-search-outbound-disa-c2eea-cancel-leaves-current-cover-chromium/error-context.md`
- `frontend/test-results/cover-search-outbound-disa-c2eea-cancel-leaves-current-cover-chromium/trace.zip`
- 对应 `-webkit/` 下的同名文件。

本次生成的亮暗截图保存在两个浏览器各自的以下目录，但尚不构成 6.6/8.2 的完整验收：

- `frontend/test-results/cover-search-four-source-p-0f78b-retry-and-confirms-adoption-<browser>/`
- `frontend/test-results/image11-refinements-drawer-dfb71-t-menu-without-the-old-icon-<browser>/`

## 仍待处理

- 6.4：禁用菜单的真实设置链路、键盘选择和焦点恢复等尚需验收。
- 6.5–6.7：搜索页/返回快照、image12 对照、未知规格/非方图/大预览、精准共享更新与页面关闭保护，不能只据现有文件或部分测试勾选。
- 8.4：原生文件选择器、权限和移动文件、四来源实际网络及断网验收仍缺证据；本次没有重新验证 QQ/163 真实网络。
- 8.5：旧数据库及文件备份的升级/回滚演练仍缺记录。
- 9.x：未同步主规格、未清理现行文档，未归档。工作区原有和本次变更尚未合并提交，待后续按范围整理，避免将未经验收的实现误报为完成。
