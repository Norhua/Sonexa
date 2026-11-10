# 1 — QQ 歌单修复与跨平台完整构建

## 执行计划

- [x] 1.1 对用户提供的 QQ 公开歌单进行有界只读排查，确认真实分页字段；记录原因并建立离线回归样本。
- [x] 1.2 修复 QQ 官方移动分享 URL 白名单与分页解析，覆盖参数冲突、误导主机、真实名称/总数变化和不完整保护；按追问选择新增带稳定身份的过滤项与已有条目保护，预览展示原因。
- [x] 1.3 补充 Windows/Linux/macOS 原生目标的锁定 Python 资源及准备、许可、冻结、自检和装配支持；保持无运行时安装。
- [x] 1.4 新增 shell/bat 一键入口，自动准备 Python/依赖、前端与 Go 依赖，构建并生成可整体分发的完整产物；覆盖缺工具、失败停止及目标选择测试。
- [x] 1.5 执行 Go/Python 回归与本机完整构建/helper 自检；记录其他平台未执行的原生验收，不冒称全平台通过。
- [x] 1.6 更新构建说明、共享 spec-diff 及本记录，检查差异并以中文约定式提交本次修改（排除原有 go.mod 改动）。

## 验证记录

环境：macOS 27.2 arm64，Go 1.26.10，Wails 2.10.2；引导 Python 3.14.8，冻结 CPython 3.12.13 / SpotifyScraper 3.9.2 / PyInstaller 6.22.3。

### 已通过

- `SONEXA_QQ_PLAYLIST_LIVE=9385819575 go test -count=1 ./internal/provider/qqmusic -run '^TestPlaylistLiveReadOnly$' -v`：生产 Provider、QQ 30 次/分钟、最多 20 次实际发送、1 分钟期限、无重试、不写数据库；11 次实际发送，11 页，1064 首可读 + 3 个过滤项 = 1067 原始项。共 33 次门禁检查不等于发送数。
- `go test -count=1 -race ./internal/...`：全量通过；补充最终过滤计数/已导入条目保护测试后，`go test -count=1 -race ./internal/playlists ./internal/provider/qqmusic ./internal/store` 再次通过。
- Python `unittest discover`：32 项通过，覆盖六目标映射、Windows npm 无 shell 调用、缺工具、失败传播、纯标准库 bootstrap help、资源/许可校验、便携验证器及内部 `.so` 链接实体化；跨平台部分为契约/替身测试。
- `./build.sh`：从新的 `build/spotify-helper/cache/darwin-arm64` 缓存目录获取归档及 wheel，完整编译/装配/自检通过；自动定位 `~/.go/bin/wails`。日志 `tmp/_scratch/build-change-1.log`（忽略目录）。
- `./build.sh --offline`：使用上述缓存重新解包、创建环境、冻结、npm ci、Go/Wails 编译、装配/自检通过；日志 `tmp/_scratch/build-change-1-offline.log`。
- 产物 `build/release/darwin-arm64/sonexa.app` 约 41 MiB，包含完整 helper。macOS sandbox 禁网、拒绝外部 Python、空 PATH、中文/空格路径、资源不改写与严格 ad-hoc 签名检查通过。
- `SONEXA_TEST_FROZEN_HELPER=1 SONEXA_TEST_APP_BINARY="$PWD/build/release/darwin-arm64/sonexa.app/Contents/MacOS/sonexa" go test -count=1 -race ./internal/provider/spotify -run '^TestFrozenHelperOfflineIntegration$' -v`：实际冻结进程与生产 Runner 路径定位通过，空歌单及 205 项分页/补取走 Go HTTP 替身。
- `cd frontend && npm test`：21 个文件 / 111 项通过。完整构建中的 `vue-tsc --noEmit && vite build` 通过。
- `SONEXA_E2E_PREVIEW=1 npm run test:e2e -- tests/e2e/synced-playlists.spec.ts`：Chromium/WebKit 共 28 项通过，含过滤数量、原因、未知位置与两个预览的展示。
- `go vet ./...`、`openspec validate add-streaming-playlist-sync --strict`、`git diff --check`、shell 语法与 Python 编译检查通过。
- 新增 Python 目标摘要与发布方 20260807 `SHA256SUMS` 核对；额外下载校验 Windows amd64 完整元数据归档并确认 PYTHON.json/许可结构，但未执行其中的 Windows 程序。
- `git ls-files build/release frontend/wailsjs frontend/dist build/spotify-helper` 为空，产物未纳入 Git。原 go.mod 改动保持原样。

### 调试中的失败与修正

- 最初 live 测试把门禁检查数误当发送数，过早触发诊断上限；已改为独立 RoundTripper 实际发送上限/计数并配置 QQ 桶，未降低生产保护。
- 一键脚本最初通过 verifier → protocol 导入 adapter，错误要求系统预装 SpotifyScraper；已拆出零依赖 errors.py，`python -I -S ... --help` 测试保证 bootstrap 不依赖第三方包。
- 新增 Chromium 用例初次在弹窗淡出期间聚焦侧栏，焦点被旧 modal 恢复导致点击超时；改为等待弹窗及遮罩确实卸载后操作，重跑 28 项全部通过，未修改产品交互绕过问题。

### 保留的边界

- Windows/macOS amd64/Linux 未原生执行一键构建、无 Python 机器安装或 UI 验收，脚本/锁/契约已提供但不宣称发行通过。
- Spotify 仅验证完整运行环境及离线协议，未提供真实 Spotify 测试歌单，未验证当前上游匿名读取是否受限制。
- QQ 未通过真实应用提交绑定/观察上游增删，不改写实际用户歌单；不据此完成原 11.4 全部任务。
- 不创建 NSIS/AppImage/DMG，不使用发布身份或公证；不改写原 change tasks/specs，不提前归档。
- QQ 同步身份改为数字 ID；依项目早期约定，不迁移旧 MID 同步数据。旧测试数据应清空或重建绑定；未自动清空任何应用数据。

### 记录与交付

共享 `spec-diff.md` 已记录用户批准的过滤语义及构建场景，`AGENTS.md` 已持久化 apply 后使用递增 change_record 的规则。提交只包含本轮源码、测试、构建锁与文档，排除已有 go.mod 改动及生成产物。
