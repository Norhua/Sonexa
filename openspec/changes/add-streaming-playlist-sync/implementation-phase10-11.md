# 第 10 组与离线验收记录

> 本文保留本阶段结果。后续浏览器失败已排查并修正，全量 304/304 通过；当前状态与剩余边界见[第 11 组离线验收](implementation-phase11-offline.md)。

## 实施范围

- 第 10 组：独立同步导航/侧栏、能力驱动的绑定窗口、同步列表与串行刷新、差异/忽略恢复窗口、批量入库窗口。
- 表格：entry ID 稳定行键、重复出现保留、上游缺席标记与 ghost 分开、只读成员菜单、独立入库列偏好与绿色可访问标记。
- 导入/复核：多目标提示及批次目标，按目标身份去重避免同名绑定丢失；目标和队列事件即时重读。
- 搜索：本地精确绑定但未入库的候选打开归属确认，不跳到不可见的曲库行；复核仍跳过。
- 设置：QQ 共享配额包含歌单，关于页增加 SpotifyScraper/CPython/PyInstaller 版本许可说明；App 监听 settings:saved 并防旧设置响应覆盖。
- 11.1：新增 `internal/app/playlists_e2e_test.go`，真实 App + 隔离 SQLite + 仅上游能力替身，四种投递路径验证双歌单共享、显式入库、普通歌单组织、移出、absent 引用保护、最后绑定回收、事件与 FK。

## 实际验证

```sh
# 第 9 组完成时已生成，前端变更未改变绑定签名
/Users/amadeus/.go/bin/wails generate module

# 加入最终 App 端到端验收后，全部内部包通过
go test -count=1 -race ./internal/...
go vet ./...

cd frontend
npm test
npm run build
SONEXA_E2E_PREVIEW=1 npm run test:e2e -- synced-playlists.spec.ts review-actions.spec.ts live-updates.spec.ts table-refinements.spec.ts shell.spec.ts
SONEXA_E2E_PREVIEW=1 npm run test:e2e
```

- Go 全量 race：通过（包含 App 端到端四种路径）。
- 前端 Vitest：21 文件、111 测试通过。
- 类型检查与 Vite 构建：通过；仍有大 chunk 警告，没有为此扩大打包优化范围。
- 同步/复核/即时刷新/表格/外壳分组：116/116 通过，Chromium + WebKit。后续又补了同步灰色行键盘/详情及批次目标刷新用例，包含在全量中通过。
- 全量浏览器首轮：298/304；更新本次影响的旧分组 Tab 顺序、QQ 配额文案断言后再次全量：**301/304，通过 301、失败 3**。不是全量通过。

## 失败与基线

1. `cover-grid.spec.ts:35` 在 Chromium/WebKit 均失败：`src` 断言要求 `/variantId=32$/`，现有 URL 是 `/cover?variantId=32&searchId=...`。
   - 在独立临时目录用 `git archive ebef291 frontend` 还原本轮 UI 之前的源码，复用本机依赖/生成绑定，构建后运行该文件；同样 **2 通过、2 失败**，错误相同。
   - 本轮没有修改 CoverPreview 或 cover-grid 代码。保留既存失败，未擅改封面语义或放宽测试。
2. 最新全量另有 `screenshot-review.spec.ts:77` 的 WebKit 失败：编辑弹窗出现 11 个“用户”标签，测试期望 0。上一轮全量此项通过；**未确认原因及是否基线偶发**，不能标为已解决或既存确定失败。
   - 涉及既有编辑 UI，暂停等待用户决定是否扩大排查范围。

中间测试修复：
- 单测需要让第一个请求进入微任务后再切上下文，否则 refreshQueue 合并成一次请求，不能模拟“旧请求迟到”。
- 浏览器忽略项可访问名称包含艺术家占位，改为按真实 checkbox 名称匹配。
- WebKit 新增按钮点击显式接焦点，弹窗关闭可稳定返回；提交期间关闭与 Escape 均禁用。
- 新测试的 SearchUpstream 替身原先误调用不存在的旧函数，改为直接提供候选 fixture。
- 出网设置事件原先未被 App 订阅，补上事件重读并防旧结果覆盖。

## 未完成及续接

- 4.2–4.4 按前次续接约定保留未完成；没有新声称 Windows/多平台进程树与发行包验收。
- 11.2 尚未补齐独立的综合重启/并发验收记录，已有包内 race 用例不冒充所有平台过程验收。
- 11.3 已执行要求的全量命令，但仍有未判明浏览器失败，暂不勾选验收完成。
- 11.4 没有获准公开测试歌单和已观察增删条件，未进行 live 请求。
- 11.5 没有实际发布目标的完整应用安装/原生窗口/签名验收。
- 11.6 尚待上述边界及最终 18 份 delta 交付核对，不归档。
- 用户操作说明已更新在根 README；API/事件用途见 `frontend/src/api/README.md`。
- 生成绑定、dist、node_modules、Playwright test-results 和冻结产物均未纳入 Git。
