# Sonexa

以音乐收藏为中心的桌面资料库。技术栈：Wails v2 + Go + Vue 3 + TypeScript + Tailwind CSS v4 + Volt + Lucide Icons + 按需 Pinia。

## 目录与文件职责

```text
main.go                  Wails 启动入口与前端资源嵌入
internal/
  app/                   App 应用协调、bindings_*.go、后台补全、桌面装配
    *_test.go            同包的生产绑定/接线回归测试
  store/                 SQLite、迁移、查询、来源与锁
  provider/              上游接口、限流、重试、MusicBrainz / iTunes
  matching/              文本处理、评分、分流
  importer/              表格解析与持久化导入队列
  covers/                封面工作池、缓存与本地服务
  export/                CSV / JSON 导出
  config/                路径与设置
  acceptance/            跨领域验收测试
frontend/
  src/                   Vue 页面、业务组件、Volt、按需 Pinia
  tests/                 Vitest 单测与 Playwright / Wails 桩件
  wailsjs/               Wails 自动生成的接口与运行时（忽略，首次使用前生成）
  docs/                  前端设计记录
build/                   图标、平台配置和安装器源码（需跟踪）
  bin/                   打包产物（忽略）
testdata/                受版本控制的离线测试金样例
openspec/                现行规格、设计、已知缺口与历史归档
prompt/                  初始产品需求材料
```

- `bindings_*.go` 是前端调用的**生产代码**，不是生成缓存或测试脚本，不可删。
- Go 的 `*_test.go` 不进入生产二进制，但需要与实现同包以访问测试接缝；不为了减少文件数删测试、导出内部字段或把它们放到 Go 不扫描的目录。
- 根目录只保留一个 Go 启动文件。`internal/app.DesktopOptions` 以包级函数装配生命周期与封面 HTTP handler，避免把它们导出为 `App` 前端绑定方法。
- App 包的生成接口是 `frontend/wailsjs/go/app/App`，模型命名空间为 `Models.app`，测试桩同样使用 `window.go.app.App`。
- 临时截图、日志和手动程序放在被 Git 忽略的 `tmp/`。其中 Go 调试程序放 `tmp/_scratch/`：Go 的 `./...` 不扫描下划线开头的目录，避免误把手动程序加入构建/测试。`.gitignore` 本身不控制 Go 包发现，不能只靠忽略 scratch 来隔离。

## 开发与验证

需要 Go、Node.js/npm、Wails v2，以及目标平台的 Wails 构建依赖。

```sh
# 初次检出：先安装依赖，再让 Wails 按正确顺序生成绑定、构建前端并打包
cd frontend
npm ci
cd ..
wails build

# Go 回归（internal 包不依赖 dist、不启动桌面或读取用户库）
go test -count=1 -race ./internal/...
go vet ./...

# 原生开发
wails dev
```

`frontend/wailsjs/` 与 `frontend/package.json.md5` 是可重建产物，不再受版本控制，旧分支历史中也已清除。修改 Go 导出 API、包名或切换分支后，重新执行 `wails build` 或 `wails generate module`，不提交生成文件。

单独运行前端构建/测试也需要先生成绑定。`wails build` 会自动处理 embed 目录；若仅生成绑定、还没有 dist，可在仓库根目录执行以下初始化（占位文件不是实际页面，会在 Vite 构建时替换）：

```sh
mkdir -p frontend/dist
touch frontend/dist/.gitkeep
wails generate module
cd frontend
npm ci
npm test
npm run build
npx playwright install chromium webkit
SONEXA_E2E_PREVIEW=1 npm run test:e2e
```

`go test ./...`/`go build ./...` 包含根启动包，需要先有 `frontend/dist/`（由前端构建生成）；直接测试 `./internal/...` 则不依赖前端产物。双浏览器测试使用 Wails 桩件，不能替代原生 Wails 人工验收。

## 跟踪与忽略边界

跟踪手写源码、`*_test.go`、测试金样例、依赖锁文件、平台打包资源及 OpenSpec 工具配置。

忽略 `.DS_Store` 等系统文件、本机配置和 `.env`、依赖目录、dist/bin、Wails 生成的整个 `frontend/wailsjs/`（含 models.ts、App 接口与 runtime）及 `package.json.md5`、覆盖率和浏览器报告、SQLite 本地数据与旁文件、临时资料。`.env.example`/模板仍可跟踪。不要忽略生产用的 `internal/app/bindings_*.go`、整个 `build/`、测试或项目级 `.pi/`/`.agents/`/`.claude/`。

## 整理后的验证（2026-09-19）

- 原有 321 个顶层 Go 测试全部仍被发现，新增 2 个桌面装配回归；`go test -count=1 -race ./...`、`go vet ./...`、`go build ./...` 通过。
- 前端 34 项单测、TypeScript/Vite 构建及生产 dist 的 Chromium/WebKit 78 项 E2E 通过。
- Wails darwin/arm64 打包通过；生成绑定与迁移前的方法名、参数、返回类型和模型字段相同，仅包命名空间从 main 改为 app。
- OpenSpec 22 份主规格及归档任务严格校验通过。现有大包告警和产品实现缺口继续保留，未进行原生人工验收。

## 自动生成文件的历史清理（2026-09-19）

- `frontend/wailsjs/`（包括 models.ts、历次 main/app 绑定及 runtime）和 `frontend/package.json.md5` 已从全部 4 个本地分支历史移除，分支名及合并拓扑保留，相关提交哈希已改变。
- 清理时逐个比较了 76 个提交的文件树与父提交关系，确认除目标路径外的文件内容不变；40 个目标历史 blob 已从当前仓库对象库清除。没有远程仓库，因此未推送或强推；其他旧克隆不会自动改变，不能把旧分支再合回来恢复这些文件。
- 本机可再生成的文件仍保留且被忽略。完整旧历史 bundle、原有文件备份与 commit-map 位于仓库外 `../Sonexa-git-history-backups/2026-09-19-generated-cleanup.vJtO06/`；该备份仍含旧生成文件，仅用于恢复，不是已清理的历史副本。
- 从无生成绑定、dist 或依赖目录的快照重新安装/生成后，前端 34 项单测、类型检查/Vite 和 78 项 E2E 通过；Go internal race、vet/build 及从零 Wails 打包通过。保留大包告警；安装提示现有 lucide-vue-next 弃用及部分依赖安装脚本待批准，本次未修改技术栈或升级依赖。

## 规格与历史

[现行规格](openspec/README.md)、[架构设计](openspec/design.md)、[已知缺口](openspec/known-gaps.md)。已合并的修复记录不再保留独立 `change_record/`；原文仍可从 Git 历史追溯，现行规格索引提供命令。文件整理不表示这些已知缺口已经修复。
