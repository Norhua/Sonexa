# 一键构建完整 Sonexa（含 Spotify helper）

**普通 `wails build` 只构建主应用，不下载或装配 Spotify helper。** 若因此提示“Spotify 运行环境缺失、损坏或版本不匹配”，使用以下入口重建完整应用，而不是给运行中的应用安装 Python 库。

## 前置工具（仅编译机）

- Go：满足根目录 `go.mod` 的版本要求。
- Node.js/npm：满足前端 Vite 的版本要求，建议当前 Node LTS。
- **Python 3.14+**：仅用于构建调度及标准库 zstd 解包。Unix 命令为 `python3`，Windows 使用 `py -3` 或 PATH 中的 `python`（脚本检查版本至少 3.14）。
- 平台原生工具链：macOS 的 Xcode Command Line Tools；Windows 的 Wails v2 所需 C/CGO 工具链和 WebView2；Linux 的 C 编译器、`pkg-config`、GTK 3、WebKitGTK 开发包。Ubuntu 24.04 可安装 `build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev`，其他发行版按其包名安装。
- Wails v2 可预先安装；脚本依次查 PATH、`go env GOBIN/GOPATH`（包括 `~/.go/bin/wails`），没有时按 `go.mod` 版本安装到私有 `build/spotify-helper/tools/`。

脚本**不会调用 sudo、修改系统 Python、全局 pip install 或安装系统 SDK**。上述基础工具需先安装；不再需要 uv 或手动准备 Python 库。应用使用者不需要这些编译工具。

## 一条命令

在仓库根目录运行，或从任意目录使用脚本完整路径：

```sh
# macOS / Linux
./build.sh
# Wails 不在常规位置时
./build.sh --wails "$HOME/.go/bin/wails"
```

```bat
rem Windows（cmd，或 PowerShell 中 .\build.bat）
build.bat
rem 自定义 Wails 路径
build.bat --wails "C:\Tools\wails.exe"
```

默认构建当前系统/架构。`--platform` 只能指定与当前环境一致的目标，**不支持交叉冻结或 universal 包**。
支持的脚本目标：macOS amd64/arm64、Windows amd64/arm64、Linux glibc amd64/arm64。不支持 Alpine/musl。
Linux 自动检测 WebKitGTK 4.1 并传入 `webkit2_41`；必要时可显式 `./build.sh --tags webkit2_41`。

执行顺序：

1. 检查目标与工具；下载 `python.lock.json` 中该平台的 CPython 3.12.13 / PBS 20260807 归档及许可元数据，核对 SHA-256 后解包。
2. 下载 `requirements.lock` 中精确锁定且带哈希的 wheel；重新建立私有 venv、离线安装、`pip check`。
3. 收集完整许可证，PyInstaller 6.22.3 one-folder 冻结 SpotifyScraper 3.9.2 helper，写逐文件 manifest。
4. 冻结 helper 离线自检；`go mod download`、`npm ci`、Wails 绑定/前端/原生构建。
5. 整体装配 helper 与许可证，macOS 进行 ad-hoc 签名并刷新摘要，再校验/自检，输出完整应用。

任一步失败立即停止，不把主二进制当作完整包，不禁用 TLS 或哈希校验。构建允许联网下载依赖；**应用运行时不下载、不自动安装、不使用 PATH Python**。

## 产物与验证边界

| 目标 | 完整产物 | 本次验证状态 |
| --- | --- | --- |
| macOS arm64 | `build/release/darwin-arm64/sonexa.app` | 已跑通在线一键构建、冻结自检、签名检查、Go Runner 定位及离线协议往返 |
| macOS amd64 | `build/release/darwin-amd64/sonexa.app` | 锁/脚本/目标映射已提供，未在该架构运行 |
| Windows amd64/arm64 | `build/release/windows-<arch>/sonexa/` | 锁/脚本/装配契约已提供，未在 Windows 运行 |
| Linux amd64/arm64 | `build/release/linux-<arch>/sonexa/` | 锁/脚本/装配契约已提供，未在 Linux 运行 |

**分发整个 `.app` 或 `sonexa/` 目录，不能只复制 `sonexa.exe`/`sonexa`。** 平铺目录自带 `resources/spotify-helper/`。Linux 建议在最老的预期部署发行版构建，仍需目标机的 GTK/WebKit 系统库。

各目标脚本都以中文/空格路径、空 PATH、无效 PYTHONHOME/PYTHONPATH 启动冻结 helper，核对 205 项 fixture 和资源未修改；macOS 额外使用 OS sandbox 禁网、拒绝外部解释器/构建目录访问并验证签名。Windows/Linux 自检依靠 helper audit hook + 本地 fixture，不冒称有 OS 沙箱或已完成干净机器验收。

当前不生成 NSIS 安装器、AppImage、DMG 或发行签名/公证。Windows 安装器模板的 `SONEXA_SPOTIFY_HELPER` 宏仍可装配整个 helper，但 NSIS 实编/安装/升级尚未验收。所有平台的原生 UI、发行签名及 Spotify 真实在线读取须另行验证；本机成功不证明全部平台可发行。

## 缓存与离线重建

默认私有缓存为 `build/spotify-helper/cache/<target>/`，包含 `python.tar.gz`、`python-full.tar.zst`、`wheelhouse/`。可用 `--cache /absolute/path` 复用同目标缓存；每次重新校验归档、解包解释器和创建 venv，避免复用被改动的 Python。

已有全部 Python、Go、npm 缓存及 Wails 后：

```sh
./build.sh --offline
```

Windows 同样支持 `build.bat --offline`。离线模式缺缓存或摘要不符直接失败，不回退下载；Go/npm 自带缓存不在 Python 缓存目录内。
`bin/`、`release/`、`spotify-helper/` 都是 Git 忽略的构建产物，不提交生成绑定、依赖或二进制。

## 开发运行与独立装配

Runner 只按主程序位置定位资源，不搜索 cwd/PATH：

| 主程序位置 | helper 目录 |
| --- | --- |
| `.app/Contents/MacOS/sonexa` | 同 bundle 的 `Contents/Resources/spotify-helper/` |
| 平铺 `build/bin/sonexa[.exe]` | `build/spotify-helper/dist/spotify-helper/` |
| 其他平铺目录 | 主程序旁的 `resources/spotify-helper/` |

先完整构建再 `wails dev`。Wails 重建 `.app` 后可能清除已装配资源，须重跑完整构建或显式装配（示例 macOS arm64）：

```sh
V=build/spotify-helper/cache/darwin-arm64/build-venv/bin/python
S=internal/provider/spotify/sidecar
"$V" -I "$S/package_app.py" assemble --platform darwin/arm64 \
  --binary "$PWD/build/bin/sonexa.app/Contents/MacOS/sonexa"
"$V" -I "$S/package_app.py" verify --platform darwin/arm64 \
  --binary "$PWD/build/release/darwin-arm64/sonexa.app/Contents/MacOS/sonexa"
SONEXA_TEST_FROZEN_HELPER=1 \
  SONEXA_TEST_APP_BINARY="$PWD/build/release/darwin-arm64/sonexa.app/Contents/MacOS/sonexa" \
  go test -count=1 -race ./internal/provider/spotify -run '^TestFrozenHelperOfflineIntegration$' -v
```

`package_app.py build` 是底层显式入口，默认离线，`--online` 才允许 Go/npm 下载；一键入口自动传递该选择。

## 故障排查

- **运行环境缺失**：确认打开的是新生成完整应用而非旧 `.app` 或单独复制的主二进制；普通 `wails build` 后重新执行完整入口。
- **摘要/版本/架构不符**：重建整个目录，不手改 manifest，不将不同架构的 helper 混用；所有内层签名完成后重新生成摘要。
- **SHA-256 不符**：停止；确认网络/缓存来源。若确认为损坏下载，只删除对应缓存归档再运行，不跳过校验。
- **缺 Python 3.14、Go、npm 或系统库**：安装上述编译前置工具；不要向系统 Python 安装 SpotifyScraper。
- **Linux `.so` 链接**：冻结阶段仅把指向 bundle 内普通文件的符号链接实体化，指向外部库的链接拒绝打包。
- **Windows 长路径**：构建目录尽量短，必要时开启系统长路径支持；运行时仍验证中文/空格路径。
- helper 不可用不影响 QQ 和本地浏览。脚本不启动主应用、不修改/删除用户应用数据。

锁定库、传输边界、历史审计见 [sidecar README](../internal/provider/spotify/sidecar/README.md)；本轮证据及规格变更见 [change_record](../openspec/changes/add-streaming-playlist-sync/change_record/1-tasks.md)。
