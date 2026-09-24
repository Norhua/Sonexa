# 网易云音乐（163music）数据获取可用性报告

> 调研与实测日期：2026-09-24。所有网易云元数据请求均直接发送至 `music.163.com/weapi/…`，按标准 WEAPI 双层 AES + RSA 加密；没有用第三方代理 API、明文 `/api` 或 EAPI 冒充 WEAPI 测试。共同边界见[报告索引](upstream-availability.md)。

## 1. 结论

**匿名 WEAPI 实测可获得基础元数据、专辑清单、封面、LRC、翻译及部分逐字歌词；不适合作为完整演唱人员、标准发行标识的唯一来源。**

- 搜索、批量歌曲详情、专辑详情、旧 / 新歌词端点、播放页百科块均返回 HTTP 200、业务 `code=200`。
- 两首歌的 `/weapi/rep/ugc/song/get` 都返回 HTTP 200、业务 `code=301`、`message=系统错误`。本次不可用；**仅凭此响应无法断定原因一定是“未登录”**。
- 《君の知らない物語》的署名只包含 supercell，详情和歌词未提供实际主唱 nagi；不能把 `ar[]` 当作所有演唱人员。
- 搜索《晴天 周杰伦》返回的前 5 条是翻唱或非原版结果，但已知歌曲 ID `186016` 仍能取到原版详情和歌词。搜索无原版、详情存在、能播放是三个独立问题。

## 2. 字段能力

| 类别 | 字段 | 获取位置与能力 | 本次情况 / 限制 |
| --- | --- | --- | --- |
| 普通 | 标题 | 搜索 / 详情 `songs[].name` | 实测；可有别名 `alia`、译名 `tns`，字段随端点变化 |
| 普通 | 署名艺术家 | `songs[].ar[]` 的 `id/name` | 实测初音ミク + ryo (supercell)，或单独 supercell |
| 普通 | 专辑 | `songs[].al.id/name`，再查专辑详情 | 实测 |
| 丰富 | 封面 | `al.picUrl` / `album.picUrl` | 实测图片；部分搜索 URL 为 HTTP，可验证 HTTPS 后使用 |
| 丰富 | 所有演唱 / 未署名演唱 | 基础详情无完整角色化名单；歌词中的演唱 / 和声 credits 可能补充 | 部分文本能力；nagi 案例未取到；不能保证全部 |
| 丰富 | 虚拟歌姬 | 可能出现在 `ar[]`、歌名或歌词信息中 | 实测初音ミク；没有可据以保证完整的统一虚拟歌姬角色字段 |
| 丰富 | 年份 | 歌曲 `publishTime`，专辑 `album.publishTime`，毫秒时间戳 | 实测；两层日期不同，0 应视为未知 |
| 丰富 | 音轨号 | 歌曲 `no`，结合专辑清单校验 | 实测 4 / 3；本次未验证大型多碟编号规律 |
| 丰富 | 碟号 | 歌曲 `cd`，为字符串 | 实测 `"1"`、搜索中也有 `"01"`；不要无条件转整数或补默认 1 |
| 丰富 | 风格 | 播放页百科块的曲风；部分专辑 `tags` | 实测“二次元-歌声合成”；专辑 tags 为空，不是基础详情必有字段 |
| 丰富 | 专辑艺术家 | `album.artists[]` 与 `album.artist` | 实测；保留原始数组及主艺人，不从全专辑歌曲艺人并集推导 |
| 丰富 | 作曲 | 本次从 `lrc.lyric` 头部“作曲”行取得 | **文本解析**，不是歌曲详情中的结构化 composer 字段 |
| 丰富 | 作词 | 本次从 `lrc.lyric` 头部“作词”行取得 | **文本解析**；`lyricUser` 是歌词贡献者，不是作词者 |
| 丰富 | 歌词 | `lrc.lyric`；`tlyric.lyric` 翻译；新版还有 `romalrc`、`yrc` 等 | 实测原文 / 翻译 / 罗马音 / 逐字内容；不同歌曲可缺失 |
| 其他 | 单曲 / EP / 专辑 | `album.type`、`album.subType` | 实测 EP / 专辑 / 精选集，subType=录音室版；不要用歌曲 `single` 代替 |
| 其他 | 国家 | 未找到本次可用响应中稳定、明确的发行国家字段 | 语言和艺人地区不能替代发行国家 |
| 其他 | 厂牌名称 | `album.company` | 实测“索尼音乐”等，也有空字符串；可能是当前发行 / 版权公司 |
| 其他 | 厂牌号 / 目录号 | 本次可用端点未提供可靠规范字段 | `companyId`、`copyrightId`、`cp` 不是目录号 |
| 其他 | ISRC | 本次可用端点未见可靠字段 | 不能把 `rt`、平台歌曲 ID、版权 ID 当 ISRC |
| 其他 | UPC / EAN / JAN | 本次可用端点未见可靠字段 | 不支持可靠反查的结论仅限本文测试范围 |
| 其他 | 其他 | 时长 `dt`、别名、MV ID、权限 `privileges`、百科语言 / BPM / 关联作品 | 多项实测；权限字段不代表成功下载音频，本次不测试音频 |

不要把 `pop` 当作流行音乐风格，也不要把 `cp` 当作国家。对未知内部字段保留原值而不猜语义。搜索歌曲数组位于 `result.songs`，歌曲详情与专辑清单的歌曲数组位于顶层 `songs`；上表的 `songs[]` 是歌曲对象的简写，不表示所有端点路径相同。

## 3. WEAPI 加密与请求方法

### 3.1 加密规则

1. 把参数序列化为 UTF-8 JSON。
2. 第一层：AES-128-CBC、PKCS#7，key=`0CoJUm6Qyw8W8jud`，IV=`0102030405060708`，输出 Base64 **字符串**。
3. 生成 16 字节 second key。本次从 ASCII 字母数字中随机选择 16 个字符，确保字符数就是字节数。
4. 第二层：用 second key 和同一 IV，对第一层 **Base64 字符串的 UTF-8 字节** 再做 AES-CBC + PKCS#7，再 Base64，得到 `params`。
5. RSA：second key 的字节逆序，按大端整数解释，使用 `e=0x010001` 和下方 modulus 做 **无填充 RSA 模幂**；输出左补 0 的 **256 位十六进制字符**，得到 `encSecKey`。不是 OAEP / PKCS#1 v1.5，也不是 Base64。
6. 正文为 URL 编码表单 `params=...&encSecKey=...`。必须编码 Base64 中的 `+`、`/`、`=`，不能直接拼接未转义字符串。

### 3.2 可复现最小脚本

保存为 `weapi_probe.py`，需要 Python 3、curl 和 OpenSSL。OpenSSL `enc` 默认执行 PKCS#7 填充；此脚本不使用 `-nopad`。示例不读取本机 Cookie，不请求登录。

```python
import base64
import json
import secrets
import subprocess
import sys
from urllib.parse import urlencode

MODULUS = (
    "00e0b509f6259df8642dbc35662901477df22677ec152b5ff68ace615bb7b725152b3"
    "ab17a876aea8a5aa76d2e417629ec4ee341f56135fccf695280104e0312ecbda92557c93870114"
    "af6c9d05c4f7f0c3685b7a46bee255932575cce10b424d813cfe4875d3e82047b97ddef52741d"
    "546b8e289dc6935b3ece0462db0a22b8e7"
)

def aes_base64(text, key):
    encrypted = subprocess.run(
        ["openssl", "enc", "-aes-128-cbc", "-e",
         "-K", key.encode().hex(),
         "-iv", b"0102030405060708".hex()],
        input=text.encode("utf-8"), capture_output=True, check=True,
    ).stdout
    return base64.b64encode(encrypted).decode("ascii")

def form(payload):
    alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    second_key = "".join(secrets.choice(alphabet) for _ in range(16))
    text = json.dumps(payload, ensure_ascii=False, separators=(",", ":"))
    first = aes_base64(text, "0CoJUm6Qyw8W8jud")
    encrypted_key = pow(
        int.from_bytes(second_key[::-1].encode("ascii"), "big"),
        0x010001, int(MODULUS, 16),
    )
    return urlencode({
        "params": aes_base64(first, second_key),
        "encSecKey": f"{encrypted_key:0256x}",
    })

if __name__ == "__main__":
    # 只接受本站 WEAPI 路径，避免误把加密表单发往其他站点。
    path = sys.argv[1]
    if not path.startswith("/weapi/") or "?" in path or "#" in path:
        raise SystemExit("第一个参数必须是 /weapi/... 路径")
    payload = json.loads(sys.argv[2])
    result = subprocess.run(
        ["curl", "--silent", "--show-error", "--max-time", "30",
         "-A", "MetadataResearch/1.0",
         "-H", "Referer: https://music.163.com/",
         "-H", "Content-Type: application/x-www-form-urlencoded",
         "--data-binary", "@-", "https://music.163.com" + path],
        input=form(payload).encode("ascii"), check=True, capture_output=True,
    )
    sys.stdout.buffer.write(result.stdout)
```

```bash
python3 weapi_probe.py /weapi/cloudsearch/pc \
  '{"s":"ワールドイズマイン","type":"1","limit":"5","offset":"0","csrf_token":""}'

sleep 1.2

python3 weapi_probe.py /weapi/v3/song/detail \
  '{"c":"[{\"id\":22677570},{\"id\":186016}]","csrf_token":""}'
```

`c` 是 **JSON 数组的字符串**，不是直接传数组。本次还验证了双层 AES 本地解密回环与 RSA 输出长度，并以真实接口业务成功验证了请求格式。

### 3.3 端点与明文参数

以下参数均先经过上述加密，不能直接作为 POST JSON 发送：

| 用途 | POST 路径 | 明文参数示例 |
| --- | --- | --- |
| 搜索歌曲 | `/weapi/cloudsearch/pc` | `{"s":"晴天 周杰伦","type":"1","limit":"5","offset":"0","csrf_token":""}` |
| 批量歌曲详情 | `/weapi/v3/song/detail` | `{"c":"[{\"id\":22677570},{\"id\":186016}]","csrf_token":""}` |
| 专辑与曲目清单 | `/weapi/v1/album/2080195` | `{"csrf_token":""}` |
| LRC / 翻译 | `/weapi/song/lyric` | `{"id":22677570,"lv":-1,"kv":-1,"tv":-1,"csrf_token":""}` |
| 新版歌词 | `/weapi/song/lyric/v1` | `{"id":22677570,"cp":false,"lv":0,"tv":0,"rv":0,"kv":0,"yv":0,"ytv":0,"yrv":0,"csrf_token":""}` |
| 播放页百科 | `/weapi/song/play/about/block/page` | `{"songId":22677570,"csrf_token":""}` |
| UGC 简要百科（本次失败） | `/weapi/rep/ugc/song/get` | `{"songId":22677570,"csrf_token":""}` |

社区封装源码常写 `/api/...`，再由请求层切换协议。本报告测试的是上表列出的 **实际 `/weapi/...` URL**。

搜索按 `offset/limit` 获取更多结果；详情可批量取 ID，但应控制批量大小。专辑检查 `album.size` 与 `songs` 数量，本次 17 轨、11 轨、6 轨样本均完整，不据此承诺所有超长专辑均一次取齐。

### 3.4 风格、歌词和 credits

播放页百科是 UI 块结构，而非稳定的扁平 schema。本次可用提取路径：

```text
data.blocks[code == "SONG_PLAY_ABOUT_SONG_BASIC"]
  .creatives[creativeType == "songTag"]
  .resources[].uiElement.mainTitle.title
```

本次为“二次元-歌声合成”。`songBizTag` 返回“活力 / 狂欢 / 欢快”，是推荐 / 情绪标签，不应全部写入 genre。`language`、`bpm` creative 的值在 `uiElement.textLinks[].text`。不要硬编码数组下标。

歌词处理要求：

- LRC、翻译、罗马音和逐字歌词分别保存；YRC 不是普通 LRC。
- `lyricUser` / `transUser` 是贡献者，不是词作者 / 演唱者。
- 词曲、和声、制作人等可在歌词头部出现，但应标记为 `lyric-credit` 文本来源；不要把“和声编写”误判为实际和声演唱。
- 空字符串、字段不存在、纯音乐提示、业务失败必须区分。本次没有测试所有无歌词 / 纯音乐分支。

### 3.5 封面

直接使用详情返回的 `al.picUrl`，本次验证的尺寸参数为 `param=500y500`：

```text
https://p1.music.126.net/BqFzD0ev7bMOxkSQqjtKmg==/109951169042217958.jpg?param=500y500
```

图片请求不使用 WEAPI 加密。保留原始 URL，避免重复追加查询参数，并核对实际图片尺寸；本次结果为 500×500，不据此保证所有图片都有任意大尺寸。

## 4. 本次实测

全程无登录 Cookie、无代理配置，同一 WEAPI 请求间隔至少 1.2 秒。仅获取元数据、歌词与封面，不尝试播放或下载受限音频。

| 测试 ID | 样本 | 实际结果 |
| --- | --- | --- |
| `ne-search-world` | 搜索《ワールドイズマイン》 | HTTP 200 / code 200，约 0.37 秒；首条 `22677570`，初音ミク / ryo (supercell) |
| `ne-search-qingtian` | 《晴天 周杰伦》，前 5 条 | HTTP 200 / code 200，但没有原版；包括翻唱、用户条目和 ID=0 的艺人 |
| `ne-detail` | `22677570` + `186016` | HTTP 200 / code 200，约 0.09 秒；均有标题、艺人、专辑、封面、cd、no、publishTime |
| `ne-album` | `2080195` 初音ミクベスト ～memories～ | HTTP 200 / code 200；17/17 曲，type=精选集，company 为空 |
| `ne-album-qingtian` | `18905` 叶惠美 | HTTP 200 / code 200；11/11 曲，type=专辑，company 为空 |
| `ne-album-single` | `81917` 君の知らない物語 | HTTP 200 / code 200；6 曲，type=EP，company=索尼音乐 |
| `ne-album-ep` | `358640241` Ex-Otogibanashi | HTTP 200 / code 200；6 曲，type=EP，company 非空 |
| `ne-lyric-world` / `ne-lyric-qingtian` | 两首歌曲旧歌词接口 | code 200；均有词曲署名文本；前者有翻译歌词 |
| `ne-lyric-new` | `22677570` 新歌词接口 | code 200；`lrc`、`tlyric`、`romalrc`、`yrc` 等非空；YRC 字符数 9560 |
| `ne-wiki-world` | 同曲播放页百科 | code 200；曲风、语言、BPM 等存在，未见独立词曲人员表 |
| `ne-ugc-world` / `ne-ugc-qingtian` | 两曲 UGC 简要百科 | **HTTP 200 / code 301 / 系统错误**；不是成功空结果 |
| `ne-detail-nagi` / `ne-lyric-nagi` | `825646` 君の知らない物語 | code 200；署名只有 supercell，歌词有 ryo 词曲，未取得 nagi 主唱身份 |
| `ne-cover` | `22677570` 封面加 `param=500y500` | HTTP 200，实际 **500×500** |

### 日期陷阱

`22677570` 歌曲 `publishTime=1212163200000`，而其专辑 `2080195` 为 `1251216000007`。后者按 UTC 是 2009-08-25 16:00:00.007，按 UTC+8 是 2009-08-26。不能把时间戳无条件当 UTC 日历日期，也不能将歌曲首发年份覆写为精选集年份。应明确转换时区并保留原值。

## 5. 接入成本与风险

- 这些是站点内部接口的社区研究用法，不是有稳定契约和 SLA 的公开开发者 API。WEAPI 加密只是协议格式，不是长期授权或稳定性保证。
- 匿名本次可用不等于所有地区、版权状态、歌曲都可用。没有测试登录后 UGC、验证码、会员和地域限制；遇到限制应停止并提示，不能循环重试规避。
- 官方公开限流额度未确认。未来接入建议保守串行、缓存、可控重试和熔断；每秒一次只是工程建议，不是官方承诺。
- 风格 UI 块和歌词头部解析比普通详情更易变化。保存原字段、提取路径、时间和来源，不能将推断升级为权威结构化数据。
- 平台 ID、图片大整数可能超过 JavaScript 安全整数范围；对不参与算术的标识用字符串保存。
- 歌词、封面及元数据的使用授权需单独评估；开源封装的许可不授权再分发音乐平台内容。

## 6. 资料

以下为第三方研究实现，不是网易云官方 API 契约；本报告已对所列 WEAPI URL 单独实测：

- [加密实现参考](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/master/util/crypto.js)
- [歌曲详情](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/master/module/song_detail.js)
- [专辑详情](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/master/module/album.js)
- [旧歌词](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/master/module/lyric.js) / [新歌词](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/master/module/lyric_new.js)
- [播放页百科](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/master/module/song_wiki_summary.js)
- [UGC 简要百科](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/master/module/ugc_song_get.js)
