# QQ 音乐数据获取可用性报告

> 调研与实测日期：2026-09-24。范围：`musicu.fcg` 的 POST JSON 接口、公开歌词接口及封面 CDN。未使用登录 Cookie，也未生成私有请求签名。共同测试边界见[报告索引](upstream-availability.md)。

## 1. 结论

**本次匿名请求可用，且不止能获取普通元数据：指定的歌曲详情方法可以返回词、曲、编曲、合声、演奏人员、厂牌、风格和歌词。适合作为中文及日文流行曲的补充来源，但没有完整人员名单保证。**

- 主要搜索入口应使用 **POST `https://u.y.qq.com/cgi-bin/musicu.fcg`**。本次搜索三组中文 / 日文关键词均成功。
- 旧 `c.y.qq.com/soso/fcgi-bin/client_search_cp` 返回 HTTP 500，不能据此判定 QQ 音乐整体不可用。
- **`get_song_detail` 与 `get_song_detail_yqq` 不等价**。相同歌曲前者给出丰富 credits 和歌词，后者的 `info` 只有风格、语言、日期等。
- 用户提供的歌词线索已原样验证：`songmid` + `g_tk=5381` + `jsonpCallback` + `format=jsonp` 成功，正文为 JSONP 包装，歌词默认 Base64。
- 《君の知らない物語》署名与详情“演唱者”仅返回 supercell，没有主唱 nagi；“演唱者”栏不一定能补出署名之外的实际演唱者。

## 2. 字段能力

下表详情路径均以 `req.data` 为基准，其中 `req` 是请求中自定义的请求块名称。

| 类别 | 字段 | 获取位置与能力 | 本次情况 / 限制 |
| --- | --- | --- | --- |
| 普通 | 标题 | `track_info.name/title/subtitle`；搜索结果对应字段 | 实测；`title` 常附译名 / Live 等，不要丢失版本信息 |
| 普通 | 署名艺术家 | `track_info.singer[]`，保留 ID、MID、name/title | 实测 supercell + 初音未来，或周杰伦 |
| 普通 | 专辑 | `track_info.album.id/mid/name/title` | 实测；搜索有 album ID=0 / MID 为空的结果，不能作为有效专辑 |
| 丰富 | 封面 | 用有效 album MID 构造 QQ 图片 CDN URL | 实测 500×500；尺寸变体不是每张专辑都有 |
| 丰富 | 所有演唱 | `info[]` 的“演唱者”“合声”等角色条目 | 有条件可取；本次周杰伦合声存在，nagi 缺失；不能保证所有人员 |
| 丰富 | 虚拟歌姬 | `singer[]` / `info[]` 中可能明确列名 | 实测初音未来；不能把 singer 数组中所有人都认定为真正发声者 |
| 丰富 | 年份 | `track_info.time_public`、专辑 `basicInfo.publishDate` | 实测；不是录音时间或原始发行历史的保证 |
| 丰富 | 音轨号 | `track_info.index_album`，配合专辑歌曲列表 | 实测；多碟时可能是**全专辑序号**，不是碟内序号 |
| 丰富 | 碟号 | `index_cd` | 实测单碟为 0，多碟为 0 / 1；本批样本是零基编号 |
| 丰富 | 风格 | `info[]` 中“歌曲流派”的 `content[].value`；专辑 `genreNew` 等 | 实测 Pop / Animation；数字 `genre` 不应凭空映射 |
| 丰富 | 专辑艺术家 | 专辑详情 `singer.singerList[]` | 实测；supercell 专辑的专辑艺人仅 supercell，单曲有初音未来 |
| 丰富 | 作曲 | `info[]` 标题“作曲”的 `content[]` | 实测 ryo / 周杰伦；该条 `type` 可能是 `JUMP_TO_SINGER`，不一定叫 composer |
| 丰富 | 作词 | `info[]` 标题“作词”，本次 `type=lyricist` | 实测；艺人 ID 有时为 0，不能据此丢掉有效姓名 |
| 丰富 | 歌词 | `get_song_detail` 的歌词条目，或独立歌词接口 | 实测 LRC；独立接口支持 JSONP / JSON，注意 Base64 |
| 其他 | 单曲 / EP / 专辑 | 专辑 `basicInfo.albumType` | 实测“录音室专辑”“EP”；不要把数字 `type=10002` 当作这三类的直接枚举 |
| 其他 | 国家 | 本次响应未见可直接规范化的发行国家 | “国语 / 日语”是语言；不能推导国家 |
| 其他 | 厂牌名称 | 专辑 `company.name` 或歌曲 info 的“唱片公司” | 实测，也有空值；可能为当前版权公司而非最早实体发行厂牌 |
| 其他 | 厂牌号 / 目录号 | `basicInfo.recordNum` 有候选值 | 本次某 EP 返回 `SRCL07081B00Z`，另两专辑为空；**其格式与实体目录号的对应未核实**，宜存原始发行编号，不能承诺标准 catalog number |
| 其他 | ISRC | 本次测试路径未见可靠规范字段 | 不把 song ID / MID、label、recordNum 当作 ISRC |
| 其他 | UPC / EAN / JAN | 本次测试路径未见可靠规范字段 | 无可靠反查证据 |
| 其他 | 其他 | 时长、BPM、语言、MV、编曲、制作人、录音 / 混音工程、乐器演奏、合声等 | 多项实测；需保留角色，避免当作演唱者 |

## 3. 获取方法

### 3.1 主要搜索接口

```bash
curl 'https://u.y.qq.com/cgi-bin/musicu.fcg' \
  -A 'MetadataResearch/1.0' \
  -H 'Referer: https://y.qq.com/' \
  -H 'Content-Type: application/json' \
  --data-raw '{
    "comm":{"ct":"19","cv":"1859","uin":"0"},
    "req":{
      "module":"music.search.SearchCgiService",
      "method":"DoSearchForQQMusicDesktop",
      "param":{
        "query":"晴天 周杰伦",
        "search_type":0,
        "num_per_page":5,
        "page_num":1
      }
    }
  }'
```

歌曲列表：`req.data.body.song.list`。同时检查 HTTP 状态、顶层 `code` 与 `req.code`，成功时本次都为 0。`req` 是客户端指定的块名，不是不可变字段；如果改为 `req_0`，返回路径也随之变化。

搜索使用 `page_num/num_per_page`，并自行核对标题、艺术家、专辑和版本。搜索结果中也有无有效专辑、负数序号、用户上传 / DJ 版本；不要把任意首条结果自动匹配为原版。

本次 `comm` 是精确测试配置，不保证不同 `ct/cv`、GET 请求或同名方法在其他平台参数下行为一致。

### 3.2 详情与专辑清单

仍 POST 同一入口，保留 `comm` / `req` 外层，替换 module、method、param：

| 用途 | module | method | param 示例 |
| --- | --- | --- | --- |
| 丰富歌曲详情 | `music.pf_song_detail_svr` | **`get_song_detail`** | `{"song_mid":"003RcjkV1XjQQy","song_id":7410077}` |
| 网页版简化详情 | `music.pf_song_detail_svr` | `get_song_detail_yqq` | `{"song_mid":"003RcjkV1XjQQy"}` |
| 专辑详情 | `music.musichallAlbum.AlbumInfoServer` | `GetAlbumDetail` | `{"albumId":12925366}` |
| 专辑歌曲 | `music.musichallAlbum.AlbumSongList` | `GetAlbumSongList` | `{"albumMid":"000BDJvT2aWQYZ","begin":0,"num":50}` |

本次歌曲详情也验证了只传 `song_mid` 的形式。专辑详情另外实测 `{"albumMid":"000BDJvT2aWQYZ"}` 成功；社区封装有 `albumMId` 大小写变体。未来实现应选定并回归测试一个已确认形式，数字 `albumId` 可避免这一歧义。

`get_song_detail` 的 `info` 为数组：

```text
info[title == "作词"].content[].value
info[title == "作曲"].content[].value
info[title == "演唱者"].content[].value
info[title == "合声"].content[].value
info[title == "歌曲流派"].content[].value
```

本次“作曲”和“演唱者”都可能 `type=JUMP_TO_SINGER`，单按 `type` 会混淆。建议保留 `title/type/selected/content`，按角色标题与结构组合判断；`get_song_detail_yqq` 的 `info` 则为对象，不能共用一个刚性 schema。

专辑列表在 `req.data.songList[].songInfo`，总数 `totalNum`；使用 `begin/num` 继续取下一页，并根据已取数量判断完成。本次单碟 12/12、多碟 25/25 都取全。

### 3.3 用户提供的歌词 JSONP 方式：已验证

```bash
curl --get 'https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg' \
  -H 'Referer: https://y.qq.com/' \
  --data 'songmid=003RcjkV1XjQQy&g_tk=5381' \
  --data 'jsonpCallback=sonexaLyricCallback&format=jsonp'
```

实际返回：

```text
sonexaLyricCallback({"retcode":0,"code":0,"subcode":0,"lyric":"<Base64>","trans":"..."})
```

- 先严格剥离**自己指定的** callback 包装，再用 JSON parser 解析；不要使用 `eval` 执行上游文本。
- 本次默认 `lyric` 是 Base64，解码为 UTF-8 LRC，共 1804 字符，带 `[mm:ss.xx]` 时间标签。
- 检查 `retcode/code/subcode` 及非空歌词，HTTP 200 本身不证明歌词有效。
- `g_tk=5381` 是本次匿名请求可用参数，不是任意登录账号都适用的认证 token。

另两种形式也已验证：

```text
# JSON，默认歌词 Base64
?songmid=003RcjkV1XjQQy&g_tk=5381&format=json

# JSON，歌词明文字符串
?songmid=003RcjkV1XjQQy&format=json&nobase64=1
```

本次没有系统验证逐字 / 卡拉 OK 歌词接口，因此不将其列为已验证能力。翻译字段的存在也不保证每首歌都有翻译。

### 3.4 封面

```text
https://y.qq.com/music/photo_new/T002R500x500M000000BDJvT2aWQYZ.jpg
```

其中 `000BDJvT2aWQYZ` 为 album MID。实测下载到 500×500 图片。其他尺寸可作为候选，但必须验证响应及像素，不能把占位图、空 MID 或不存在的大图当作有效封面。

## 4. 本次实测

无登录 Cookie、无代理配置。`musicu.fcg` 请求串行、间隔至少 1.2 秒；耗时是单次观察。

| 测试 ID | 请求 / 样本 | 结果 |
| --- | --- | --- |
| `qq-search-world` | ワールドイズマイン | HTTP 200 / 两层 code 0，约 0.35 秒；找到 supercell 原专辑版，也有混音等版本 |
| `qq-search-qingtian` | 晴天 周杰伦 | HTTP 200 / code 0；原版 `0039MnYb0qxYhV` / `97773`，album `000MkMni19ClKG` |
| `qq-old-search` | 旧 client_search_cp，同一关键词 | **HTTP 500**，约 5.10 秒，空正文 |
| `qq-detail-world` | `003RcjkV1XjQQy`，get_song_detail | code 0；署名 supercell + 初音未来，词 / 曲 / 编曲 ryo，Pop，LRC；2009-03-04，第 7 轨 |
| `qq-detail-qingtian` | `0039MnYb0qxYhV` | code 0；周杰伦词曲 / 合声，另有吉他、贝斯、鼓、录音 / 混音信息 |
| `qq-detail-yqq` | 同一个 World is Mine，get_song_detail_yqq | code 0；`info` 只含 genre、lan、pub_time，**未返回前一方法的词曲条目** |
| `qq-album` | supercell / `98264` | code 0；录音室专辑，专辑艺人 supercell；company / recordNum 为空 |
| `qq-album-single` | 君の知らない物語 / `12925366` | code 0；albumType=EP；recordNum=`SRCL07081B00Z`，语义需另核验 |
| `qq-detail-nagi` | `001UVTVs2SpTNY`，君の知らない物語 | code 0；演唱者只列 supercell，没有 nagi；词曲 ryo |
| `qq-tracks-multidisc` | `14323`，周杰伦 2004 无与伦比演唱会 | code 0；25/25 曲，index_cd=0 / 1，返回数组非顺序排列 |
| `qq-lyric-jsonp` | 用户提供的 JSONP 参数组合 | HTTP 200 / retcode、code、subcode 均 0；约 0.29 秒，callback 包装与 Base64 解码均验证成功 |
| `qq-lyric-json-base64` / `qq-lyric-world` | JSON 的 Base64 / 明文两种形式 | HTTP 200 / code 0；都取得歌词 |
| `qq-cover` | 500 尺寸封面 | HTTP 200，实际 **500×500** |

### 多碟与人员语义的实测陷阱

1. 专辑 `14323` 返回的数组开头是 index_album=10，随后是 25、24、23……；**数组次序不是曲序**。
2. 第 1 碟 index_cd=0，index_album 为 1–13；第 2 碟 index_cd=1，index_album 为 14–25。后者是全专辑编号，不是第 2 碟第 14 轨。若要派生碟内序号，须先确认完整清单、按碟分组排序，并标记为派生值；不保证其他专辑沿用同一规律。
3. 《晴天》的“合声编写”和“合声”是两个独立角色，前者不等于演唱。
4. 2003 年《叶惠美》的当前 company 为“杰威尔音乐有限公司”；响应自己的简介说该公司成立于 2007 年。因此该值不能无核验地解释为“2003 年首版发行厂牌”。

## 5. 接入成本与风险

- 这些方法是未公开稳定契约的站点内部服务。匿名、无签名在本次特定方法 / 参数下可用，不代表所有 musicu 方法都不需要认证或签名。
- 官方公开请求额度未确认。建议保守限流、缓存、错误熔断、有限退避；遇到鉴权 / 风控提示，报告不可用，不尝试规避限制。
- 同名方法差异、字段结构差异和平台配置都可能变化，需按端点保留契约测试。
- `recordNum` 不宜直接标准化为 MusicBrainz catalog-number；仅凭它非空不能宣称解决了厂牌号需求。
- 丰富 credits 可显著补充资料，但名单不完整，且可能是当前平台维护的元数据；仍应保存来源、时间、角色与原始值。
- 版权独立于接口可访问性。歌词、封面和商业使用需审查平台条件，社区库许可证不等于数据许可证。

## 6. 资料与可核对入口

- [主要 POST 入口](https://u.y.qq.com/cgi-bin/musicu.fcg)（需要本报告中的请求体；浏览器直接 GET 不等价）
- [独立歌词接口](https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg)
- [QQMusicApi 歌曲模块](https://github.com/luren-dc/QQMusicApi/blob/main/qqmusic_api/modules/song.py)
- [专辑模块](https://github.com/luren-dc/QQMusicApi/blob/main/qqmusic_api/modules/album.py)
- [歌词模块](https://github.com/luren-dc/QQMusicApi/blob/main/qqmusic_api/modules/lyric.py)

社区代码用于定位和交叉核对接口，不属于腾讯官方稳定 API 承诺；`get_song_detail` 的丰富返回以及 JSONP 线索以本文直接请求结果为证。
