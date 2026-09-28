# matching/provenance Specification

## Purpose

用匹配来源、上游曲目标识和匹配时间记录收藏身份，防止重复入库。候选显示时区分标识命中、ISRC 命中与文本疑似重复，避免把不同发行位置自动合并。

## 当前限制

灰色行的发行上下文及编辑身份保存见 [G02、G03](../../../known-gaps.md)。

## Requirements

### Requirement: 身份与唯一性
曲目 SHALL 可保存 match_source、match_ref、matched_at；来源和标识组合在有来源时唯一，无需匹配时为空。MusicBrainz 真实位置使用 track MBID，iTunes 使用小写 country:trackId，QQ 音乐使用歌曲 MID。MusicBrainz 搜索缺少位置时可产生 recording@release 合成标识，但不能用于精确详情补全。

#### Scenario: 同录音不同版本
- **WHEN** 同录音出现于 CD 和数字发行的不同 track
- **THEN** 两者匹配身份不同，不能仅按 recording 判为同一清单位置

### Requirement: 候选本地识别
候选 SHALL 依次按来源+标识、ISRC、无匹配来源曲目的规范化曲名+署名查找；文本阶段仅在双方都有时长时要求差值 ≤3 秒。前两类显示本地与上游徽章，文本命中显示疑似标记，均不自动执行合并。

#### Scenario: ISRC 与文本命中
- **WHEN** 标识未命中但 ISRC 相同，或无来源曲目的文本相同
- **THEN** 分别标注「经 ISRC 匹配」或「疑似重复，需确认」；已有来源的曲目不参与文本阶段

### Requirement: 重复入库处理
显示候选时 SHALL 查询当前本地状态；搜索中已有曲目显示已在库中，有本地标识时可跳转。写入发生唯一约束冲突时返回已有曲目，不创建第二条。

#### Scenario: 并发加入
- **WHEN** 搜索和复核几乎同时加入同一来源同一标识
- **THEN** 后一次返回已有曲目身份，不显示唯一键错误

### Requirement: 身份编辑格式
手动身份编辑 SHALL 校验 MusicBrainz UUID、iTunes 两位小写地区加数字编号，或 QQ 音乐 10–20 位字母数字的歌曲 MID 格式；普通浏览不改变身份。编辑入口及保存行为见[曲目编辑](../../editing/track-editing/spec.md)。

#### Scenario: 非法标识
- **WHEN** 用户提交不符合来源格式的标识
- **THEN** 标识更新失败并提示格式要求；其他字段的事务保护仍待 G03 完成
