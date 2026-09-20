# upstream/itunes Specification

## Purpose

使用无需账号或密钥的 iTunes Search API 补充搜索和封面来源，按地区构造请求与曲目身份。明确当前字段能力、清单数量上限、精确目标和缓存有效期。

## Requirements

### Requirement: 请求与地区
iTunes 搜索和 lookup SHALL 共用默认每分钟 20 次的配额，传 country 而不传 lang。地区优先使用当次手动选择，其次文字系统建议，再使用设置默认或系统区域。建议规则为假名→日本、谚文→韩国、简体特有字→中国大陆、繁体特有字→台湾、西里尔字母→俄罗斯。

#### Scenario: 手动覆盖建议
- **WHEN** 输入含假名而用户选择美国
- **THEN** 请求使用美国；界面显示探测建议但不覆盖用户选择，不保证任何地区返回特定标题语言

### Requirement: 字段能力
候选 SHALL 保存响应提供的曲名、艺术家、歌集、时长、位置、年份与封面地址，不推断参与角色、ISRC、载体或发行国家。地区参数不是发行国家。缺少相关度时可用时长参与评分。

#### Scenario: iTunes 候选
- **WHEN** 搜索仅使用 iTunes
- **THEN** 不支持的候选可选列隐藏，已知字段正常显示，缺失字段不影响其他信息解析

### Requirement: 清单与精确目标
lookup SHALL 使用 limit=200，区分发行条目和曲目条目，以 country:trackId 精确选择目标；地区不符或未命中返回错误，无目标时仅获取清单。清单条目少于声明总数时保存为 partial。

#### Scenario: 长清单
- **WHEN** 发行声明 120 首且响应包含全部曲目
- **THEN** 解析全部 120 个位置；超过固定上限的清单不承诺取全，见 [G12](../../../known-gaps.md#g12--清单重取与数量上限)

### Requirement: 缓存与来源说明
iTunes 原始响应和清单 SHALL 默认缓存 30 天，过期重新获取；关于页显示来源说明，导出不包含图片。已收藏元数据的生命周期及条款核验见 [G10](../../../known-gaps.md#g10--itunes-数据生命周期)。

#### Scenario: 过期清单
- **WHEN** 用户再次打开过期的 iTunes 清单
- **THEN** 执行获取流程，不把已存清单永久视为有效
