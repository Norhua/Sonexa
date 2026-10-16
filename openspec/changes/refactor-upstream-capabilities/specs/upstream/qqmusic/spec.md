# Spec Delta

## MODIFIED Requirements

### Requirement: 请求与限流
QQ 音乐 SHALL 以 POST JSON 请求 `u.y.qq.com/cgi-bin/musicu.fcg`，歌词回退以 GET 请求 `c.y.qq.com` 的 `fcg_query_lyric_new.fcg`（`format=json&nobase64=1`），均携带 `Referer: https://y.qq.com/`。无需账号或密钥，但与其他上游一样受统一出网条件约束。元数据搜索、详情、清单分页、封面查询、独立歌词搜索及获取共用一个进程级令牌桶；默认值、可配置范围和保存规则见[上游设置](../../platform/settings/spec.md)，交互优先、重试、突发容量及图片下载例外沿用[统一配额](../provider-registry/spec.md)。元数据请求沿用响应缓存，同一地址的不同请求以请求体区分缓存，原始响应不按日期失效，显式刷新绕过读取并可更新缓存；手动歌词搜索与获取为[歌词搜索](../lyrics-search/spec.md)规定的不读写响应缓存例外，QQ 手动封面查询同样不读写原始响应缓存，但继续登记搜索得到的封面目录。

HTTP 200 不代表成功：响应顶层 `code` 与每个被使用的请求块的 `code` SHALL 均为 0，否则作为来源错误报告，不把空数据当作无结果；手动歌词中部分请求块失败时仍按本规格的普通块回退与通道规则保留其他可用内容。

#### Scenario: 请求块错误码
- **WHEN** 响应顶层 code 为 0，但搜索块 code 非 0
- **THEN** 该次搜索显示 QQ 音乐错误，不显示为无结果，也不产生候选

#### Scenario: 出网禁用
- **WHEN** 未配置 MusicBrainz 联系方式时选择 QQ 音乐搜索
- **THEN** 与其他上游一样不发送请求，显示统一原因和设置入口

#### Scenario: 搜索调用共享平台预算
- **WHEN** 元数据、封面、歌词分别请求 QQ 歌曲搜索接口
- **THEN** 各自保留约定的检索词、结果数量与结果模型，但所有实际请求消费同一 QQ 配额，不按用途增加额度

#### Scenario: 刷新与禁用缓存不同
- **WHEN** 用户刷新 QQ 元数据并随后发起手动歌词或封面查询
- **THEN** 元数据刷新可写入新响应缓存，手动歌词和封面查询不读写原始响应缓存；封面目录和已保存歌词仍按各自业务规则处理
