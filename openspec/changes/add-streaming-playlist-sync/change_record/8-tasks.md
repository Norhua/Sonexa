# 8 — 执行计划

- [x] 8.1 统一查询范围与多选分类条件，覆盖曲目/计数/艺术家/角色/专辑及同步出现项查询。
- [x] 8.2 完善专辑已保存未入库行与灰色清单行区分、代表发行和计数。
- [x] 8.3 实现页面默认与级联多选菜单、搜索叠加、导航及异步刷新隔离。
- [x] 8.4 各页面保留未入库歌曲禁止标签/普通歌单的动作及服务端约束。
- [x] 8.5 补充离线后端与前端单元/E2E 回归，运行测试、类型检查及生产构建。
- [x] 8.6 汇总验证和 spec-diff，审查差异并中文约定式提交（不含已有 AGENTS.md 修改）。

## 实施说明

- `TrackFilter` 增加 IncludeSynced 与三个分类 ID 数组，null/省略与空数组有不同语义；复用匹配 ID 子查询，不在前端自行推算集合关系。
- 保留默认音乐库读入口，新增过滤读入口供页面使用；同步读入口保留 entry 级身份，专辑 DTO 补 InLibrary/SavedCount。普通集合和导出保持入库约束。
- `ViewOptionsMenu` 复用本地 Volt TieredMenu，定制项保留键盘 itemlink 与父项 label ID；处理首次 hover、连续多选不关闭和 mousedown 焦点，避免 Escape 不关闭或 Enter 不选择。菜单颜色/边缘定位没有另写全套实现。
- 用户选择记录见 8-why；原 proposal/design/specs/tasks 没有改动，本轮最终差异汇总到共享 spec-diff。

## 回归与验证

- `go test ./...`：通过，包含全部历史 importer/matching 回归；本轮不修改导入/匹配状态机，也没有开发者报告的具体匹配异常，因此没有增加 matching-issues 条目。
- `go vet ./internal/store ./internal/app`：通过。
- `npm test`：25 文件、141 项通过。
- `npm run build`：vue-tsc 与生产构建通过；保留已有主包大于 500KB 的 Vite 警告，不在本轮做无关拆包。
- `SONEXA_E2E_PREVIEW=1 npx playwright test --reporter=line`：最终 Chromium/WebKit 共 360 项通过（180 场景 × 2 浏览器）。
- `/Users/amadeus/.go/bin/wails generate module`：成功，生成绑定按仓库约定忽略；新筛选 API 和 DTO 已被实际前端类型检查/构建使用。
- `git diff --check`：通过。

新增后端测试：
- `TestBrowseMultiSelectScopesAndCounts`：分类并集/交集、空选、不限制时未分类歌曲、集合类型隔离、FTS/LIMIT、曲目计数/名单/分面、同步重复出现与缺席、标签/歌单混合拒绝。
- `TestRecentAllUsesSavedTimeWithoutInventingMembership`：两个时间来源、排序、默认范围与不伪造入库。
- `TestAlbumBrowseScopeDistinguishesSavedUncollectedFromGhost`：真实 App/Store 专辑列表、代表、详情正常行/灰色行、收藏数/已保存数与艺术家分面。
- 扩展 `TestLibraryScopeExportEmptyStateAndReferenceAwareDelete`：IncludeSynced + LIMIT 不能改变导出入库约束。

前端：新增 view-filters 单元与 browse-filters E2E；更新旧单标签菜单回归，保留主题/左右定位/长菜单/搜索叠加/异步名单/添加时间等覆盖；新增合并专辑成员权限证据测试。同步歌单与专辑版本原有 E2E 适配新多选操作。

## 调试与边界

- 初期连续多选后鼠标焦点离开菜单，导致 Escape 无效；定制 item 缺少键盘查找标记也会让 Enter 失效。已补 DOM 标记与焦点保持，鼠标后继续键盘多选、Home/End、Escape 均通过。
- 新 E2E 替身曾对 Vue proxy 数组使用 structuredClone 而失败，改为 JSON 边界克隆，与 Wails 实际 JSON 传输一致，不修改业务逻辑掩盖测试问题。
- 第一次生产全量 357/358：WebKit 编辑弹窗 `.search-fields` 即时坐标断言得到两种 y 值；该文件和编辑器业务未改。独立 WebKit 重复 3 次通过，最终生产全量 360/360 通过（最终额外增加窄窗口场景）。
- 没有原生窗口人工验收、真实网络、完整应用打包或 Windows/Linux 执行。未修改数据库和 provider；原 change 未完成的平台/在线验收任务不勾选。
