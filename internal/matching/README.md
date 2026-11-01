# 元数据输入证据

本目录同时保留检索评分与严格元数据证据，两者不可替代。**展示分数不等于身份概率**。

## 入口与隔离

- `Weights.ScoreCandidate`、`CompareTitle`、`CompareCredit`：既有检索展示与歌词比较入口，默认权重及旧语义不变。
- `NewInputProfile`、`ParseMetadataText`、`ParseMetadataCredit`：本次元数据输入的证据模型，保留原文、规则版本、未知注释和完整限定。
- `CheckMetadataEvidence`：只检查一个候选是否满足本次输入，不检查收藏、不选择发行、不授权写入。
- `CheckMetadataPool`：保留 eligible / excluded / unresolved 三类；有未排除的未知竞争者时不得进行自动选版。
- `MetadataFacts` 必须来自同一套有出处的事实，不能把互相矛盾的用户当前值与上游快照拼为假候选。完整性及位置验证由资料提供者声明，不由搜索排名推断。

当前阶段 Runner 已传递持久化 profile，并在 library 自动选择前解析非精确身份；完整的输入优先选版及精确跳过仍由变更后续任务完成。歌词不读取这些新元数据规则。

## 来源身份与接受准备

- `CandidateResolver` 由 matching 声明，`InspectCandidate` 不出网；`LoadDecisionFacts` 按给定候选返回同序结果，明确区分 exact / ambiguous / not_found / insufficient / invalid / unsupported。上游错误保留为错误，不伪装为空结果。
- `CandidateSources` 在 App 集中装配，library 通过注入消费。非 App 的 registry 调用按能力接口发现来源，不按来源 ID 分支。
- MusicBrainz 的 `recording@release` 仅用于召回。解析只查询给定发行，检查完整清单、真实 track、碟序和位置；同发行多位置不按首项或收藏状态选择。同次发行共用解析结果，并复用已有响应缓存、配额和重试。
- iTunes 在来源内检查小写 storefront 与曲目/发行一致性；QQ 在来源内检查 MID。已有精确候选不额外出网，未实现的决策清单补取明确 unsupported。
- `library.PrepareCandidate` 先解析再创建 seed；`CandidateSeed` 和直接 `PrepareAdmission` 均重新检查精确身份及一致的发行关系，不允许合成 seed 绕过。完整身份不要求展示字段齐全，缺失名称/署名仍保持未提供，不能借输入填充。
- `App.PrepareAdmission` 返回正常计划，或者仅带 `resolution` 的候选展开结果。后者没有可提交的计划 ID/hash，前端不调用 commit，而是回到列表选择真实位置；展开说明显示碟号与轨号。直接接受/加入兼容入口不能提交展开结果。
- 自动解析得到的真实候选再执行输入证据核验；仍有未解析目标时保留复核，Runner 保存展开候选而不是错误的合成收藏。精确所有权徽章、重复终态、问题修订和多行事务仍属于后续任务，不能由身份解析测试推定已完成。

## 名称、版本与署名

1. 完整名称的 NFKC、大小写、假名、空白和标点规范化可得到 `exact`；空值不能。相同未知注释原文可比较，但不能仅靠剥离不同未知注释证明等价。
2. 明示译名、原名、读音和带来源出处的别名可产生 `verified_alias`。无提示跨文字括号只保存备选，必须完整共同主体和独立的其他字段证据成立才提升；三项未验证备选不可循环互证。
3. 未知注释、无共同证据的纯译名、模糊相似不授予自动资格。不得将查询变体冒充上游别名，也不得把输入译名写入来源快照。
4. `(Vocal) Song` 和 `Song(Vocal)` 的 Vocal 为普通人声描述，不等同于 Off Vocal。
5. 版本在信息丢失前提取，保存类型、完整负载和原文。Remix 作者、Live 场次、重录/重制年份、限定版子型号、国家和载体不能只比较关键词。未识别写法保持保守，不按歌曲名称特判。
6. 角色与 CV、明确合作成员分别比较。相同声优不授权跨角色；发行署名或团体成员关系需要明确来源、对应专辑和已验证位置才能解释差异，不能覆盖明确的角色或合作冲突。

## 数值与自动证据

- 导入 profile 区分 missing / known / invalid，原始行不修改。时长支持 `mm:ss`、`hh:mm:ss`、明确的秒/毫秒后缀及表头单位；无单位纯数字不按数量级猜作身份事实。旧 `ParseDuration` 的宽松工具行为不用于新映射证据。
- 位置必须正整数，可读取有效 `n/总数`；碟号缺失不补 1。一般年份只作语义未指定的旁证，不能混用首次发行年与再版年决定身份。
- 时长比较独立于 relevance：差 ≤3000ms 为支持，>3000 且 ≤10000ms 为弱相容，>10000ms 为冲突。支持与弱相容不等于 exact，也不能构造输入等价键。
- 精度最多补偿秒级表示的半单位舍入，不按歌曲长短放宽，不接受任意大精度扩大容差。
- 自动候选要求可靠曲名及署名/专辑正证据，明确输入不能有未解释冲突；目标专辑与位置资料不足仍 unresolved。
- 截断只记 `compatible_truncation`。专辑截断当前采用至少 12 个规范化字符、完整目标的 70% 以上前缀，并要求可靠歌曲/署名、完整专辑资料、已验证位置及相同发行限定。短通用前缀或限定尾部不明仍复核；即使通过也须继续检查其他竞争者。

## 存储边界与验证

`import_item.input_profile` 保存完整 JSON，文件导入经 App → Store → Runner 传递；无 profile 的内部调用由已明确单位的映射值构造，不猜原始列。本次新库代际为 `track-identity-v3`，旧代际（含仅有 profile 的 `track-identity-v2`）拒绝打开，不自动迁移或删除数据。整数修订、问题及成员约束见 [store 结构说明](../store/README.md)；语义归并与问题生命周期仍由后续任务实现。

定向验证：

```sh
go test ./internal/matching ./internal/importer ./internal/lyrics
```

测试包括 unknown/conflict/truncation 的区分、可靠异写与反例、完整版本负载、角色/合作保护、原始行及解析状态持久化、relevance 与时长独立、未知竞争者保留。`internal/app/identity_admission_test.go` 验证真实准备入口的身份展开、已有目标及 seed 拒绝；自动跳过和复核事务仍需要后续 Runner/App/Store 集成验证，不能由上述测试替代。
