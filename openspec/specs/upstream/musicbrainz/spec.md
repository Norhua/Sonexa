# upstream/musicbrainz Specification

## Purpose

以 MusicBrainz 作为导入匹配的主要来源，在共享限流下搜索曲目位置、获取精确详情和发行清单。保留结构化署名与实际参与关系，按来源提供封面和缓存行为。

## Requirements

### Requirement: 请求标识与限流
MusicBrainz 请求 SHALL 携带含应用名、版本、联系方式的 User-Agent，未配置联系方式时不请求。默认平均每秒一次，搜索、导入与详情共享配额，可在设置调整。

#### Scenario: 并发请求
- **WHEN** 多个元数据请求同时等待
- **THEN** 按共享令牌桶和交互优先级放行，均携带配置的请求标识

### Requirement: 查询与结果粒度
有曲名的组合查询 SHALL 使用 recording AND (release OR artist OR artist alternates)，仅包含已填条件，艺术家备选最多两个；仅曲名级不带辅助条件。搜索结果按录音、发行、碟、位置展开，使用响应中的真实 track 标识；没有位置时保留合成候选。

#### Scenario: 歌集和艺术家条件
- **WHEN** 曲名和歌集相符，但录音署名与输入不同
- **THEN** 可通过歌集条件返回候选，再由统一匹配规则判断展示

#### Scenario: 搜索响应解析
- **WHEN** 响应使用 media.track 或 media.tracks
- **THEN** 均解析位置及已知曲序、载体等字段；纯数字 number 可回退为位置，A1 等编号不推断为数字位置

### Requirement: 精确详情与署名角色
详情 SHALL 通过一个发行请求获取可用字段、作品关系和清单。指定目标时必须精确命中 track MBID；曲目署名优先于录音、再回退发行署名，原文与成员使用同一来源。作曲作词从作品关系取得，演唱演奏等来自实际关系，不因署名补造角色。

#### Scenario: 非首曲或未命中
- **WHEN** 请求第二碟曲目或响应找不到目标 track
- **THEN** 仅解析指定位置，未命中返回错误；空目标仅返回歌集与清单

#### Scenario: CV 与缓存
- **WHEN** 详情包含 CV 署名，或同发行响应被另一首复用
- **THEN** 遵循完整 CV 身份规则，不额外查询配音关系；缓存响应按当前目标重新解析

### Requirement: 清单与缓存
详情附带的清单 SHALL 一并保存，总数跨碟汇总，少于声明总数时为部分清单。已存 MusicBrainz 清单不因时间失效，原始 HTTP 响应使用配置有效期。CAA 封面独立于元数据配额。

#### Scenario: 清单复用
- **WHEN** 详情响应已包含完整清单
- **THEN** 保存清单且不再发一次清单请求；封面规则见[封面规格](../cover-art/spec.md)
