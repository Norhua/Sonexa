# upstream/musicbrainz Specification

## Purpose

接入 MusicBrainz 作为主要上游：它提供本应用所需的绝大多数结构化字段，但严格限速且要求规范的请求标识，因此必须对请求频次精打细算。

## Implementation status

角色补充请求已按完整 CV 身份裁决移出详情生产路径。手动 CAA 索引未纳入图片池的并发边界见 [G05](../../../known-gaps.md)。

## Requirements

### Requirement: 规范的请求标识
向 MusicBrainz 发出的每个请求 SHALL 携带包含应用名、版本与用户所填联系方式的请求标识头。联系方式未配置时 SHALL NOT 发出任何请求。

#### Scenario: 携带联系方式
- **WHEN** 用户已在设置中填写联系方式，系统发起一次 MusicBrainz 请求
- **THEN** 请求头中的标识包含应用名、版本与该联系方式

#### Scenario: 未配置时不出网
- **WHEN** 联系方式为空
- **THEN** 系统不发出请求，并向用户说明需要先填写联系方式

### Requirement: 速率限制
MusicBrainz 的请求 SHALL 默认限制为平均每秒 1 次，且该值 SHALL 可在设置中调整。搜索、导入匹配与详情拉取 SHALL 共用同一个令牌桶。

#### Scenario: 并发请求被排队
- **WHEN** 同时有 5 个 MusicBrainz 请求待发出
- **THEN** 它们按令牌桶排队发出，平均速率不超过每秒 1 次

### Requirement: 曲名与旁证锚定并联
有曲名的搜索 SHALL 以 recording 曲名为必须条件，歌集、署名及至多两个署名备选作为 OR 锚点，而不是 recording、artist、release 三字段 AND。仅曲名阶梯 SHALL 不带锚点，输出仍由统一匹配闸门筛选。

#### Scenario: 录音署名不同于导出署名
- **WHEN** 曲名与歌集对应，但导出歌手是歌集署名而非录音署名
- **THEN** 可通过歌集锚点召回，不因 artist 条件与 release 强制相交而丢失

### Requirement: 搜索结果展开为曲目粒度
MusicBrainz 的搜索结果 SHALL 被展开为曲目粒度——即「录音 × 发行 × 碟 × 位置」的组合，而不是以录音为一行。

#### Scenario: 同一录音出现在多张发行上
- **WHEN** 某录音收录于 3 张发行
- **THEN** 搜索结果列出 3 行，各行的载体、国家、年份、曲序可能不同

#### Scenario: 展开后的行可直接展示所需列
- **WHEN** 渲染搜索结果表格
- **THEN** 每行都能提供标题、艺术家、歌集名、时长、第几曲/共几曲、年份、载体、国家等列，不需要为此再发额外请求

#### Scenario: 搜索与详情清单键名差异
- **WHEN** 搜索响应使用 media.track 而详情使用 media.tracks
- **THEN** 均解析为位置，优先带真实 track 标识、载体、碟号和音轨号；仅有数字 number 时可回退位置，不猜测 A1 等文本编号

### Requirement: 详情拉取一次获取尽可能多的信息
在每秒 1 次的限制下，详情拉取 SHALL 以单个请求取回全部所需信息，SHALL NOT 拆成多次调用。该请求 SHALL 至少请求以下可用信息，缺失项如实保留为空：曲目级与发行级署名、演唱者、器乐演奏者、作曲、作词、主要类型与次要类型、年份、曲序与碟序及总数、风格、国家、厂牌、目录号、载体格式、时长、ISRC、注释。

#### Scenario: 一次请求取回全部字段
- **WHEN** 系统为某条候选拉取详情
- **THEN** 只发出一次请求，从该响应提取已提供字段；上游未提供的实际参与角色不能从署名推断

#### Scenario: 作曲作词的来源
- **WHEN** 解析详情响应
- **THEN** 作曲与作词从作品层级的关系中取得，而不是因为录音层级没有该信息就判为缺失

### Requirement: 详情响应顺带写入曲目清单
执行详情拉取时 SHALL 保存同次响应中的清单，不另请求。完整响应标 complete，已知返回少于声明总数时标 partial；总数 SHALL 汇总所有碟而非只取目标单碟。

#### Scenario: 清单免费获取
- **WHEN** 系统为某条候选拉取详情
- **THEN** 该发行的完整曲目清单（含每条的曲目标识、曲序、标题、时长、录音标识）被一并写入，不额外消耗配额

### Requirement: 署名与实际角色独立解析
详情 SHALL 分别提供结构化署名和实际参与角色，曲目署名优先于录音、再回退发行署名，成员与原串使用同一优先级。普通署名 SHALL NOT 推断为 vocal；完整 CV 身份遵循 library/artists，不因角色类型额外查询配音关系。

#### Scenario: 角色身份不触发额外配音查询
- **WHEN** 详情包含角色或 CV 署名
- **THEN** 保留完整署名身份，只解析响应中的实际关系，不自动发起角色配音补充请求

#### Scenario: 同发行缓存复用仍各自定位
- **WHEN** 同发行第二首的详情复用第一首请求的原始响应缓存
- **THEN** 重新按第二首真实 track 标识解析，不复用第一首的目标解析结果

### Requirement: 详情必须定位指定曲目
有目标的详情 SHALL 精确命中 track MBID；未命中返回错误，不用同 recording 或首曲替代。无目标请求 SHALL 只返回发行与清单，不写曲目详情。

#### Scenario: 非首曲详情
- **WHEN** 目标为第二碟某个 track
- **THEN** 仅返回该 track 的标题、署名、位置和关系，不因曲序重复而命中第一碟

### Requirement: 封面索引服务独立于元数据限流
MusicBrainz 的封面来自独立的封面档案服务。其图片与索引请求 SHALL NOT 占用 MusicBrainz 的每秒 1 次配额，而 SHALL 受封面工作池的并发上限约束。

#### Scenario: 取封面不拖慢元数据请求
- **WHEN** 封面工作池正在并发取 8 张图
- **THEN** MusicBrainz 的元数据请求仍按每秒 1 次正常进行

### Requirement: 上游相关度分值参与评分
MusicBrainz 的搜索响应提供相关度分值，系统 SHALL 在置信度评分中使用它。

#### Scenario: 相关度参与打分
- **WHEN** 对 MusicBrainz 候选评分
- **THEN** 其相关度分值按约定权重计入总分

### Requirement: 数据可长期留存
MusicBrainz 的数据可自由缓存与长期存储，SHALL NOT 被施加缓存有效期作废。关于页 SHALL 对其署名。

#### Scenario: 旧数据不作废
- **WHEN** 某 MusicBrainz 来源的曲目清单获取于一年前
- **THEN** 系统直接使用，不因时间而重取
