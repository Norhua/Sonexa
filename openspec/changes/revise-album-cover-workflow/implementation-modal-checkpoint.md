# 封面搜索大弹窗续作检查点

## 当前状态

- Change：`revise-album-cover-workflow`，schema：`spec-driven`。
- 接续 [上次检查点](implementation-resume-checkpoint.md) 的 32/43，本轮完成 6.4、6.5、6.6、6.7、8.2，当前 **37/43**。
- 剩余 8.3、8.4、8.5、9.1、9.2、9.3。全量 Go race 出现失败后暂停，没有把失败任务标为完成。
- 用户在实施中确认搜索改为**应用内大弹窗**，不是独立系统窗口。已同步 proposal、design、相关 delta 和任务 6.5；没有提前同步现行主规格或归档。
- 原有未提交改动继续保留。未触碰日常数据库，未进行原生 Wails 人工验收，未重新执行真实 QQ/163 网络验证。

## 已修复与完成

### 6.4：菜单与真实设置保存

上次失败使用 `settings:saved` 事件模拟 gate 更新，但设置 store 的实际路径为 `save → load → CheckOutbound`。回归改为打开设置表单、清空联系方式并保存，替身按 Wails JSON 参数序列化和持久化语义运行，不额外引入事件订阅来迎合测试。

通过 Chromium/WebKit 验证：网络菜单禁用，本地选择继续可用，取消无副作用；nil 区域键盘打开菜单，ArrowDown/ArrowRight/Enter 选择本地导入，Escape/取消恢复封面焦点；无歌集时两个动作均禁用。鼠标右键现会先聚焦封面区域，避免关闭菜单后焦点丢失。

### 6.5：应用内大弹窗

- `CoverSearchView.vue` 使用 Volt Dialog，宽度 1200px、高度 820px，并受视口最大宽高约束。
- `App.vue` 不再隐藏或替换主内容区/详情；背景保留并设置 inert，搜索弹窗通过 Dialog teleport 显示，不包含侧栏。
- 关闭按钮、取消或 Escape 关闭并取消会话；恢复原封面焦点和列表滚动、筛选、详情。
- 大预览为更上层弹窗，Escape 每次只关一层。
- 来源列表由后端提供，iTunes 可选地区或恢复默认；搜索条件不修改歌曲匹配身份。

证据：`frontend/tests/e2e/cover-search.spec.ts`、`table-refinements.spec.ts`；`frontend/tests/unit/cover-search.test.ts`。

### 6.6：网格与预览

- 新 `CoverPreview.vue` 统一加载、成功、失败占位和迟到图片事件保护；非方图 object-contain，失败隐藏破图图标。
- 首次卡片仅取同图最小档，不默认下载 iTunes 3000/网易云原图；显式选规格后才下载对应档位。
- 原图未知、构造待验证、实际宽高分别显示；采用前展示已选来源/类型/规格，采用后反馈实际尺寸。
- 四来源失败/有效空区分，单源重试保留其他卡片。
- 已对照 image12 检查左下两个徽标和亮暗主题截图，不将参考封面内容作为项目图片资源。

证据：`frontend/tests/e2e/cover-grid.spec.ts`。

### 6.7：精确共享更新与取消

- 后端 `CoverChangedEvent` 包含 releaseId/revision 及所属歌集的 provider/ref，以精确刷新候选身份。
- `CoverArt` 仅匹配本歌集或同一发行，使用递增 revision，忽略旧版本；不再无差别刷新所有没有 releaseId 的候选。
- 专辑名单/头部跟随目标更新，迟到详情不覆盖后来选中的专辑；主视图/目标变化及关闭取消搜索会话。
- `useCandidateCovers.ts` 统一搜索/复核的批量预取及表格消费者退出取消，移除搜索 store 的重复预取入口。

证据：`frontend/tests/e2e/cover-updates.spec.ts`、`internal/app/cover_dto_test.go`，以及原有会话取消/采用测试。

## 验证命令与结果

```sh
go test -count=1 -race ./internal/app -run 'Test(CoverDTO|CoverAdoption|CoverSearch)'
# 通过

go test -count=1 -race ./internal/...
# 失败：internal/app 一个测试出现 SQLITE_BUSY；其余包通过

go vet ./...
# 通过（在 race 失败后单独执行）

~/.go/bin/wails generate module
# 通过；生成产物仍被忽略，不提交

cd frontend
npm test
# 6 文件，37 测试通过
npm run build
# 通过；主 chunk 约 925 kB，有现存 >500 kB 警告
SONEXA_E2E_PREVIEW=1 npm run test:e2e
# Chromium + WebKit，102/102 通过

openspec validate revise-album-cover-workflow --strict
# 通过
git diff --check
# 通过
```

日志保存在本机 `tmp/cover-workflow-verification/`：`go-race.log`、`go-vet.log`、`bindings.log`、`unit.log`、`build.log`、`e2e.log`。

## 新阻塞：全量运行的 SQLite 并发失败

```text
--- FAIL: TestCoverPreviewAdoptionAndAutomaticImagesSharePool (0.36s)
    cover_quota_test.go:120: database is locked (5) (SQLITE_BUSY)
FAIL github.com/norhua/sonexa/internal/app
```

这是隔离数据库中同时进行预览、采用和自动目录请求时的错误，不是原生用户数据库的报错。此前针对性 race 测试通过，完整运行才暴露，不能简单重跑至绿或放宽断言。

只读排查：`SaveCoverFile/saveCoverFileTx` 在 deferred transaction 中先读后写；并发 CAA 目录写可能导致 SQLite WAL 读事务升级竞争。`AdoptCoverFile` 已先写再查版本，但其前置下载会写临时预览，因此还需定位具体失败事务并增加稳定复现。**此处为候选原因，尚未证明或修复**。

下一轮建议：先给失败路径增加定位和确定性并发测试，在不取消共享并发、不跳过 revision、不吞掉 BUSY 的前提下修复事务竞争，再重跑全量。8.3 保持未完成。

## 截图证据（本机生成）

均位于 `frontend/test-results/`，两个浏览器各一份：

- `cover-search-cover-search--a76b2--closes-one-layer-at-a-time-<browser>/cover-search-modal.png`
- `cover-grid-image12-badges--6281a-mages-and-explicit-adoption-<browser>/image12-grid-light.png`、`image12-grid-dark.png`、`cover-specs-pending.png`
- `cover-grid-empty-and-failed-*/` 中的预览失败、搜索错误和有效空截图（以实际 Playwright 输出目录为准）
- `cover-updates-same-album-r-252c1-out-refreshing-other-albums-<browser>/cover-shared-update.png`

全量 E2E 已保存上述证据，因此 8.2 完成；这些浏览器替身证据不替代 8.4 的真实来源和原生文件选择器验收。

## 尚未交付

- 修复 SQLite 并发并通过 8.3 全量验证。
- 原生验收、旧数据库/采用目录副本升级回滚演练。
- 现行规格/设计/功能索引/期限文档清理及最终证据汇总。
- 当前未提交：工作区含此前会话的大量未提交实现，且全量后端验证失败，不将其包装为已完成的交付 commit。
