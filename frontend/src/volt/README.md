# Sonexa Volt components

Volt = 本地 Vue 包装源码 + **unstyled PrimeVue 4.5.5** + Tailwind CSS v4。

来源：<https://github.com/primefaces/primevue/tree/4.5.5/apps/volt/volt>（MIT，见 `LICENSE.md`）。
官方组件与普通控件接线已核对；TrackTable 的选择行为由独立的受控模型实现。
`upstream-sources.json` 记录各官方源文件的 SHA-256；它们不是当前定制文件的校验和。
不要通过滚动 master 的 CLI 覆盖本地定制文件。升级运行时后，先核对 PT/slot API，再跑测试。

## 定制入口

- 调色板、浅深色、选中与 hover 对比：`../style.css`。surface 色阶是单一来源，primary 默认引用它。
- 密度与样式：各组件 `theme`（Tailwind pass-through）；优先在这里调整，不在业务页面重复覆盖。
- 单例覆盖使用 `pt:root:class`（如搜索框）而非 `class` 与默认工具类竞争；状态变体须同层覆盖（如 `p-small:pl-8`）。
- DataTable 显式合并传入的 `pt`，防止调用方整对象替换主题。固定表头、小字号/紧凑行；排序图标只在已排序、hover 或键盘聚焦时出现。
- 表格业务状态归 `TrackTable.vue` + `lib/useListSelection.ts`；普通 DataTable 本身不带曲库业务。排序用同一份 `orderedRows`，以 `lazy` 关闭 PrimeVue 二次排序（并非服务端分页）。
- Dialog 修正 unstyled flex/滚动/视口边界；禁用 PrimeVue 默认全局 Escape，由 `dialogStack.ts` 只关闭最上层，嵌套关闭互不干扰。WebKit 点击按钮未聚焦/保存时焦点落到 body 也可关闭；Select/Popover 消费 Escape 时保留父弹窗。支持取消焦点恢复，默认不点遮罩关闭，防误丢编辑。
- ContextMenu 是按 Volt Menu 模式编写的本地组件，不声称上游提供。无进入动画，确保外部关闭监听/键盘焦点立即生效；Escape 后恢复来源焦点，不抢新弹窗焦点。
- 图标插槽统一 Lucide；`SortIcon.vue` 是本地补充。运行时仍可能传递依赖 `@primevue/icons`，不用 PrimeIcons 字体包。
- Checkbox 移除上游与 4.5.5 插槽类型不匹配的 dataP；Chip 移除图标补键盘角色/可访问名称。
- Popover 的 `hide()` 必须调用 hide，不能 toggle；Menu/Popover exposed 参数加类型。
- Tag 默认小型轻字重。InputNumber 默认禁止空数值、不显示千分符，输入 min-width 为 0，避免窄设置项溢出；应用仍由后端校验范围。
- Message 修正 warn 变体拼写。

## 验证

```sh
cd frontend
npm ci
npx playwright install chromium webkit
npm test
npm run test:e2e
npm run build
SONEXA_E2E_PREVIEW=1 npm run test:e2e
```

E2E 用真实应用组件和 Wails API 桩件，**不等于原生 WKWebView 的人工验收**。
测试与桩件在 `frontend/tests/` 入库，不依赖被忽略的 `tmp/harness`。
