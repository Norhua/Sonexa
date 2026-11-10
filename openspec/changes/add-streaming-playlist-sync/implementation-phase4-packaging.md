# 第 4 组进程与构建验收

## 当前进度

本轮继续采用批量实现、分组测试，完成 **4.4**，累计 **55/59**。
4.2、4.3 做了实现与本机验证，但保留未完成；11.4、11.5 未执行。
没有请求真实 QQ/Spotify、启动主应用原生窗口、安装系统工具、访问签名身份或执行安装器。

## 实现

### 4.2 进程与通信

- `runner.go` 检测帧头／正文短写，Runner 自身施加 15 分钟总期限。
- 错误／结果退出超时时，先清理整个 Windows Job／Unix 进程组再 Wait，避免后代持有 stdout/stderr 管道拖延退出。
- 取消监视 goroutine 增加结束通知，返回前等待退出，避免读取已结束 Run 的状态或延后清理。
- 提取纯资源定位函数，生产逻辑不变，覆盖中文／空格路径、bundle 与平铺开发布局；没有生产环境变量覆盖路径。
- 新增不依赖短 deadline 的错误 ID、版本、帧、总输出、崩溃与退出阻塞测试，防止把超时取消当作协议拒绝通过。
- Unix 替身后代继承管道，覆盖错误帧和先返回结果后后代仍阻塞，验证回收。
- 新增 Windows 原生 Job 后代回收与句柄计数测试。本机仅交叉编译 amd64/arm64；首次编译发现测试常量包引用错误，修正为 `windows.ERROR_INVALID_PARAMETER` 后两目标通过。**未运行 Windows 测试，不勾 4.2。**

### 4.3 资源装配

- `sidecar/artifacts.py` 统一冻结与构建自检的版本、目标、逐文件摘要和许可文本校验，拒绝缺失／额外／不安全路径／符号链接。
- `freeze.py` 使用共享清单与许可核验，仍仅接受已有精确锁的原生目标，不添加未经验证的 Windows/Linux 归档哈希。
- `prepare_python.py --offline` 只接受已缓存且摘要正确的 Python 归档，缺失或损坏不联网补救。
- `package_app.py` 提供显式 `check/build/assemble/verify`。普通 `wails build` 不受影响，也不虚称已携带 Spotify。
- 显式 build 强制 Go/npm 离线与本地 toolchain，先校验 helper，再调用 Wails 生成绑定／前端／主程序。
- macOS 装配核对 Mach-O 架构，把整个 one-folder 目录及许可替换进 `.app/Contents/Resources/spotify-helper`；不把旧 native 依赖合并保留。
- 依次 ad-hoc 签 helper/native、重建 manifest 摘要、签主程序和外层 `.app`，严格校验并运行无外部 Python 自检。只使用 `--sign -`，不使用发布身份／公证服务。
- Windows NSIS 增加显式 `SONEXA_SPOTIFY_HELPER` 宏，启用时要求主 helper、manifest 与许可 inventory，整目录复制及升级清理；拒绝一份 helper 混用双架构。**没有 NSIS 实编或安装／卸载证据，不勾 4.3。**
- Linux 及其他 macOS 架构无已验证锁／发行流程，仍明确拒绝，不宣布支持。

## 4.4 干净检出步骤与证据

完整可复跑步骤在 [build/README.md](../../../build/README.md)，helper 准备、开发资源位置、显式构建、故障处理与边界同时更新到根和 sidecar README。

本轮以 Git 暂存源码树 `65abb6227cc8563b6bcf5dab298556c7df9958cb` 导出到新的临时目录，初始化本地验收仓库。
开始时不存在 node_modules、wailsjs、dist、venv、解包 Python 或冻结二进制。最终源码与该树相比只追加验收文档／任务状态，不改本轮已测实现。

实际目录：`/tmp/sonexa-clean-build-6tbWnU`。仅复制已有 hash-locked `python.tar.gz`、`python-full.tar.zst` 与 wheelhouse，
不复制以前的 node_modules、绑定、dist、venv、Python 解包内容或冻结产物。Go/npm/uv 使用本机缓存，均显式离线。
**干净源码重新构建不等于没有缓存／没有构建工具的终端用户系统**；终端用户无外部 Python 用独立沙箱自检证明。

按文档实际执行：

```sh
python3 internal/provider/spotify/sidecar/prepare_python.py --offline tmp/_scratch/spotify-audit
P="$PWD/tmp/_scratch/spotify-audit/python/bin/python3.12"
V="$PWD/tmp/_scratch/spotify-audit/venv-a/bin/python"
S=internal/provider/spotify/sidecar
uv --no-config venv --offline --python "$P" tmp/_scratch/spotify-audit/venv-a
uv --no-config pip sync --offline --python "$V" --no-index --no-build --require-hashes \
  --find-links tmp/_scratch/spotify-audit/wheelhouse "$S/requirements.lock"
"$V" -I "$S/collect_licenses.py" --wheelhouse tmp/_scratch/spotify-audit/wheelhouse \
  --python-metadata tmp/_scratch/spotify-audit/python-metadata/python --output build/spotify-helper/licenses
"$V" -I "$S/freeze.py"
"$V" -I "$S/verify_frozen.py" build/spotify-helper/dist/spotify-helper
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local npm_config_offline=true npm_config_audit=false npm_config_fund=false
(cd frontend && npm ci --offline --no-audit --no-fund)
"$V" -I "$S/package_app.py" build --platform darwin/arm64 --wails /Users/amadeus/.go/bin/wails
APP="$PWD/build/bin/sonexa.app/Contents/MacOS/sonexa"
"$V" -I "$S/package_app.py" verify --platform darwin/arm64 --binary "$APP"
SONEXA_TEST_FROZEN_HELPER=1 SONEXA_TEST_APP_BINARY="$APP" \
  go test -count=1 -race ./internal/provider/spotify -run '^TestFrozenHelperOfflineIntegration$' -v
```

结果：
- 重新解包 CPython 3.12.13，14 个精确依赖重新安装，许可收集与 one-folder 冻结通过。
- Wails 2.10.2 为 macOS 27.2 / arm64 生成绑定、构建前端和主应用，显式装配得到约 41 MiB `.app`。
- 打包副本中的 helper 自检 `rawSeen=205`，禁网、外部 Python 不可读、PATH 无工具、中文／空格路径、资源只读均通过；helper 与解释器、外层 `.app` ad-hoc 严格校验通过。
- `SONEXA_TEST_APP_BINARY` 仅在测试中使用；生产 Runner 按 `.app` 中实际路径定位资源，真实 helper + Go HTTP fixture 完成明确空歌单与 205 项分页／补取往返。没有启动 Sonexa 主窗口。
- 临时复制完整 `.app`，分别删除 libpython、添加额外文件、改错目标、删除许可 inventory，四种情况均在启动 helper 前拒绝；完整产物未被这些负向测试修改。
- 新检出 `git status --short` 无输出；`git ls-files frontend/wailsjs frontend/dist frontend/node_modules build/bin build/spotify-helper` 无输出。
- 没有执行 Windows 安装器、发布签名、公证或原生窗口。4.4 是当前已具备锁的 macOS arm64 构建流程／文档验收，不代替 4.3 的其他平台交付。

## 分组与最终回归

```sh
go test -count=1 -race ./internal/provider/spotify ./internal/playlists ./internal/app
go test -count=1 -race ./internal/...
go vet ./...
GOOS=windows GOARCH=amd64 go test -c ./internal/provider/spotify -o tmp/_scratch/playlist-packaging/spotify-windows.test.exe
GOOS=windows GOARCH=arm64 go test -c ./internal/provider/spotify -o tmp/_scratch/playlist-packaging/spotify-windows-arm64.test.exe
# 锁定 Python 离线 unittest（adapter + artifacts）
"$V" -I -c 'import sys,unittest; sys.path.insert(0,"internal/provider/spotify/sidecar"); r=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.discover("internal/provider/spotify/sidecar/tests",top_level_dir="internal/provider/spotify/sidecar")); sys.exit(not r.wasSuccessful())'
(cd frontend && npm test)
openspec validate add-streaming-playlist-sync --strict
git diff --check
```

- Go 全量 25 包中 24 包有测试并通过，qrcdes 无独立测试；race/vet 通过。
- Python 最终 **23/23**，包含构建强制离线、资源位置、升级旧文件清理、清单及许可错误拒绝。
- 干净检出前端单测 **111/111**，类型检查／Vite 构建由 Wails 执行通过。
- 本轮未改前端源码／浏览器测试，没有再跑 Playwright；上一轮 304/304 仅作为历史证据，不混为本轮结果。
- OpenSpec 严格校验、diff 检查、相对文档链接及生成产物不跟踪检查通过。
- 原始日志为 `tmp/_scratch/playlist-packaging/`，不提交日志、缓存或二进制。

## 剩余条件

1. **4.2**：在 Windows 原生环境执行新树／句柄组，不能靠交叉编译勾选。
2. **4.3**：明确实际发布系统／架构后补平台 Python 锁、native 依赖与许可、安装器／发行签名校验。当前 macOS arm64 仅 ad-hoc 本地包。
3. **11.4**：默认关闭的有界 live 入口仍待实施与获准样本；没有公开测试歌单／观察增删授权，不发送请求。
4. **11.5**：实际发布目标的完整安装、原生窗口、升级一致性与签名人工验收。

不归档，不缩小剩余任务的验收标准。继续前需提供相应平台环境或明确在线／原生测试条件。
