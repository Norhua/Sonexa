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
  wailsjs/               Wails 生成接口与运行时（纳入版本控制）
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
# 初次检出：安装前端依赖、生成可嵌入的 dist
cd frontend
npm ci
npm run build
cd ..

# Go 回归（不依赖 dist、不启动桌面或读取用户库）
go test -count=1 -race ./internal/...
go vet ./...

# 原生开发 / 打包
wails dev
wails build
```

Wails 生成的 Go 绑定保持跟踪，使纯前端类型检查与测试不依赖临时启动原生应用。修改导出 API 或包名后，先重新生成，再提交生成接口与调用方：

```sh
wails generate module
cd frontend
npm test
npm run build
npx playwright install chromium webkit
SONEXA_E2E_PREVIEW=1 npm run test:e2e
```

`go test ./...`/`go build ./...` 包含根启动包，需要先有 `frontend/dist/`（由前端构建生成）；直接测试 `./internal/...` 则不依赖前端产物。双浏览器测试使用 Wails 桩件，不能替代原生 Wails 人工验收。

## 跟踪与忽略边界

跟踪源码、`*_test.go`、测试金样例、依赖锁文件、Wails 生成接口、平台打包资源及 OpenSpec 工具配置。

忽略 `.DS_Store` 等系统文件、本机配置和 `.env`、依赖目录、dist/bin、Wails 的 `package.json.md5`、覆盖率和浏览器报告、SQLite 本地数据与旁文件、临时资料。`.env.example`/模板仍可跟踪。不要忽略整个 `build/`、所有生成代码、测试或项目级 `.pi/`/`.agents/`/`.claude/`。

## 整理后的验证（2026-09-19）

- 原有 321 个顶层 Go 测试全部仍被发现，新增 2 个桌面装配回归；`go test -count=1 -race ./...`、`go vet ./...`、`go build ./...` 通过。
- 前端 34 项单测、TypeScript/Vite 构建及生产 dist 的 Chromium/WebKit 78 项 E2E 通过。
- Wails darwin/arm64 打包通过；生成绑定与迁移前的方法名、参数、返回类型和模型字段相同，仅包命名空间从 main 改为 app。
- OpenSpec 22 份主规格及归档任务严格校验通过。现有大包告警和产品实现缺口继续保留，未进行原生人工验收。

## 规格与历史

[现行规格](openspec/README.md)、[架构设计](openspec/design.md)、[已知缺口](openspec/known-gaps.md)。已合并的修复记录不再保留独立 `change_record/`；原文仍可从 Git 历史追溯，现行规格索引提供命令。文件整理不表示这些已知缺口已经修复。
