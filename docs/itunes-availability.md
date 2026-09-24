# iTunes 数据获取可用性报告

> 调研与实测日期：2026-09-24。范围：**无需密钥的 iTunes Search API / Lookup API**，不把 Apple Music API 的能力算入本报告。共同测试边界见[报告索引](upstream-availability.md)。

## 1. 结论

**适合获取商店版歌曲的标题、署名、专辑、封面、日期、轨号和粗粒度风格；不适合补齐完整演唱人员、词曲作者、歌词、ISRC 和发行目录号。**

本次日区、台湾区和中国大陆区请求均得到有效响应；专辑清单、单曲 lookup、UPC lookup 与封面下载均成功。无需登录、Cookie 或开发者 token。

最重要的边界：

- `artistName` 是一段商店署名，不是有角色的完整演唱名单。实测 `artistName=ryo (supercell)`，初音ミク只出现在 `trackName` 的 `feat.` 中。
- `collectionType=Album` **不能区分专辑、EP 和单曲**。本次 1 首歌的发行及名称含 `- EP` 的发行都返回 Album。
- `country` 是商店地区，不是作品原产国。
- 已知 UPC 可作为 lookup 输入，但响应不一定给出 UPC，不能据此声称“能从歌曲反查 UPC”。

## 2. 字段能力

| 类别 | 字段 | 获取位置与能力 | 本次情况 / 限制 |
| --- | --- | --- | --- |
| 普通 | 标题 | song 对象 `trackName` | 实测；可能包含 feat.、Live、Cover、版本后缀 |
| 普通 | 署名艺术家 | song 对象 `artistName`，辅以 `artistId` | 实测；字符串不等于结构化多艺人 / 角色列表 |
| 普通 | 专辑 | `collectionName`、`collectionId` | 实测；名称可能随地区、查询方式本地化 |
| 丰富 | 封面 | `artworkUrl30/60/100`；可尝试图片 CDN 尺寸变体 | 实测下载 1200×1200；大尺寸构造不是 API 的保证 |
| 丰富 | 所有演唱 / 虚拟歌姬 / 未署名演唱 | 无专门演唱角色字段；姓名可能在标题或署名中 | 只能提取候选；无法保证全部，更不能凭艺人简介补齐 |
| 丰富 | 年份 | song / collection 的 `releaseDate` | 实测；商店日期可能不同于原始首发日期，单曲与集合时间也可能不同 |
| 丰富 | 音轨号 | `trackNumber`，总数 `trackCount` | 实测；必须绑定具体 collection |
| 丰富 | 碟号 | `discNumber`、`discCount` | 实测；不是“从搜索结果排序推断” |
| 丰富 | 风格 | `primaryGenreName`，通常只有一个主类别 | 实测“ポップ”“国语流行”；名称本地化，粒度较粗 |
| 丰富 | 专辑艺术家 | song 的可选 `collectionArtistName`；或 collection 对象 `artistName` | 实测 Various Artists；song 字段缺失时应查专辑，不把曲目艺人直接当作专辑艺人 |
| 丰富 | 作曲 | 公开 Search / Lookup 无稳定专用字段 | 本次未返回；不能把 `artistName` 当 composer |
| 丰富 | 作词 | 无稳定专用字段 | 本次未返回 |
| 丰富 | 歌词 | 不提供歌词正文 / LRC | `previewUrl` 是试听音频，不是歌词 |
| 其他 | 单曲 / EP / 专辑 | `collectionType` 只给出集合的大类；名称后缀可提供弱提示 | 实测 EP 和 1 首歌发行均为 Album，不能可靠结构化分类 |
| 其他 | 国家 | `country` | 实测 JPN / CHN；只代表请求商店，不是发行原产国 |
| 其他 | 厂牌 | collection `copyright` 可提供版权文字 | 版权主体不等于原发行厂牌；无稳定独立厂牌字段 |
| 其他 | 厂牌号 / 目录号 | 无稳定专用字段 | 封面文件名即使看似包含目录号，也不能当规范数据 |
| 其他 | ISRC | 无公开 Search / Lookup 标准输出字段 | 本次未返回 |
| 其他 | UPC / EAN | 可用 `lookup?upc=...` **查询**；不是保证输出的字段 | UPC 输入实测成功，但响应未返回 `upc` |
| 其他 | 其他 | 时长、显式内容标记、商店链接、试听链接、价格、货币、商店 ID | 实测多项；部分商店 / 条目省略价格字段 |

Apple Music API 是另一个有授权要求的 API 体系，不能把其可能出现的 `isrc`、`composerName` 等字段写成免费 iTunes Search 的字段。本次没有凭据，也没有对 Apple Music API 做授权实测。

## 3. 获取方法

### 3.1 搜索

```bash
curl --get 'https://itunes.apple.com/search' \
  --data-urlencode 'term=supercell ワールドイズマイン' \
  --data 'country=jp&media=music&entity=song&limit=5'
```

- `term` 是宽松搜索，不是严格的标题 + 艺人交集；实测《晴天》搜索也混入《星晴》《稻香》。应用必须自行核对。
- 显式传 `country`，默认 US 不适合所有曲库。身份 / 缓存键建议至少包含地区和 track / collection ID。
- `limit` 官方范围 1–200；`resultCount` 是本次返回数，不是整个曲库的总匹配数。
- `lang` 官方只列出 `en_us`、`ja_jp`，不是任意语言码；未传时也不能假设名称一定一致。
- 筛选 `wrapperType=track`、`kind=song`，不要把专辑或音乐视频当成歌曲。

### 3.2 精确单曲与专辑清单

```bash
# 单曲身份确认
curl --get 'https://itunes.apple.com/lookup' \
  --data 'id=642832873&country=jp'

sleep 3.1

# 专辑 + 专辑内歌曲
curl --get 'https://itunes.apple.com/lookup' \
  --data 'id=642832495&country=jp&entity=song&limit=200'

sleep 3.1

# 已知条码时的检索，不代表反查条码
curl --get 'https://itunes.apple.com/lookup' \
  --data 'upc=720642462928&country=us&entity=song&limit=200'
```

专辑清单响应混有 `wrapperType=collection` 和歌曲对象。按类型及 `collectionId` 分组，不要简单地把第一项之后的全部记录认作同一张专辑；本次 UPC 检索返回 **36 个结果**，包含多个集合与曲目，必须重新确认发行身份。

对于超长合集，检查 collection 的 `trackCount` 与已取到曲目数；公开文档没有能保证任意专辑完整遍历的通用分页流程，不应虚构 `offset` 分页支持。也不能直接声称固定 limit 下已取齐。

### 3.3 封面

以响应给出的 `artworkUrl100` 为基础，本次把末尾 `100x100bb.jpg` 改为 `1200x1200bb.jpg`，得到实际 1200×1200 图片。

这是图片 CDN 的实测尺寸请求，不是“把数字改成 3000 就必然有 3000 原图”。处理时应保留原始 URL、设置大小上限、验证 MIME 与真实像素、允许回退。URL 中的图片文件名不是可信条码或目录号来源。

## 4. 本次实测

测试无登录、无代理配置；同一 iTunes API 连续请求间隔至少约 3.1 秒。图片 CDN 单独测试。

| 测试 ID | 样本 / 请求 | 结果 |
| --- | --- | --- |
| `itunes-search-world` | 日区搜索 supercell + ワールドイズマイン | HTTP 200，约 0.40 秒，5 个结果；包含现场与翻唱版本 |
| `itunes-song-lookup` | track `642832873` | HTTP 200，署名 `ryo (supercell)`；标题含 `feat. 初音ミク`；disc=1 / track=2；collectionArtistName=Various Artists |
| `itunes-album-world` | collection `642832495`，entity=song | HTTP 200，约 0.50 秒；1 个 collection + 24 首歌，清单数量与 trackCount 一致 |
| `itunes-search-qingtian` | 台湾区《晴天》/ 周杰倫 | HTTP 200；原版 track `535824738`，也有现场及其他歌曲 |
| `itunes-qingtian-cn` | 同一 track ID，中国大陆区 lookup | HTTP 200，1 条；署名变为“周杰伦”，country=CHN、风格“国语流行” |
| `itunes-single-lookup` | collection `1755141035`，仅 1 首歌 | HTTP 200；`collectionType=Album`，标题没有 Single 后缀 |
| `itunes-ep-lookup` | collection `1869838405`，Ex-Otogibanashi - EP | HTTP 200；6 首歌，`collectionType=Album` |
| `itunes-upc-lookup` | 官方示例 UPC `720642462928` | HTTP 200，约 0.69 秒，36 个混合结果；首个集合是 Weezer (2024 Remaster)，所有返回项都未含 `upc` 字段 |
| `itunes-cover` | collection `642832495` 的 1200 尺寸变体 | HTTP 200；实际 **1200×1200** |

### 观察到的名称与日期差异

- 同一个 collection `642832495`，song lookup 中是日文专辑名，而 collection lookup 中是英文专辑名。两个请求都传 `country=jp`，不要仅用专辑名称作关联键。
- 该专辑 `copyright` 为 2012 年，`releaseDate` 为 2013 年；版权年份、商店发行日期、现场演出年份不是同一个概念。
- `releaseDate` 在 song 与 collection 对象可能有不同时间部分；适合作为发行日期来源，不应把小时差当作两个不同版本。

## 5. 接入成本与风险

- 官方归档文档给出约 **20 次/分钟**，并注明可能变化。搜索和 lookup 应共享保守配额，使用缓存、退避；本次成功不代表长期 SLA。
- 地区目录与语言不同，商品可下架；HTTP 200 + `resultCount=0` 是有效空结果，不等于网络故障。
- 更适合作为“快速补齐普通字段、封面与商店发行清单”的来源，而不是全面 credits 数据库。
- 不要把 `- Single` / `- EP` 后缀、价格、轨数规则当作正式类型字段。可留为推断，但应标明置信度。
- 官方文档对专辑图、试听等 **promotional content** 有推广商店内容等用途条件；“无需 key”不等于图片可任意再分发。接入前应审查现行 Apple 条款。本报告不自行推导任何固定缓存天数，也不把技术可下载视作授权。

## 6. 资料

Apple 公开 Search API 文档为归档文档，因此必须结合当前实测使用：

- [Overview 与素材用途说明](https://developer.apple.com/library/archive/documentation/AudioVideo/Conceptual/iTuneSearchAPI/index.html)
- [搜索参数、limit 与频率](https://developer.apple.com/library/archive/documentation/AudioVideo/Conceptual/iTuneSearchAPI/Searching.html)
- [Lookup 与 UPC / EAN 示例](https://developer.apple.com/library/archive/documentation/AudioVideo/Conceptual/iTuneSearchAPI/LookupExamples.html)
- [返回字段说明](https://developer.apple.com/library/archive/documentation/AudioVideo/Conceptual/iTuneSearchAPI/UnderstandingSearchResults.html)
