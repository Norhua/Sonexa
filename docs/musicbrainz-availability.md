# MusicBrainz 数据获取可用性报告

> 调研与实测日期：2026-09-24。范围：公开 MusicBrainz Web Service v2，以及独立的 Cover Art Archive（CAA）。本报告只评价上游，不评价任何现有项目实现。共同测试边界见[报告索引](upstream-availability.md)。

## 1. 结论

**适合作为结构化音乐身份、发行版本、词曲作者和演唱关系的主要来源；不提供歌词正文，数据完整性依赖社区录入。**

- 无账号、无 API key 即可读取大部分元数据。搜索、录音详情、发行详情、作品关系、发行组风格和 CAA 图片在本次测试中均成功。
- 五个来源中，MusicBrainz 对“署名艺术家”和“实际演唱者”的建模最明确。本次找到署名仅为 `supercell`、但录音关系明确记录 `nagi` 主唱的实际案例。
- 不能保证找齐所有歌手、虚拟歌姬和伴唱。部分录音有完整署名，却没有任何 `vocal` 关系。
- 必须先选中正确发行和录音：同名音频、MV、现场、重录和混音不能合并。搜索排名不是版本确认。

## 2. 字段能力

“可取”表示接口存在对应结构，不代表每条记录非空；“实测”表示本次样本确实返回。

| 类别 | 字段 | 获取位置与能力 | 本次情况 / 限制 |
| --- | --- | --- | --- |
| 普通 | 标题 | `recording.title`；具体发行中的 `media[].tracks[].title` | 实测；发行音轨名可以不同于录音标题 |
| 普通 | 署名艺术家 | `artist-credit[]` 的 `name`、`joinphrase`、`artist.id` | 实测；保留顺序、署名名和连接词，不只拼接规范姓名 |
| 普通 | 专辑 | 录音关联 `releases`，再取选中 `release.title` | 实测；同一录音可出现于多个专辑 |
| 丰富 | 封面 | CAA `/release/{mbid}`，必要时查询 `/release-group/{mbid}` | 实测目录和图片；包含 Front、Back、Booklet 等多图 |
| 丰富 | 所有演唱 | 录音的 artist relations：`type=vocal`，并保留 `attributes` | 实测 `nagi` / `lead vocals`；无“名单完整”保证 |
| 丰富 | 虚拟歌姬 | 署名与演唱关系中的 artist；可具有 `type=Character` | 实测初音ミク为 Character；类型不代表每首歌的演唱角色 |
| 丰富 | 年份 | `release.date`、`release-group.first-release-date`、`recording.first-release-date` | 实测三者可不同；日期还可能只有年或年月 |
| 丰富 | 音轨号 | `release.media[].tracks[].number` 与 `position` | 实测；`number` 是展示字符串，可能是 `A1`，不要强转整数 |
| 丰富 | 碟号 | `release.media[].position` | 实测 CD 与 DVD 为第 1、2 介质；保留介质格式 |
| 丰富 | 风格 | 对 recording / release / release-group 请求 `inc=genres` | 实测发行与录音为空、发行组非空；不可把艺人风格冒充歌曲风格 |
| 丰富 | 专辑艺术家 | `release.artist-credit[]` | 实测；不同于单曲署名 |
| 丰富 | 作曲 | recording → `performance` → work → artist relation `composer` | 实测 ryo |
| 丰富 | 作词 | 同一 work 的 artist relation `lyricist` | 实测 ryo；不要与翻译作者、改编者混淆 |
| 丰富 | 歌词 | work 的 `url-rels` 可以提供 `type=lyrics` 外链 | 实测歌词外链；**不返回正文或 LRC** |
| 其他 | 单曲 / EP / 专辑 | `release-group.primary-type`，另有 `secondary-types` | 接口支持 Album / Single / EP 等；实测 Album |
| 其他 | 国家 / 地区 | `release.country`、`release-events[].area` | 实测 JP；这是发行地区，不是艺人国籍或歌曲语言 |
| 其他 | 厂牌名称 | `release.label-info[].label` | 实测 GT music |
| 其他 | 厂牌号 / 目录号 | `release.label-info[].catalog-number` | 实测 MHCL-1493、MHCL-1494、MHCL-1495 |
| 其他 | ISRC | recording 的 `isrcs[]`，请求 `inc=isrcs` | 实测音频 `JPP400900066`；对应 MV 为空 |
| 其他 | UPC / EAN / JAN | `release.barcode` | 实测 `4582290350301`；这是 13 位条码，不能一概命名为 UPC-A |
| 其他 | 其他身份 / 信息 | MBID、ISWC、时长、语言、发行状态、包装、介质、外部链接、编曲 / 制作 / 乐器演奏关系 | 多项实测；注意字段实体层级 |

`label-code` 是另一种厂牌标识，并非专辑目录号。没有关系只表示未录入，不能解释为“没有伴唱”或“没有作词”。

## 3. 获取方法

### 3.1 搜索 → 发行 → 录音 → 作品

基础地址：`https://musicbrainz.org/ws/2/`。使用 `fmt=json`，并发送带版本及有效维护者联系方式的 `User-Agent`。

```bash
# 将示例 User-Agent 中的联系方式替换为真实维护者地址。
curl --get 'https://musicbrainz.org/ws/2/recording/' \
  -A 'MetadataResearch/1.0 (https://your-domain.example/contact)' \
  --data-urlencode 'query=recording:"ワールドイズマイン" AND artist:supercell' \
  --data 'fmt=json&limit=5'

sleep 1.2  # 与后续请求保持间隔；多个使用者还需共享限流器。

# 已确认的音频录音；不要选成同名 MV。
curl --get \
  'https://musicbrainz.org/ws/2/recording/b5c3432e-7ec5-4d81-92fe-e8e8e8dbd18b' \
  -A 'MetadataResearch/1.0 (https://your-domain.example/contact)' \
  --data 'fmt=json' \
  --data-urlencode 'inc=artist-credits+isrcs+genres+artist-rels+work-rels+work-level-rels+url-rels'
```

完整发行清单示例：

```text
GET /ws/2/release/588d5b01-a0ef-3599-b076-c69c8c36f635
    ?fmt=json
    &inc=recordings+artist-credits+release-groups+labels+isrcs+genres+artist-rels+work-rels+recording-level-rels+work-level-rels+url-rels
```

实际构造 URL 时对 `inc` 整体编码，不能让字面 `+` 被表单编码器错误处理。

关系 include 是组合开关：只有 `work-level-rels` 不够，还要有 `work-rels`，以及期望的 `artist-rels` / `url-rels`。读取作品的 `composer`、`lyricist` 时，先筛选录音到作品的 `performance` 关系，不要把任何关联作品都当作被演出的作品。

- 搜索 / browse 使用 `limit`、`offset`，常规最大 limit 为 100。
- lookup 内附带的关联实体有数量上限，官方说明通常为 25；全量关联发行须使用 browse 分页，不能以附带列表替代全集。
- 对 release 的曲目清单，使用 `media[].tracks[]`；不要用搜索结果中匹配到的少数 `media[].track[]` 充当整张专辑。
- `/isrc/{isrc}` 和 `/iswc/{iswc}` 支持标识符查询，结果仍可能不是唯一实体。

### 3.2 演唱名单

建议独立保存以下数据：

1. `creditedArtists`：原样保留 `artist-credit[]`。
2. `vocalPerformers`：录音关系中 `type=vocal` 的艺人、角色属性与署名。
3. `instrumentPerformers`、录音师、制作人等：另存角色，不能混入演唱者。

**不要仅检测 `attributes` 是否含 `lead vocals`。** 本次《君の知らない物語》同时有 `type=recording`、属性为 `lead vocals` 的录音工程关系；实际主唱是 `type=vocal` 的 `nagi`。按属性直接提取会把录音师当作主唱。

### 3.3 封面

```text
GET https://coverartarchive.org/release/588d5b01-a0ef-3599-b076-c69c8c36f635
```

从响应 `images[]` 选择 `front=true` 或相应 `types`，使用实际提供的 `image` / `thumbnails` 地址。图片请求可能重定向至 Internet Archive。目录可用不代表图片一定可用；无图、404、429、5xx 与超时必须分开记录。

## 4. 本次实测

测试无登录、无代理配置；MusicBrainz 请求串行，间隔至少 1.2 秒。耗时是单次客户端观察，不是 SLA。

| 测试 ID | 请求 / 样本 | 结果 |
| --- | --- | --- |
| `mb-search-world` | 搜索《ワールドイズマイン》+ supercell，limit=5 | HTTP 200，约 1.84 秒，总匹配数 16；多个录音版本 |
| `mb-release-supercell` | `588d5b01-…`，supercell 初回限定发行 | HTTP 200，约 2.45 秒；2009-03-04 / JP，CD 12 轨 + DVD 9 轨，目录号与条码均非空 |
| `mb-recording-world-audio` | `b5c3432e-…`，CD 第 7 轨 | HTTP 200，ISRC=`JPP400900066`；关联作品的词曲作者均为 ryo |
| `mb-recording-world` | `be45c857-…`，同名 DVD 录音 | HTTP 200，`video=true`，ISRC 空；说明不能只按曲名匹配 |
| `mb-recording-nagi` | `1c8d2ed4-258f-4435-8db9-fa2f8be24712`，《君の知らない物語》 | HTTP 200，约 2.64 秒；署名 supercell，vocal / lead vocals 为 nagi；ISRC=`JPSR00901782` |
| `mb-group-supercell` | 发行组 `b82f022d-26e3-4d53-81c3-d00916646842` | HTTP 200；j-pop、j-rock、pop、pop rock、rock；首次发行 2008-08-16 |
| `caa-supercell` | 上述限定发行的封面目录 | HTTP 200，约 3.53 秒，56 张图的目录，不代表全部已下载 |
| `caa-cover` | 第一张 Front 的 250 档 | HTTP 200；实际解码尺寸 **250×250** |

额外观察：同一张发行的日期为 2009 年，发行组首次发行是 2008 年；该音频录音的 `first-release-date` 又是 2008-05-31。应保存日期语义，而不是覆盖成一个无来源的“年份”。

## 5. 接入成本与风险

- **限流明确**：默认每来源 IP 平均最多 1 请求/秒；超限可能整批拒绝并返回 503。使用全局队列、缓存、合并请求、退避和 `Retry-After`，不要并行冲击同一服务。
- 冷门 / 新曲、中文别名和角色关系可能缺失；搜索分数只能辅助匹配，需结合发行、时长、介质和录音类型。
- 带多个介质、作品关系的发行详情，以及包含大量小册子图的 CAA 目录，响应可能较大；按需获取，不必搜索时就展开全部关系。
- 核心数据是 CC0；**标签、风格关联、注释、评分等补充数据并非全都 CC0**，官方列为 CC BY-NC-SA 3.0。CAA 图片版权独立，不能套用元数据许可。
- 推荐作为发行身份与细粒度 credits 的基础来源；歌词正文需要另选来源，不能认为拿到歌词 URL 就已拿到歌词。

## 6. 资料

- [官方 API 文档](https://musicbrainz.org/doc/MusicBrainz_API)
- [搜索文档](https://musicbrainz.org/doc/MusicBrainz_API/Search)
- [限流与 User-Agent](https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting)
- [CAA API](https://musicbrainz.org/doc/Cover_Art_Archive/API)
- [数据许可](https://musicbrainz.org/doc/About/Data_License) / [核心与补充数据范围](https://musicbrainz.org/doc/MusicBrainz_Database)
- [可核对的实际主唱录音](https://musicbrainz.org/recording/1c8d2ed4-258f-4435-8db9-fa2f8be24712)
