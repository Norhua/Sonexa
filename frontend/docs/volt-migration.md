# Volt 替换计划与审查（GPT 分支）

## 边界

- 起点 `7a29eb79a985ef7f1a3c4a4ec5b7a51dde874009`，分支 `fix-v1-shell-and-import-gpt`。
- `fix-v1-shell-and-import` 保留 Claude 提交；不 reset、不重写历史。
- OpenSpec 的 proposal/design/tasks 与 22 份 spec 仅供审查，不修改。
- `tmp/v1-deviations.md` 是被忽略的工作文档，含 Claude 分支第七轮内容；原样保留。GPT 副本 `tmp/v1-deviations-gpt.md` 按本分支实际实现修订，旧测试结论不计作本轮证据。
- 不改变 Go/SQLite 数据模型、上游匹配、导入分流与 Wails API。

## 审查发现

1. 原项目实际只有一个 shadcn Button；绝大多数表单、浮层、表格为手写。仅替换依赖无法解决交互一致性。
2. PrimeVue 4.5.5 的 DataTable 多选不是完整的文件管理器交互：范围锚点为数组下标，排序/重载后可能指向别的行；方向键默认仅移动焦点；Enter 默认选中而非打开；没有逐行禁止选中的公开属性。直接启用原生 selection 再外层过滤会有两套状态机。
3. 专辑清单位置与曲目身份不同：灰色行 id 可重复为 0，「其他版本」也可能多位置命中同一首曲目。不能只用 track id 当 DataTable dataKey。
4. 原曲目表格监听 window 的 Cmd+A，没有焦点作用域；设置/其他浮层打开时也可能改动背景选择。列表中的文字选择与整行拖拽互相争用。
5. 歌单筛选时只传可见 id 去重排并不代表完整歌单顺序；仅默认顺序且无筛选时允许重排。
6. 候选表格把「已在库中」按钮 disabled，却又要求能点击跳转；原搜索本地结果将已采用封面写进错误的 `coverPath` 属性。
7. 原生自制模态框缺焦点约束与统一 Escape；原始导入解析失败时错误被藏在只有 preview 才渲染的 footer。

## 实施方案

### 1. 基础与普通控件

- 使用官方 tag `primevue/4.5.5/apps/volt/volt` 的源码与固定 `primevue@4.5.5`，而非滚动 master；MIT 许可、来源与本地定制清单一并入库。
- `src/volt/` 承担通用控件与 Tailwind pass-through；采用单一中性色阶、可区分的 hover/selected/focus，保留 Lucide 图标。
- 替换 shadcn/reka、原生输入/选择控件、自制菜单/Popover/Dialog，保留特定业务的导航结构和只读表格。
- 对 Claude 方案中可独立复用的普通控件迁移逐项 review；不继承其曲目表格交互实现和验收声明。

### 2. 曲目列表

- Volt DataTable 负责布局、粘性表头、排序和行重排；`TrackTable` 保持唯一业务入口，各视图同列。
- **唯一的受控选择模型**（独立可测试模块）负责普通点击、Cmd/Ctrl 切换、Shift 连续/收缩、Cmd/Ctrl+Shift 叠加、方向键/Home/End、Enter 打开、Escape 清空。不启用 DataTable 内部 selectionMode，避免互相覆盖。
- 排序采用 `lib/trackRows.ts` 的单一稳定比较器（中文自然排序、缺失值始终末尾）。DataTable `lazy` 仅关闭内部二次排序，保留列头排序事件和图标；并非服务器分页。核对发现 `value-change` 不在每次外部 sortField/value 更新时触发，不能拿它作为显示顺序的唯一真源。
- 独立行 key 表示位置；对外曲目 id 去重。锚点/焦点均用稳定 key。
- 快捷键仅在表格行/列表容器获得焦点时生效。灰色行不参与选择、键盘导航和导出。
- `select-text` 文字允许拖选；拖动重排使用现有 # 列内手柄，不增加一列，不修改 PrimeVue 私有状态或靠冒泡覆盖 draggable。
- 右键已选行保留多选，右键其他普通行改为独选，灰色行菜单仅两项；支持键盘菜单键/Shift+F10。
- 列头与外部排序菜单共用状态，专辑清单固定顺序；切换视图上下文清理选择。

### 3. 验证与交付

- 单元测试：选择模型、排序/更新时锚点、重复 track id、ghost、范围收缩。
- Playwright 真实组件/应用桩件：Chromium + WebKit，鼠标/键盘、文字选择、拖拽、右键边界/关闭、模态焦点、主题对比、导入/编辑/设置/复核。
- `npm run build`、`go test ./...`、`wails build`；桩件不替代原生 Wails 人工验收。
- 分阶段提交：计划 → 控件与主题 → 曲目交互及测试 → 偏差与验证结果。

## 非目标

本轮不加入分页/虚拟滚动、批量编辑、播放、数据库迁移。虚拟滚动将来可以只减少 DOM 而不改变全选语义，但需要针对大库与拖动另行验证；不把它描述为已实现或固有限制。
