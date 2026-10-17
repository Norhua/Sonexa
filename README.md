# Sonexa

以音乐收藏为中心的桌面资料库，不依赖本地音频文件。

技术栈：Wails v2 + Go + Vue 3 + TypeScript + Tailwind CSS v4 + Volt + Lucide Icons + Pinia。

基本信息允许缺失；具体发行按本地组聚合，各版本保留自己的收藏、结构和封面，支持合并或拆分展示。编辑器采用两栏草稿、确认“使用”及原子保存；未切换发行上游时可原地修改共享专辑信息，新建或切换上游则逐曲确认迁入，空发行随最后一首收藏迁出或删除而清理。

当前开发模型需要在清空历史应用数据后初始化；旧模型会明确拒绝打开，程序不会自动删除或转换。本轮原生人工验收已由用户确认完成；浏览器回归仍不能代替后续版本的原生验收，证据范围见文档核对记录。

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
  store/              SQLite、当前值/快照、事务、查询与文件引用
  provider/           上游接口、限流、重试、MusicBrainz / iTunes / QQ 音乐
  domain/             三层身份、统一快照和跨层契约
  library/            归组、准入及编辑保存计划
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

Go 测试通常与实现同包；跨上游组合测试放在 `internal/app`，避免消费侧契约与适配器循环依赖。`internal/app/bindings_*.go` 是手写业务代码。前端统一 API 使用 `frontend/wailsjs/go/app/App`、`Models.app`，浏览器测试通过 `window.go.app.App` 提供测试替身。

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

## 歌词来源验证

歌词业务契约、评分和通道合并位于 `internal/lyrics`；四来源协议位于
`internal/provider/{netease,qqmusic,kugou,kuwo}`，由 App 注入并由会话保留服务快照。
QQ QRC 转换和 MIT 许可的 DES 实现位于 `internal/provider/qqmusic/{qrc.go,qrcdes/}`，
许可证及来源提交记录随实现保留。

在线组合测试默认跳过；显式启用时只输出标题、通道状态和字节数，不转储歌词全文：

```sh
SONEXA_LYRICS_LIVE=1 go test -v ./internal/app -run '^TestLiveSources$' -count=1
# 可附加 SONEXA_LYRICS_SOURCE=qq 及 TITLE/ARTIST/ALBUM/DURATION_MS 查询变量
SONEXA_QQ_PROBE=/path/to/local-response.json go test -v ./internal/provider/qqmusic -run '^TestLocalQQProbe$' -count=1
```

迁移后的离线回归不代表当前真实来源已验通；在线与原生验证边界单独记录。

封面搜索消费 `internal/covers/sources` 的窄接口，由 CAA/iTunes/QQ/网易云实现。
CAA 协议位于 `internal/provider/caa`；MusicBrainz 仅委托图片策略。
原有 QQ/网易云封面 live 入口已迁到 App 组合测试，仍默认跳过：

```sh
SONEXA_LIVE_COVERS=1 go test -v ./internal/app -run '^TestLiveCoverSources$' -count=1
```

## 版本控制

提交手写源码、测试、测试数据、依赖锁文件、平台资源及项目工具配置。不提交依赖目录、构建产物、Wails 生成绑定、`package.json.md5`、测试报告、数据库、本机配置和 `.env`；规则见 `.gitignore`。

临时资料放 `tmp/`，临时 Go 程序放 `tmp/_scratch/`，避免被 Go 的 `./...` 扫描。Git 忽略规则不控制 Go 包发现。
