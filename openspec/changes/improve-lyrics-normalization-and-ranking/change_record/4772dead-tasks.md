# 4772dead 执行计划

依据：[缘由与实施前审查](4772dead-why.md)。起始提交 `4772dead6acde85b4884b667302e395b563db923`。本轮不运行 openspec update、不改历史任务勾选。

## 任务

- [x] 1. 调查原实现/XLRC 草案、建立 why/tasks，审查范围、保真扩展及架构边界。
- [x] 2. 实现公共 XLRC 模型、解析、序列化、显式父行关联、语言/人声/ruby；覆盖保真扩展、同刻多声部、非法输入、大小限制和往返。
- [x] 3. 网易云/QQ/酷狗适配 XLRC，保留原生真实时间及格式内关联；删除酷我 provider/装配/配额/默认来源并更新相关回归。
- [x] 4. 接通 XLRC 本地输入、不可变文件保存、前端格式/人声/注音和详情显示；重新生成 Wails 绑定。
- [x] 5. 执行后端 race、前端 typecheck/单测/构建及歌词 E2E，检查直接交付→评分→预览批准→保存回读与来源边界。
- [x] 6. 最终自审并修复，更新累计 spec-diff、格式说明/README 和本记录验证结果；中文约定式提交。

## 验证与审查记录

### 已通过

- `go test -count=1 -race ./internal/...`：所有后端包通过（含三来源完整 App 管线、本地 XLRC 导入、预取未批准拒绝采用、采用文本/行词结束/声部注音回读、来源依赖边界）。
- `go vet ./...`：通过，无输出。
- `go test ./internal/lyricdoc -run '^$' -fuzz '^FuzzXLRC$' -fuzztime=15s -parallel=2`：通过，约 84 万次执行；不把有界 fuzz 等同于完全证明。
- `npm --prefix frontend test`：20 个文件 / 98 项通过。
- `npm --prefix frontend run build`：typecheck 与 Vite 构建通过，保留已有大 chunk 提示（非错误）。
- `SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- tests/e2e/lyrics.spec.ts`：Chromium/WebKit 共 20 项通过，新增 XLRC 同刻双声部、ruby、多语言及采用测试。
- 最终补充未知元信息头、扩展协商、独立/父关系矛盾拒绝，以及存储 `.xlrc` 扩展名/字节保真测试；再次运行 `go test -race ./internal/lyricdoc ./internal/lyrics ./internal/provider/{qqmusic,netease,kugou} ./internal/app ./internal/acceptance ./internal/store` 全部通过。
- `openspec validate improve-lyrics-normalization-and-ranking --strict`、`git diff --check`：通过；此验证不自动审核本轮自定义扩展的语义。

### 工具与环境

- PATH 无 `wails`。首次 `go run .../cmd/wails@v2.10.2 generate module` 成功；再次执行遇 Go proxy EOF，离线版本查询也不能完成。因此从已缓存的官方 Wails v2.10.2 模块目录，以 `GOPROXY=off go build -mod=readonly -o /tmp/sonexa-wails-v2.10.2 ./cmd/wails` 构建 CLI，再在项目根执行 `generate module` 成功。
- 生成文件 `frontend/wailsjs/` 按项目现有 `.gitignore` 忽略，未手写或强制纳入版本库；重新生成后类型检查通过。未修改 go.mod/go.sum，未新增运行时依赖。

### 自审修正

- 修正 QQ 相邻词结束与原生行结束丢失；酷狗 language 保留显式父行、空行及确定对应的音节时间。
- 同刻不同声部、语言、父关系不相互去重；明确同文翻译不当原文副本删除；unknown 不能通过 XLRC 保存回读升级为有翻译。
- 显式父关系必须存在且时间/人声一致；独立轨回读不再触发精度挂接。字段纳入 clone、预算及 seal；ruby 与 word/line 投影矛盾时拒绝。
- 加入语法字面转义，避免括号或时间样式正文往返被解释成标签；ruby 查找及父行索引有界，防止小文本展开巨大投影。
- 三来源默认列表、配额测试、前端替身及预取上限同步；只保留边界检测中的旧来源字符串负例。

### 未执行/限制

真实上游网络 probe 和 Wails 原生窗口人工验收未执行，不自动勾选原任务 9.3/9.4。浏览器替身不证明非公开端点今天提供声部/ruby，也不替代真实文件对话框人工验证。XLRC 0.1 为草案，Sonexa 保真扩展不保证第三方工具无损互通。未同步主 spec、未归档、未清空历史数据。
