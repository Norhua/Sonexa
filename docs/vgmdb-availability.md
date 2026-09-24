# VGMdb 数据获取可用性报告

> 调研与实测日期：2026-09-24。严格区分 **VGMdb 主站**、**第三方 vgmdb.info 解析服务**、**公开历史 HTML 样本的离线解析**与**独立图片 CDN**。共同边界见[报告索引](upstream-availability.md)。

## 1. 结论

**内容上适合游戏 / 动画 / 日系音乐的发行版本、目录号、条码、曲目表、人员名单与扫描图；但本次匿名自动获取主站数据失败，不能列为已经可用的在线元数据 API。**

- `https://vgmdb.net/album/79` 与主站搜索请求均返回 **HTTP 403、Cloudflare challenge 页面**，不是有效专辑 HTML 或“没有结果”。
- `https://vgmdb.info/album/79?format=json` 及服务首页在本机均出现 **TLS 连接失败**；历史 HTTP 入口返回 empty reply。不是“HTTP 200、JSON 不完整”，也不能由此推断服务在全世界永久离线。
- `vgmdb.info` / `hufman/vgmdb` 是第三方抓取解析层，**不是 VGMdb 官方 API**。其 README 明确说明主站没有提供该项目所需的 API，并提到当前抓取受 Cloudflare / 用户会话影响。
- 为评估字段能力，本次另取公开仓库中 album 79 的历史 HTML，使用同仓库固定版本解析器 **实际离线解析成功**。这验证了历史格式可解析，不验证今天的主站页面仍可抓取。
- 历史样本提供的封面 CDN 地址本次仍能下载：250×247。**图片可用不等于搜索和元数据可用。**

## 2. 字段能力：内容模型与当前可达性分开

本节“离线验证”均指历史公开 HTML，不是实时 VGMdb 响应。字段名来自 `hufman/vgmdb` 解析器，不是官方接口契约。

| 类别 | 字段 | 内容模型 / 获取位置 | 本次证据与限制 |
| --- | --- | --- | --- |
| 普通 | 标题 | 专辑 `names`；曲目 `discs[].tracks[].names`，多语言 | 离线解析成功；album 的语言键如 `en/ja`，track 键如 `English/Japanese/Romaji`，不要假设相同 |
| 普通 | 署名艺术家 | 并非每个专辑 / 音轨都有独立规范 artist-credit 字段 | 经常需要原封面、notes 或页面明确说明；不能直接用所有 composers / performers 填署名 |
| 普通 | 专辑 | 页面实体 `/album/{id}`，`name/names` | 离线验证；在线详情本次 403 |
| 丰富 | 封面 | `picture_*`、`covers[].name/thumb/medium/full` | 离线取得 6 个 cover 条目；其中一张 CDN 实测可下载 |
| 丰富 | 所有演唱 / 虚拟歌姬 / 未署名演唱 | `performers`、可能的 `vocals`、notes、credits、小册子扫描 | 历史样本含王菲等 performers，但无 vocals 字段；**performer 包括乐器演奏，不等于歌手**，且是专辑级名单 |
| 丰富 | 年份 | `release_date` | 离线实测 1999-11-20；发行版本需区分再版 |
| 丰富 | 音轨号 | 当前解析器 track 结构主要含 names / track_length；由完整 disc.tracks 顺序派生 | 离线 1 碟 13 轨；没有每轨稳定录音 ID，也没有保证返回原始展示轨号 |
| 丰富 | 碟号 | `discs[]` 顺序和 `name`，如 Disc 1 | 离线验证；属于发行内位置 |
| 丰富 | 风格 | `classification`、`category/categories` | **不等于通用音乐 genre**；Arrangement、Vocal、Original Soundtrack 描述内容类别 |
| 丰富 | 专辑艺术家 | 无普遍一致的专辑署名字段 | 不能把 composer、publisher 或所有 performer 强行归一为 album artist |
| 丰富 | 作曲 | 专辑级 `composers[]`；具体到曲目的分工可能在 notes 中 | 离线实测植松伸夫；整张专辑名单不保证适用于每一轨 |
| 丰富 | 作词 | 专辑级 `lyricists[]`，更细分工可能在 notes / 扫描图中 | 离线实测 3 位作词者；不能全部写入每一首歌 |
| 丰富 | 歌词 | 没有标准全文歌词 / LRC 字段 | 小册子可能含歌词，但扫描图不等于可用歌词 API；本次未做 OCR |
| 其他 | 单曲 / EP / 专辑 | `publish_format` / `media_format` / `classification` 各有不同含义 | 不能可靠映射成三分类型；例如 Commercial, Limited Edition / CD / Arrangement 均不是 Album / EP / Single |
| 其他 | 国家 | 当前通用 album schema 没有稳定 country 字段 | 不以 JPY、日语、出版商地区或条码前缀推断发行国家 |
| 其他 | 厂牌号 / 目录号 | `catalog`，以及再版 `reprints[].catalog` | 离线实测 `SSCX-10037`；这是强项，但实时获取本次不可用 |
| 其他 | 厂牌 / 出版 / 发行公司 | `organizations[]` 的 `role`，另有 publisher / distributor | 离线实测 DigiCube（publisher）与 SME Intermedia Inc.（distributor）；角色不能混用 |
| 其他 | ISRC | 通用 album / track 解析模型未见标准 ISRC 字段 | 本次无数据；notes 即使偶然写有，也不等于系统支持反查 |
| 其他 | UPC / EAN / JAN | 页面 Barcode → 解析器 `barcode` | 离线实测 `4517331100379`；13 位条码不是 UPC-A；旧 schema 未列此键但解析器实际会输出 |
| 其他 | 其他 | 多语言曲名、时长、媒介、版本 / 再版、商品关联、平台、活动、价格、notes、编曲、封面类型 | 离线验证多项，适合版本校验与人工补充 |

**全部演唱者的核心限制**：页面可能很详细，但“整张原声有王菲、钢琴演奏者和合唱成员”并不意味着每一轨都由这些人演唱。只有明确的曲目范围与角色证据，才能下沉到歌曲。历史样本的 track schema 没有逐轨 composer / vocalist 列表。

## 3. 获取方法与实际可行性

### 3.1 主站 HTML

```bash
curl --get 'https://vgmdb.net/album/79' \
  -A 'MetadataResearch/1.0' \
  --max-time 30

curl --get 'https://vgmdb.net/search' \
  --data 'do=results&action=quicksearch' \
  --data-urlencode 'quicksearch=supercell' \
  --max-time 30
```

这两个请求本次都得到 challenge 403。第二个请求只证明该搜索入口被拦截，**没有验证搜索表单能返回正确结果或分页规律**。不得把挑战页面交给专辑解析器再报告“空专辑”。

未来若有获准的数据访问方式，应先取得可读取的页面，再依页面结构解析：

1. album ID / 目录号 / 名称用于定位具体发行，而不是只按歌曲标题搜全站。
2. 解析多语言专辑名、目录号、发行日期、组织角色、完整多碟曲目。
3. 人员名单保持专辑级作用域；notes 中明确的曲目分工另行解析，并保留原文和人工确认能力。
4. 封面目录与图片下载独立处理；不是拿到一个 cover URL 就认为取得全部小册子。

### 3.2 第三方 JSON 服务

第三方项目支持与主站相近的实体路径，以及 `format=json` / 内容协商：

```text
GET https://vgmdb.info/album/79?format=json
GET https://vgmdb.info/search/albums/supercell?format=json
```

- 本次第一个 URL 和服务首页均无法建立 TLS 连接，第二个只是源码中路由模式对应的示例，**未成功实测搜索**。
- 不应将此服务当作可信长期生产依赖或官方有 SLA 的元数据源。
- 自托管 `hufman/vgmdb` 只改变解析层部署位置，**不会消除主站访问限制**。本次没有自托管网络抓取，也没有尝试借助登录会话绕过挑战。
- 项目 README 描述了用户会话配置，但本次没有读取浏览器 Cookie、没有取得用户凭据、没有验证登录后可用性。若后续需要，应先获得使用者明确授权并核对站点规则。

### 3.3 历史样本离线测试：可复现条件

固定仓库版本：

```text
hufman/vgmdb
86894699362d6a7863c30e3bed054be49384cac8
```

本次读取这个版本对应的：

```text
tests/album_ff8.html
vgmdb/parsers/album.py
vgmdb/parsers/utils.py
```

使用隔离 Python 环境：`beautifulsoup4==4.14.3`、`lxml==5.4.0`。仅加载 parser，禁用网络抓取，调用：

```python
# 在可导入上述固定版本 parser 的环境中执行。
from pathlib import Path
from vgmdb.parsers.album import parse_page

result = parse_page(Path("tests/album_ff8.html").read_text(encoding="utf-8"))
assert result["catalog"] == "SSCX-10037"
assert result["barcode"] == "4517331100379"
assert len(result["discs"]) == 1
assert len(result["discs"][0]["tracks"]) == 13
```

实际解析结果：

```json
{
  "name": "FITHOS LUSEC WECOS VINOSEC: FINAL FANTASY VIII [Limited Edition]",
  "catalog": "SSCX-10037",
  "barcode": "4517331100379",
  "release_date": "1999-11-20",
  "publish_format": "Commercial, Limited Edition",
  "media_format": "CD",
  "classification": "Arrangement"
}
```

另有 composer 植松伸夫、8 位 performers、3 位 lyricists、1 碟 13 轨、6 个 cover 条目。`vocals` 未出现；曲目对象只有多语言名称和时长。旧 schema 没列 `barcode`，parser 却成功提取该字段，说明只看 schema 也会漏判能力。

## 4. 在线实测记录

| 测试 ID | 请求 | 结果与可得结论 |
| --- | --- | --- |
| `vgm-net-79` | `https://vgmdb.net/album/79` | **HTTP 403**，约 1.93 秒；正文含 Just a moment，响应头含 `cf-mitigated`；没有取得专辑内容 |
| `vgm-net-search` | 主站搜索 supercell | **HTTP 403**，约 4.52 秒；无法评价搜索结果质量 |
| `vgm-info-79` | 第三方 JSON 专辑入口 | curl exit 35，`SSL_ERROR_SYSCALL`，约 2.33 秒；没有 HTTP 响应 |
| `vgm-info-home` | 第三方 HTTPS 首页 | 同样 TLS 失败，约 2.07 秒 |
| `vgm-info-http` | 第三方历史 HTTP 入口 | curl exit 52，Empty reply from server；没有响应，不作为推荐明文接入方式 |
| `vgm-fixture-79` | GitHub 中公开历史 HTML | HTTP 200；随后离线解析成功，**不是主站实时数据** |
| `vgm-historic-cover` | 历史样本的 medium-media.vgm.io 地址 | HTTP 200，约 2.47 秒，实际 **250×247**；只验证这一张历史图片仍可访问 |

实际图片地址：

```text
https://medium-media.vgm.io/albums/97/79/79-1264618929.png
```

本次没有轮换代理、规避挑战、读取登录 Cookie 或探测内部权限入口。不能把同一图片 CDN 可用当作主站限制已解决。

## 5. 面向后续开发的建议

- **定位为可选的专业发行资料补充源，而非启动必需的在线基础服务。** 先解决获准访问渠道与稳定性，再讨论自动填充。
- 面向 VGM / 动画原声的高价值字段是目录号、版本、条码、曲目多语言名、credits 和扫描图，不是流媒体可播放性。
- 数据模型必须允许“专辑级人员”“仅 notes 提及”“明确曲目级角色”“推断值”分别保存；不要自动把专辑所有参与者复制到每一轨。
- 目录号可优先用于匹配，但要考虑合并号段、初回版、再版、赠品及 unofficial / bootleg 条目；不能把同名所有发行合并。
- 无公开稳定限流契约可依赖。若获准抓取，也应保守请求、缓存、尊重限制；挑战 / 403 应是不可用状态而非频繁重试触发器。
- 社区解析器的开源不意味着 VGMdb 内容、封面扫描、歌词有相同许可；生产和再分发前需单独核对数据使用权。

## 6. 资料

- [VGMdb 专辑 79](https://vgmdb.net/album/79)（本次自动请求 403）
- [第三方项目 README](https://github.com/hufman/vgmdb/tree/86894699362d6a7863c30e3bed054be49384cac8)
- [固定版本 album parser](https://github.com/hufman/vgmdb/blob/86894699362d6a7863c30e3bed054be49384cac8/vgmdb/parsers/album.py)
- [固定版本 album schema](https://github.com/hufman/vgmdb/blob/86894699362d6a7863c30e3bed054be49384cac8/schema/album.json)
- [离线 HTML 样本](https://github.com/hufman/vgmdb/blob/86894699362d6a7863c30e3bed054be49384cac8/tests/album_ff8.html)
- [样本断言测试](https://github.com/hufman/vgmdb/blob/86894699362d6a7863c30e3bed054be49384cac8/tests/album.py)
- [第三方搜索实现](https://github.com/hufman/vgmdb/blob/86894699362d6a7863c30e3bed054be49384cac8/vgmdb/parsers/search.py)
