# Spotify 辅助程序

**状态：已接入歌单能力、App 与同步界面，离线验证通过；完整发行包、真实歌单及原生窗口尚未验收。**

SpotifyScraper `3.9.2` 通过局部适配提供原始分页读取/解析，helper 使用 PyInstaller `6.22.3` one-folder 冻结，携带 CPython `3.12.13`（PBS `20260807`）。用户运行时不需要 Python、pip、uv 或下载。包版本/哈希见 `requirements.lock`，解释器归档及发布校验来源见 `python.lock.json`，许可证见 [LICENSES.md](LICENSES.md)。

## 目录与边界

- `adapter.py`：锁定库版本的分页/字段适配与完整性核对。
- `protocol.py`：大端 uint32 长度 + UTF-8 JSON 帧；只通过 stdio 请求 Go 代为 HTTP，不实现直连。
- `helper.py`：版本握手、一次读取和 `--self-test`。外部输入为帧中数据，不作为命令/文件路径。
- `tests/`：合成原始响应 fixture、HTTP 对端替身、分页/协议保护测试。
- `collect_licenses.py / freeze.py / verify_frozen.py`：仅开发/构建使用；产物统一位于 Git 忽略的 `build/spotify-helper/`。

协议版本为 1，消息携带 `requestId / kind`；HTTP 交换另有递增 sequence。响应 body 使用 Base64，status / headers 单独传递；错误为有限 code 和脱敏信息。当前限制：单帧/最终结果 32 MiB，单响应 8 MiB，累计响应 256 MiB，最多 500 次请求、200 页、20,000 原始项。Go 通过 `provider/spotify` 独立实施超时、每次实际发送计数、允许目标、动态门禁、共享桶和进程取消，Python 检查不代替 Go 保护；Windows 实际进程与发行包验收仍未完成。

`helper.py` 的 audit hook 拒绝 Python socket 出网及外部进程启动。这是防误调用的附加保护，不是针对恶意 native 扩展的完整沙箱；本机冻结验证另用 OS sandbox 禁网并禁止访问外部解释器。

## 可复现准备与分组测试

以下在仓库根目录执行，仅针对 **macOS arm64**。开发机需要 uv、Python 3.14+（只用于读取 zstd 元数据归档；不会打入 helper）及 Xcode 命令行工具。其他目标必须补充本平台 Python 归档锁和验证，不使用跨平台冻结或 PATH 解释器回退。

### 1. 获取与校验 Python

```bash
mkdir -p tmp/_scratch/spotify-audit
python3 internal/provider/spotify/sidecar/prepare_python.py tmp/_scratch/spotify-audit
```

该准备脚本只下载锁中的 HTTPS 构建资源，先核对归档 SHA-256 再解包，保留元数据/许可证。最初 urllib 请求返回 403 的阻塞已解除：使用构建下载 User-Agent 后取得归档，摘要与发布方 `SHA256SUMS` 一致；没有禁用 TLS 或使用其他未经校验镜像。

### 2. 锁定下载与两次离线安装

```bash
P="$PWD/tmp/_scratch/spotify-audit/python/bin/python3.12"
S=internal/provider/spotify/sidecar
"$P" -I -m pip --isolated download --require-hashes --only-binary=:all: \
  --index-url https://pypi.org/simple -r "$S/requirements.lock" \
  -d tmp/_scratch/spotify-audit/wheelhouse
for suffix in a b; do
  uv --no-config venv --python "$P" "tmp/_scratch/spotify-audit/venv-$suffix"
  uv --no-config pip sync --python "tmp/_scratch/spotify-audit/venv-$suffix/bin/python" \
    --no-index --find-links tmp/_scratch/spotify-audit/wheelhouse \
    --require-hashes --no-build "$S/requirements.lock"
done
uv --no-config pip check --python tmp/_scratch/spotify-audit/venv-a/bin/python
```

实际验证的两次安装各为 14 包，`pip freeze` 相同；比对 site-packages 中 1,945 个文件的 SHA-256 相同（排除路径相关 RECORD、uv_cache.json 和生成字节码）。这证明依赖安装载荷一致，不承诺平台签名后的整个冻结目录逐字节可复现。

### 3. 许可证、单测与冻结

```bash
V="$PWD/tmp/_scratch/spotify-audit/venv-a/bin/python"
S=internal/provider/spotify/sidecar
"$V" -I "$S/collect_licenses.py" \
  --wheelhouse tmp/_scratch/spotify-audit/wheelhouse \
  --python-metadata tmp/_scratch/spotify-audit/python-metadata/python \
  --output build/spotify-helper/licenses
"$V" -I -c 'import sys,unittest; sys.path.insert(0,"internal/provider/spotify/sidecar"); r=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.discover("internal/provider/spotify/sidecar/tests",top_level_dir="internal/provider/spotify/sidecar")); sys.exit(not r.wasSuccessful())'
"$V" -I "$S/freeze.py"
"$V" "$S/verify_frozen.py" build/spotify-helper/dist/spotify-helper
```

许可证收集保留所下载 wheel（包括内嵌依赖）的许可，不虚构当前平台没有安装的包。Windows 条件包的版本/许可已另行核对，发行时须在对应原生环境重新收集。

产物位于 `build/spotify-helper/dist/spotify-helper/`，约 22 MiB；整个目录必须作为单元分发，不能只复制主可执行文件。`manifest.json` 包含文件摘要、平台和版本，后续签名改变文件后必须重新生成摘要。当前仅做 ad-hoc 签名校验，尚无发布签名、公证、Wails bundle 或安装器装配。

## 锁定库源码审计与兼容范围

审计的是 PyPI wheel（SHA-256 `8a83af7f6569864f409e406110988adc58ae6d433b9527b4a25be5dfebcd148c`），来源 [SpotifyScraper](https://github.com/AliAkhtari78/SpotifyScraper)。

- 公开 `SpotifyClient(transport=...)` 覆盖 embed、pathfinder、分页和匿名 token provider；注入时不创建默认 HttpxTransport。库默认传输的 403/429 重试与自动重定向不使用。
- 不调用 `get_playlist()`：它默认 100 项，`_paginate` 会吞掉中途错误并返回部分结果，`_get_entity` 会降级为不带可靠总数的 embed。
- 当前局部适配使用 `_fetch_next_data / _fetch_union` 私有扩展点，逐页读取；每页重验歌单 URI、总数、原始页长、重复页与名称一致性。embed 只取得同歌单匿名会话，不作为成员快照降级。
- `parse_playlist_tracks_page` 的普通 Track 解析复用，原始位置在过滤前核对。仅明确 Episode/LocalTrack 可报告排除；未知类型、缺歌曲身份或无法解释的过滤使整轮失败。
- 库模型强制 name/album 字符串并把缺时长写 0；适配仅在传给库的副本中使用空字符串兼容，交付时按原始缺失恢复 null，不修改上游数据，不伪造歌曲 ID。详情只补同一稳定 track 的缺字段，已有值不覆盖；详情仍缺字段可为 null。
- `_fetch_union` 的 TokenError 可触发默认 bootstrap track；bridge 在 HTTP 401/403/429 或 GraphQL errors 时先终止，不进入该刷新分支，不读取默认样例，不请求登录/Cookie。
- 条目键为 track Ref + 出现序号；重排不换身份。相同副本之间没有可靠原生条目 ID，不能辨认具体删了哪个副本。完全重复的原始页按不完整拒绝，不猜测其真实性。

升级任一锁定组件时须重新审计传输调用、默认回退、过滤/缺失语义、私有扩展点和许可证，再跑同组测试；不能只修改版本号。

## 本轮证据与未完成边界

2026-11-09，macOS 27.2 / arm64：

- 两次离线 hash-locked 安装一致，依赖完整性检查通过。
- 初次前置组 13 项单测通过；随后加入 Go/Python 共用协议 fixture，现为 14 项：205 项三页、非歌曲原始位置、缺字段及同身份补取、重复出现、明确空列表、截断/重复页/变化总数/未知身份/embed 降级拒绝、鉴权限制无重试、错误帧/请求标识。
- 单测拦截 socket connect 与 DNS；生产 helper 另有禁止 socket/外部进程的 audit hook。
- one-folder 冻结及版本握手通过；OS sandbox 禁止网络、构建目录、Nix/uv 外部 Python，PATH 指向不存在目录，HOME 为空，Python 注入变量无效，中文/空格路径正常。自检读取 205 项，产物文件无改变；主 helper 与解释器 ad-hoc 签名校验通过。
- 初次读取 zstd 归档时系统 tar 缺 zstd，改用开发机 Python 3.14 的标准库仅解包构建元数据；helper 本身没有该依赖。

未请求真实 QQ/Spotify 歌单；Go 受控传输、资源定位、进程封装和只读能力装配已实现，并用真实冻结 helper + Go HTTP 替身验证空歌单及 205 项分页/补取往返。同步业务与 App/UI 现已完成离线接入，最新结果见[第 11 组验收记录](../../../../openspec/changes/add-streaming-playlist-sync/implementation-phase11-offline.md)。Windows 进程运行、安装包或原生 UI 验收尚未完成；Windows 仅交叉编译，Linux/其他 macOS 架构未验证，当前结果不代表完整发行支持。
