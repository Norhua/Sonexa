# Spotify helper 依赖与许可证

本清单对应 `requirements.lock`、`python.lock.json` 的精确版本，第三方代码未作全局补丁。完整许可文本由 `collect_licenses.py` 从已校验的 wheel / Python 构建包收集到发行产物的 `licenses/`；`inventory.json` 记录原始路径及每份文本的 SHA-256，保留 setuptools 内嵌依赖的许可，不能仅复制本清单代替许可文本。

## Python 包

来源为 [PyPI](https://pypi.org/) 各包对应版本的发布 wheel。Python 3.12 / macOS arm64 安装 14 包；Windows 条件包另行下载并核对许可，但未在 Windows 安装或验收。

| 包 | 版本 | 许可 | 用途 |
| --- | --- | --- | --- |
| SpotifyScraper | 3.9.2 | MIT；Copyright (c) 2025 Ali Akhtari | 运行时读取/解析 |
| httpx | 0.28.1 | BSD-3-Clause | 库依赖；不使用默认 HTTP 客户端发网 |
| httpcore | 1.0.9 | BSD-3-Clause | httpx 传递依赖 |
| anyio | 4.16.0 | MIT | httpx 传递依赖 |
| certifi | 2026.10.23 | MPL-2.0 | httpx 传递依赖；CA 数据未修改 |
| h11 | 0.16.0 | MIT | httpcore 传递依赖 |
| idna | 3.20 | BSD-3-Clause | httpx/anyio 传递依赖 |
| typing_extensions | 4.16.0 | PSF-2.0 | anyio 传递依赖 |
| PyInstaller | 6.22.3 | GPL-2.0-or-later + Bootloader exception；运行时 hook 为 Apache-2.0 | 构建；产物嵌入 bootloader / loader / runtime hooks |
| pyinstaller-hooks-contrib | 2026.9 | 构建 hook 为 GPL-2.0-or-later；runtime hooks 为 Apache-2.0 | 构建 |
| altgraph | 0.17.5 | MIT | 构建 |
| packaging | 26.3 | Apache-2.0 OR BSD-2-Clause | 构建 |
| setuptools | 84.2.1 | MIT；内嵌包许可分别保留 | 构建 |
| macholib | 1.16.4 | MIT | macOS 构建 |
| pefile | 2024.8.26 | MIT | Windows 构建条件依赖 |
| pywin32-ctypes | 0.2.3 | BSD-3-Clause | Windows 构建条件依赖 |

PyInstaller 的 `COPYING.txt` 明确允许将未修改 bootloader 及 loader 与其他程序组合后分发；工具本身不因该例外变成 MIT，修改或单独分发工具仍适用其许可。冻结产物不包含 pip、浏览器、用户 Cookie、MCP 或媒体下载 extras，也不运行这些入口。

## Python 及 native 依赖

使用 [python-build-standalone](https://github.com/astral-sh/python-build-standalone) `20260807` 的 CPython `3.12.13`，`python.lock.json` 包含 Darwin/Windows/Linux glibc 各 amd64/arm64 目标。可执行归档与完整元数据归档的 SHA-256 均与同次发布 `SHA256SUMS` 核对。解释器二进制及 native 代码由归档摘要整体锁定，不把本机解释器文件哈希冒充上游归档哈希。以下动态库核对结果仅来自 macOS arm64；其他目标构建时分别收集本平台归档许可，不套用 macOS 清单。

- CPython：Python-2.0 / CNRI-Python 及其合并许可，完整 `LICENSE.cpython.txt` 保留。
- Python 元数据列出的静态/native 组件包括 OpenSSL（Apache-2.0，旧版 OpenSSL 许可文本一并保留）、libffi（MIT）、bzip2、liblzma（0BSD）、mpdecimal（BSD-2-Clause）、expat（MIT）；逐模块关联见生成的 `nativeModules`。
- macOS 系统库（libSystem、ncurses、panel、libedit、zlib、CoreFoundation、SystemConfiguration）不复制系统二进制。`otool -L` 已核对冻结解释器只引用 bundle 相对路径和系统路径，没有 Nix、Homebrew 或构建环境路径。
- 此目标的 zlib 为系统 `/usr/lib/libz.1.dylib`（运行时报告 1.2.12）。PBS 通用元数据同时列出 zlib-ng 许可路径但归档没有该文件；`links.system=true` 表明此目标不携带 zlib-ng。不能据此要求用户安装另一个 zlib 或虚称归档包含不存在的文件。
- 为不漏掉静态合入解释器的许可，收集完整 PBS `python/licenses/`，包括未被 helper 调用的模块许可；清单不表示 helper 启用了这些能力。

尚未完成完整 Sonexa 安装包许可呈现及 Windows/Linux native 核对，属于任务 4.3、11.5；本清单不宣称 Spotify 服务条款合规结论。
