# 构建资源与 Spotify helper

`build/` 保存平台配置；生成产物不提交。

- `bin/`：Wails 主程序／`.app` 输出。
- `darwin/Info.plist`、`Info.dev.plist`：macOS 发行／开发配置。
- `windows/`：应用 manifest、图标、`info.json` 与安装器配置。
- `spotify-helper/`：冻结工作目录、完整许可文本和 one-folder 产物，均由 `.gitignore` 排除。

## 已验证范围与前置条件

当前只有 **macOS arm64** 的 CPython 归档锁和无外部 Python 验证器。其他目标明确拒绝冻结／装配，
不得把 Windows 交叉编译或安装器模板当成该平台可发布的 Spotify 支持。

开发机需要 Go、Node/npm、Wails v2、uv、Python 3.14+（仅解包 zstd 构建元数据）及 Xcode 命令行工具。
helper 运行时携带 CPython 3.12.13，不调用这些开发工具。版本、哈希与许可来源见
[sidecar README](../internal/provider/spotify/sidecar/README.md)。

根 README 的普通 `wails build` 仍只构建主应用；它不自动下载或装配 helper。
需要带 Spotify 资源的包时，使用下面的**显式构建入口**，该入口先拒绝资源缺失／版本不符，再构建主应用并装配。
当前仅做本地 ad-hoc 签名，**不具备发行签名、公证或完整原生安装验收结论**。

## 从干净检出离线准备

先取得 sidecar README 指定的 hash-locked 构建缓存。以下允许从已下载缓存复制归档和 wheel，
但不复制已有 node_modules、绑定、dist、Python 解包目录、venv 或冻结二进制，确保重新构建。
所有命令在新检出的仓库根执行；`CACHE` 为已有 `spotify-audit` 缓存绝对路径。

```sh
export CACHE=/absolute/path/to/spotify-audit
mkdir -p tmp/_scratch/spotify-audit
cp "$CACHE/python.tar.gz" "$CACHE/python-full.tar.zst" tmp/_scratch/spotify-audit/
cp -R "$CACHE/wheelhouse" tmp/_scratch/spotify-audit/
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
```

`--offline` 缓存不存在或摘要不符即失败，不回退下载。首次获得缓存按 sidecar README 单独操作；
离线验收不能悄悄改用联网安装。uv/npm/Go 所需缓存缺失也应先报告，不绕过锁或校验。

## 构建、装配、校验 `.app`

```sh
export GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local
export npm_config_offline=true npm_config_audit=false npm_config_fund=false
(cd frontend && npm ci --offline --no-audit --no-fund)

V="$PWD/tmp/_scratch/spotify-audit/venv-a/bin/python"
S=internal/provider/spotify/sidecar
# --wails 可指向已安装的 Wails v2 绝对路径；不会自动安装。
"$V" -I "$S/package_app.py" build --platform darwin/arm64 --wails /absolute/path/to/wails

APP="$PWD/build/bin/sonexa.app/Contents/MacOS/sonexa"
"$V" -I "$S/package_app.py" verify --platform darwin/arm64 --binary "$APP"
# 通过生产 Runner 定位 bundle 中的 helper，以真实进程 + Go HTTP fixture 验证；不启动主应用。
SONEXA_TEST_FROZEN_HELPER=1 SONEXA_TEST_APP_BINARY="$APP" \
  go test -count=1 -race ./internal/provider/spotify -run '^TestFrozenHelperOfflineIntegration$' -v

git status --short
git ls-files frontend/wailsjs frontend/dist frontend/node_modules build/bin build/spotify-helper
```

显式构建会调用 `wails build -platform darwin/arm64 -m -nosyncgomod -trimpath`，生成绑定与前端资源；
不会改 go.mod，不启动原生窗口。装配步骤校验架构、逐文件摘要、完整许可证，将 helper **整体替换**到 bundle，
避免升级残留旧 native 库；依次 ad-hoc 签 helper/native、重建资源摘要、签主程序和外层 `.app`，
最后执行严格签名及无外部 Python 自检。签名失败不会被忽略，也不读取发布身份／钥匙串。

自检将 helper 复制到中文／空格路径，在 macOS 沙箱中禁止网络、资源写入和外部 Python，PATH 指向空工具目录；
读取 205 项离线 fixture。`SONEXA_TEST_APP_BINARY` 仅供测试，不是生产资源路径覆盖选项。
构建缓存和临时检出均可在确认不再使用后删除；不要清空用户应用数据。

## 开发运行与资源位置

| 主程序位置 | Runner 使用的 helper 目录 |
| --- | --- |
| `.app/Contents/MacOS/sonexa`（开发／发行 bundle 均适用） | 同 bundle 的 `Contents/Resources/spotify-helper/` |
| 平铺 `build/bin/sonexa` | `build/spotify-helper/dist/spotify-helper/` |
| 其他平铺安装目录 | 主程序旁的 `resources/spotify-helper/` |

先准备同版本 helper 再 `wails dev`。若开发程序是 `.app`，需要装配到该 bundle，而不只保留冻结目录：

```sh
"$V" -I "$S/package_app.py" assemble --platform darwin/arm64 \
  --binary "$PWD/build/bin/sonexa.app/Contents/MacOS/sonexa"
```

`assemble` 对已有主程序装配并自检，不启动它。若 Wails 重建 bundle 清除了资源，须在下一次使用 Spotify 前重新装配；
当前没有经过原生窗口验证的自动开发重建钩子。普通 Wails 构建缺 helper 时 Spotify 明确不可用，QQ／本地仍可用。

## Windows 与其他目标

`build/windows/installer/project.nsi` 增加显式 `SONEXA_SPOTIFY_HELPER` 目录宏：启用时必须存在 helper.exe、manifest 和
完整许可 inventory，将整个目录复制到 `$INSTDIR\resources\spotify-helper`；升级前清理旧目录，卸载随安装目录清理。
双架构安装器不能共用一个 helper，启用该宏时明确拒绝。该宏未启用的既有安装器不声明 Spotify 资源齐备。

**Windows 尚无归档锁、冻结／签名／NSIS 实编及安装测试，不能发布声称 Spotify 可用的 Windows 包。**
Windows Job Object 的原生树回收／句柄测试已提供，可在 Windows 执行；本机只能交叉编译。
Linux 和其他 macOS 架构同样没有完整发行证据，不由本机结果推定支持。

## 完整性失败与排查

- 缺失、架构或版本错误：核对主程序真实位置及整个 one-folder 目录，不能只复制可执行文件。
- 摘要不符、额外资源或许可证不完整：重新准备匹配目录，不手改 manifest 绕过检查。签名后由装配脚本重建摘要。
- 不安装用户 Python、不修改 PATH 作为运行时回退，不运行在线修复；helper 失败不影响 QQ 与本地浏览。
- 错误帧、退出阻塞和不完整结果不生成成功快照；恢复资源后由用户显式重试。
- macOS ad-hoc 校验不能代替发布签名、公证、升级与原生窗口验收，任务 4.2、4.3、11.5 仍按平台保留边界。
