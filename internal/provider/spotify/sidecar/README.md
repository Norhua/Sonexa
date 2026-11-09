# Spotify 辅助程序：前置验证记录

**状态：仅依赖锁定草案与源码审计，尚未提供可运行 helper。**
本目录对应 `add-streaming-playlist-sync` 的任务 1.1–1.3；三项均未完成，不能据此声明 Spotify 支持。

## 当前文件

- `requirements.in`：候选运行/构建依赖，不启用浏览器、登录、媒体、CLI 或 MCP extras。
- `requirements.lock`：由 uv 0.13.5 生成的精确版本与 SHA-256，包含平台条件依赖。
  尚未在隔离环境完成两次锁定安装及比对，**不是已验证的发行锁**。
- 第三方 wheel、解包源码和隔离 Python 位于 Git 已忽略的 `tmp/_scratch/spotify-audit/`，不随代码提交。

## 依赖与来源

| 组件 | 本次选定版本 | 已获得的证据 |
| --- | --- | --- |
| CPython | 3.12.13，python-build-standalone 构建 20260807 | uv 成功安装 Darwin arm64 构建；归档 SHA-256 尚未独立取得和锁定 |
| SpotifyScraper | 3.9.2 | 从 PyPI 获取 wheel 并核验 SHA-256，已阅读歌单读取、分页、解析、匿名会话和传输相关源码 |
| PyInstaller | 6.22.3 | PyPI 元数据与锁文件；未安装或冻结验证 |
| 传递依赖 | 见 `requirements.lock` | 解析得到 16 个包（含平台条件）；未完成逐包源码/许可证核对 |

SpotifyScraper wheel：

```text
https://files.pythonhosted.org/packages/c4/95/22a70cadd41f73b95988a5818127ee80c9b83129418e960d089a712304a0/spotifyscraper-3.9.2-py3-none-any.whl
sha256: 8a83af7f6569864f409e406110988adc58ae6d433b9527b4a25be5dfebcd148c
```

上游仓库：[AliAkhtari78/SpotifyScraper](https://github.com/AliAkhtari78/SpotifyScraper)。本次审计对象是已校验的发布 wheel，不是未经核对的仓库分支。

### 许可证核对进度

这不是完整发行许可证清单，不可直接用于发布：

- SpotifyScraper：wheel 中 `spotifyscraper-3.9.2.dist-info/licenses/LICENSE` 为 MIT，版权为 `Copyright (c) 2025 Ali Akhtari`，已阅读完整许可。
- PyInstaller：PyPI 元数据声明 GPLv2-or-later，附允许分发非自由程序的特殊例外；仍须核对安装包内完整许可和例外文本。
- CPython、冻结时收集的 native 库以及所有传递依赖：待核对并随实际发行产物收录许可，不以顶层包许可替代。

## SpotifyScraper 3.9.2 源码审计

以下结论来自源码，**不是离线 fixture 或真实来源验收结果**。

### 传输扩展点

`SpotifyClient(transport=...)` 有公开注入点，`http/transport.py` 中的同步 `Transport` 只要求 `get(url, headers=...)` 与 `close()`。
响应提供 `status_code / headers / text / content / json()`。
注入 transport 时不会建立默认 `HttpxTransport`，且客户端不负责关闭注入的 transport。

已检查的歌单入口中，embed、pathfinder 分页和匿名 token provider 均使用该 transport。
后续应实现局部 stdio bridge，由 Go 做真实 HTTP、允许目标、门禁、取消、预算、重试与重定向检查。
这只证明存在可用扩展点，尚不能代替 socket 拦截测试证明无隐藏直连。

默认 `HttpxTransport` 会跟随重定向并对 403/429 重试，不可用于 Sonexa 生产读取。
`_fetch_union` 遇 `TokenError` 会刷新 token 再请求，匿名 provider 还含默认 bootstrap track。
适配时须在 bridge 对鉴权/受限响应立即停止，不能让库的刷新分支绕过限制，也不能自动读取默认样例歌曲。

### 完整性风险

- `get_playlist()` 默认 `max_tracks=100`；传入 `None` 才尝试继续分页。
- `_paginate` 正确按原始页长推进 offset，但吞掉分页错误、记录警告并返回已取得部分；不重验每页 total 或重复页。
- `_get_entity` 对 `ParsingError / NetworkError` 降级为 embed；`parse_playlist_embed` 不提供可核对的总数。
- `parse_playlist_tracks_page` 忽略缺失 `itemV2/data` 和全部非 `Track` 项，不能单从输出歌曲数推断覆盖完整。
- `_playlist_track` 要求歌曲 URI 与名称，缺少时长则输出 `0`；部分专辑对象缺 URI/名称也会解析失败。因此不能直接把库模型的 `0` 当已知时长，或把基础字段缺失当成员消失。

后续局部适配需要用库读取/解析扩展点保留原始页，独立核对身份、稳定总数、原始位置、明确排除项和重复页；禁止把截断或 embed 降级结果作为可提交快照。
可能用到 `_fetch_next_data / _fetch_union` 等私有方法，升级必须重新审计并运行契约 fixture。
基础字段兼容处理仍须保留缺失状态、使用同一稳定 track 补取，不复制 demo 的 CSV、默认歌单或另一套业务解析器。

## 本轮阻塞与验证范围

执行环境为 macOS / arm64。未访问真实 Spotify 或 QQ 歌单。

已执行：

1. 获取 PyPI 发布元数据，下载 SpotifyScraper wheel 并比对发布 SHA-256。
2. 用 `uv --no-config -v python install cpython-3.12.13-macos-aarch64-none --install-dir <隔离目录> --no-bin --no-registry` 成功准备构建用 Python；未修改用户默认解释器。
3. 用该 Python 执行 `uv --no-config pip compile ... --universal --generate-hashes --index-url https://pypi.org/simple`，生成包锁草案。

阻塞：尝试从 uv 下载日志给出的同一地址获取归档以记录 SHA-256，返回 **HTTP 403 Forbidden**：

```text
https://releases.astral.sh/github/python-build-standalone/releases/download/20260807/cpython-3.12.13%2B20260807-aarch64-apple-darwin-install_only_stripped.tar.gz
```

此前尝试获取 uv 的 GitHub 构建元数据时返回 HTTP 404。当前不能断言这些错误的具体原因；uv 下载成功也不能替代可提交的解释器归档锁与复现证据。
未把本机已安装文件的哈希冒充发行归档哈希，没有关闭 TLS 校验或回退到用户 Python 作为运行时方案。

尚未执行：锁定安装复现、完整许可证收集、分页/缺字段/过滤/截断 fixture、socket 拦截、one-folder 冻结、无外部 Python 自检、中文空格路径自检。
**没有任何系统/架构通过 helper 验收**；macOS arm64 只是本轮准备环境，不代表已支持平台。

继续前须解决解释器归档及可信哈希的获取，或由操作者提供可核验的归档/元数据；之后按任务分组实现和测试。当前阻塞不是 SpotifyScraper 方案已被证明不可行，也不允许绕过前置验证直接实现并发布后续业务。
