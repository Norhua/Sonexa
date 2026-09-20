# Sonexa

以音乐收藏为中心的桌面资料库，不依赖本地音频文件。

技术栈：Wails v2 + Go + Vue 3 + TypeScript + Tailwind CSS v4 + Volt + Lucide Icons + Pinia。

## 文档

- [功能规格与术语](openspec/README.md)
- [架构设计](openspec/design.md)
- [已知问题与未完成目标](openspec/known-gaps.md)
- [文档与实现核对](openspec/verification.md)

## 目录

```text
main.go               Wails 启动与前端资源嵌入
internal/
  app/                应用协调、前端绑定、后台补全与桌面配置
  store/              SQLite、迁移、查询、来源与锁
  provider/           上游接口、限流、重试、MusicBrainz / iTunes
  matching/           文本处理、评分、入库判定
  importer/           表格解析与持久化导入队列
  covers/             封面下载、缓存与本地服务
  export/             CSV / JSON 导出
  config/             路径与设置
  acceptance/         跨模块验收测试
frontend/
  src/                Vue 页面、共享组件、Volt、状态管理
  tests/              Vitest 与 Playwright 测试
  wailsjs/            自动生成的绑定与运行时（不提交）
build/                平台资源和配置；bin/ 为打包产物
testdata/             离线回归测试数据
openspec/             规格、设计、已知问题和历史归档
prompt/               初始需求材料
```

Go 测试与实现同包，`internal/app/bindings_*.go` 是手写业务代码。前端统一 API 使用 `frontend/wailsjs/go/app/App`、`Models.app`，浏览器测试通过 `window.go.app.App` 提供测试替身。

## 开发与验证

需要 Go、Node.js/npm、Wails v2 及目标平台构建依赖。

```sh
# 首次检出：安装前端依赖，再生成绑定、构建和打包
cd frontend
npm ci
cd ..
wails build

# Go 测试与静态检查
go test -count=1 -race ./internal/...
go vet ./...

# 原生开发
wails dev
```

首次使用或修改 Go 导出 API 后，须先运行 `wails build` 或 `wails generate module` 生成绑定。只生成绑定时，需先为根启动包准备 embed 目录：

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

`go test ./...` 和 `go build ./...` 包含根启动包，依赖前端 `dist/`；测试 `./internal/...` 不需要前端产物。浏览器测试不能替代原生窗口、输入法、文件对话框和大曲库验收。

## 版本控制

提交手写源码、测试、测试数据、依赖锁文件、平台资源及项目工具配置。不提交依赖目录、构建产物、Wails 生成绑定、`package.json.md5`、测试报告、数据库、本机配置和 `.env`；规则见 `.gitignore`。

临时资料放 `tmp/`，临时 Go 程序放 `tmp/_scratch/`，避免被 Go 的 `./...` 扫描。Git 忽略规则不控制 Go 包发现。
