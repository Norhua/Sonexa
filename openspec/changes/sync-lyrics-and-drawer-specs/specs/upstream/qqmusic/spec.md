## MODIFIED Requirements

### Requirement: 请求与限流
QQ 音乐 SHALL 以 POST JSON 请求 `u.y.qq.com/cgi-bin/musicu.fcg`，歌词回退以 GET 请求 `c.y.qq.com` 的 `fcg_query_lyric_new.fcg`（`format=json&nobase64=1`），均携带 `Referer: https://y.qq.com/`。无需账号或密钥，但与其他上游一样受统一出网条件约束。搜索、详情、清单分页与歌词共用默认每分钟 30 次的令牌桶，可在设置中调整；交互优先与重试沿用[统一接口](../provider-registry/spec.md)。元数据请求沿用响应缓存，同一地址的不同请求以请求体区分缓存，原始响应不按日期失效，显式刷新绕过缓存；手动歌词搜索与获取为[歌词搜索](../lyrics-search/spec.md)规定的不读写响应缓存例外。

HTTP 200 不代表成功：响应顶层 `code` 与每个请求块的 `code` SHALL 均为 0，否则作为来源错误报告，不把空数据当作无结果。

#### Scenario: 请求块错误码
- **WHEN** 响应顶层 code 为 0，但搜索块 code 非 0
- **THEN** 该次搜索显示 QQ 音乐错误，不显示为无结果，也不产生候选

#### Scenario: 出网禁用
- **WHEN** 未配置 MusicBrainz 联系方式时选择 QQ 音乐搜索
- **THEN** 与其他上游一样不发送请求，显示统一原因和设置入口

### Requirement: 标识与搜索候选
QQ 音乐的曲目标识 SHALL 为歌曲 MID（10–20 位字母数字），发行标识为专辑 MID。元数据搜索使用 `DoSearchForQQMusicDesktop`，检索词为已填的曲名、艺术家、专辑以空格连接，仅曲名级只用曲名；独立歌词的查询条件按[歌词搜索](../lyrics-search/spec.md)处理。候选曲名使用 `title`（保留译名、Live 等版本信息），署名为各歌手名以 ` / ` 连接，时长由秒换算，年份取歌曲的发行时间，缺失时取专辑的。专辑 ID 为 0 或专辑 MID 无效的结果不给发行标识和封面。碟号为 `index_cd` 加 1；`index_album` 可能是全专辑序号，因此只有第 1 碟的候选给出音轨号。

#### Scenario: 第 2 碟候选
- **WHEN** 搜索结果的 `index_cd` 为 1、`index_album` 为 14
- **THEN** 候选碟号为 2，音轨号留空，不把 14 当作碟内序号

#### Scenario: 无效专辑
- **WHEN** 搜索结果的专辑 ID 为 0
- **THEN** 候选可以加入曲库，但没有发行标识和自动封面，补全状态为 absent

### Requirement: 歌词
QQ 元数据详情或补全指定目标歌曲时，系统 SHALL 优先使用歌曲详情中的「歌词」条目；该条目缺失或为空时再请求一次独立歌词接口，并校验其 `retcode`、`code`、`subcode` 均为 0。歌词保存上游原文，通常为带时间标签的 LRC；获取失败不影响其他字段写入。仅获取清单时不请求歌词。存储与展示见[数据存储](../../platform/data-store/spec.md)和[界面与交互](../../platform/app-shell/spec.md)。手动歌词搜索使用下述独立端点与转换流程，不改变本元数据链路。

#### Scenario: 详情缺少歌词
- **WHEN** 歌曲详情没有歌词条目，独立歌词接口又返回错误码
- **THEN** 其他详情字段照常写入，曲目已有的歌词保持不变

## ADDED Requirements

### Requirement: 手动歌词翻译与逐字获取
独立歌词搜索 SHALL 使用 `music.musichallSong.PlayLyricInfo.GetPlayLyricInfo`，一个 `musicu.fcg` 请求批量获取逐字和普通歌词两块，明确 `songMid`、`trans=1`、`roma=1`、`crypt=0` 和 `qrc=1/0`；不为取得歌词额外查询数字歌曲 ID。普通原文和翻译独立解码 Base64 与实体，兼容响应声明的加密内容；QRC 和罗马音仍可能为十六进制密文，解码失败不得伪报上游未提供。原文、翻译、罗马音及逐字状态按[歌词搜索](../lyrics-search/spec.md)分别报告。

#### Scenario: 普通接口没有翻译
- **WHEN** 旧接口返回成功但翻译为空，新端点提供翻译
- **THEN** 使用新端点已取得的翻译，不把旧接口未提供描述为解析失败；新端点同样未提供时明确显示缺失，不声称翻译已获取

#### Scenario: 显式逐字与普通批量请求
- **WHEN** 用户选中 QQ 歌词候选并获取正文
- **THEN** 以歌曲 MID 在同一请求取两种歌词，消费同一共享配额；HTTP 成功后仍分别校验顶层与请求块业务状态，普通块可用时不因逐字块失败丢弃正文

### Requirement: 在线 QRC 转换与回退
在线 QRC SHALL 经十六进制解码、QQ 变体 DES 和有界 zlib 解压，读取 XML 的 `LyricContent` 或逐字正文；按上游绝对毫秒时间转换为增强型 LRC 行及词起止标签，校验行/词时间、顺序与边界，保留空词时间间隔与普通署名行。没有上游逐字时间时不得按字数或行号推造时间。响应、解压及转换大小与取消限制遵循[歌词搜索](../lyrics-search/spec.md)。此能力不扩展到本地 QRC 文件导入或播放器同步高亮。

无 QRC 时 SHALL 使用同批普通 LRC；QRC 解析失败时显示逐字失败并尝试普通块，仍无可用原文时回退旧 `fcg_query_lyric_new.fcg`，兼容其 JSON/JSONP 包装、Base64 与实体编码并检查业务错误。回退保留新接口已取得的翻译/罗马音并显示警告，两条路径都无法取得可用原文时不可保存；可选通道失败不直接丢弃可用原文。不绕过认证、限流或证书校验。

#### Scenario: 逐字内容转换
- **WHEN** 上游 QRC 包含绝对毫秒词时间、空词间隔和署名行
- **THEN** 增强型 LRC 保留实际词起止与间隔、署名正文，校验失败不输出猜测时间；预览和采用保留转换结果

#### Scenario: 逐字失败但普通歌词可用
- **WHEN** QRC 解码或时间校验失败，而同批普通 LRC 可用
- **THEN** 预览普通 LRC 并显示逐字失败，翻译/罗马音独立保留；没有 QRC 与 QRC 解析失败不使用相同状态

#### Scenario: 旧接口回退保留附加通道
- **WHEN** 新端点只取得翻译或罗马音，旧接口取得可用原文
- **THEN** 合并已取得的通道并报告回退；若旧接口同样失败则不给可采用文档，不清除当前歌曲的既有歌词
