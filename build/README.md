# 构建资源与 Spotify helper 边界

`build/` 保存平台配置；生成产物不提交。

- `bin/`：Wails 主程序／`.app` 输出。
- `darwin/Info.plist`、`Info.dev.plist`：macOS 发行／开发配置。
- `windows/`：应用 manifest、图标、`info.json` 与安装器配置；现有 Wails 配置不等于已装配 helper。
- `spotify-helper/`：冻结工作目录、完整许可文本和 one-folder 产物，均由 `.gitignore` 排除。

## 已有准备与自检

按[sidecar README](../internal/provider/spotify/sidecar/README.md)准备锁定解释器、hash-locked 依赖、
许可证并执行 `freeze.py`。目前锁定和冻结证据仅为 **macOS arm64**；其他目标没有可用验证时，
脚本明确拒绝，不跨平台冻结、不回退用户 Python。

产物为 `build/spotify-helper/dist/spotify-helper/` 整个目录，包含主 helper、`_internal/`、
`licenses/` 和 `manifest.json`。不能只复制可执行文件。

已具备锁定构建环境和冻结产物时，可在仓库根目录离线自检：

```sh
V="$PWD/tmp/_scratch/spotify-audit/venv-a/bin/python"
"$V" internal/provider/spotify/sidecar/verify_frozen.py build/spotify-helper/dist/spotify-helper
SONEXA_TEST_FROZEN_HELPER=1 go test -count=1 -race ./internal/provider/spotify -run '^TestFrozenHelperOfflineIntegration$' -v
```

前者用 macOS 沙箱禁止网络及外部 Python，在中文／空格路径和无 PATH 工具环境执行自检，
校验资源不变及 helper/libpython 的 ad-hoc 签名；后者让真实 helper 通过 Go HTTP 替身读取空歌单和 205 项 fixture。
这两个命令不请求真实 Spotify，也不打开原生窗口；构建机执行自检脚本需要 Python，不意味着终端用户需要 Python。

## 主应用构建与运行时资源位置

根 [README](../README.md#开发与验证) 的 `wails generate module`、`npm run build`、`wails build` 和
`wails dev` 仍适用主应用。**当前 `wails.json` 没有 helper 装配钩子；单独运行 `wails build` 不会生成完整 Spotify 发行包。**

Go Runner 按真实主程序路径定位资源，不搜索当前工作目录或 PATH：

| 主程序位置 | 期望的 helper 目录 |
| --- | --- |
| macOS `.app/Contents/MacOS/sonexa`（开发／发行 bundle 均适用） | 同 bundle 的 `Contents/Resources/spotify-helper/` |
| 开发用平铺 `build/bin/sonexa` | `build/spotify-helper/dist/spotify-helper/` |
| 其他平铺安装目录 | 主程序旁的 `resources/spotify-helper/` |

`wails dev` 的实际主程序若位于 `.app`，优先使用 bundle 资源，不能仅凭开发冻结目录存在就认为已装配。
当前尚未提供验收完成的跨平台装配／签名流程。后续任务 4.3 必须按目标原生构建、整体复制许可和 native 依赖，
完成内层 helper/native 签名、更新摘要、外层应用签名／安装器，再验证安装及卸载资源一致性；SHA-256 不能代替发行签名。
Windows Job Object 目前仅交叉编译；Linux、其他 macOS 架构、Windows 安装器都没有完整运行验收结论。

## 完整性失败与排查

- 来源显示“运行环境缺失、损坏或版本不匹配”：先核对主程序实际位置、整个 one-folder 目录及 manifest 的系统／架构／版本。
- 文件摘要不符、资源缺失或多出文件：重新准备匹配的整个资源目录；不要把脚本或未列入 manifest 的依赖塞进安装目录。
- 不通过修改 PATH、安装系统 Python、pip/uv 或运行时下载来修复；只影响 Spotify，QQ 与离线浏览仍可用。
- helper 帧错误、进程异常或请求不完整不提交同步快照；用户可在修复资源后显式重试。
- 不删除历史用户数据来排查 helper；开发模型不兼容与 helper 不可用是两个不同错误。

最新证据见[第 11 组离线记录](../openspec/changes/add-streaming-playlist-sync/implementation-phase11-offline.md)。
本轮仅复检已有 helper 和纠正文档，**未从干净检出执行完整发行构建／安装**，4.2–4.4 与 11.5 保持未完成。
