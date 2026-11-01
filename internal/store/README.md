# 队列与复核问题存储

## 新库边界

当前空库初始化代际为 `track-identity-v3`。此前 `track-identity-v2` 只有输入 profile，没有整数修订和问题表，不能当作本结构打开；`upstream-snapshot-v1` 及没有代际标记的旧业务库也明确拒绝。程序不转换历史输入、不修复旧收藏身份、不自动删除用户数据。同代际重开不重复初始化，高于本程序支持的 schema 版本仍拒绝。

收藏继续由 `track(match_source, match_ref)` 的部分唯一索引约束；Recording、ISRC 不是唯一键。终态的本地目标 ID 和来源身份属于当次处理诊断，没有指向收藏的外键，不会因删除/换绑抹掉历史结果，也不能作为新输入的选版映射。

## 原始输入

`import_item` 保留批次、行号、完整 `raw_row` 和解析列。数据库触发器拒绝改写这些原始字段；复核查询必须另外保存。`input_profile` 保存版本化 `matching.InputProfile`，含原文、限定、解析精度与失败状态；派生 profile 可重建，不取代原始行。

- `revision`：从 1 开始的正整数，队列状态、失败/重试、候选更新和准入结束时递增。`updated_at` 仅供展示，不作修订令牌。准入 baseline 读取该整数。
- `review_reason / review_evidence`：当前行的待处理原因与证据，仍保留既有逐行读取协议。
- `resolution_reason`：与状态分离，区分新建、无需匹配、精确已有、用户跳过及移出。
- `track_id / resolution_target`：本地 ID 与当次核验的来源 track/release 身份。
- `resolution_problem_id / resolution_problem_revision`：成对保存当次问题范围的诊断修订，不引用未来的活跃问题。

## 活跃问题基础结构

| 表 | 内容 |
|---|---|
| `review_problem` | 稳定 ID、active/closed、正整数 revision、规则版本、语义锚点和等价证明 JSON、查询与候选事实及各自修订、时间 |
| `review_problem_item` | 问题与来源行、是否活跃、成员修订、采用的输入修订与证明 JSON、时间 |

`review_problem_item(item_id) WHERE active=1` 是部分唯一索引，一行最多一个活跃归属。插入或更新活跃成员须引用当前仍未终结的行修订和 active 问题；关闭问题/结束行前必须先解除活跃成员关系。历史成员可保留但不占用活跃归属；批次删除级联移除其行和成员，不级联删除其他批次原文或共享问题。

整数列校验类型及正值，profile、目标、锚点、证明、查询和候选事实列校验 JSON。JSON 可解析不代表证明成立；语义证明仍由消费业务判断，不能由数据库唯一索引替代。

**阶段边界：** 当前落地问题表、成员约束、整数修订及单行准入/跳过事务。语义归并、创建/加入/拆出/关闭的业务事务、问题查询的版本化 DTO、全部交互入口的范围 CAS、多行批准和租约仍按变更后续任务实施。旧逐行队列不会仅因存在这些表就自动成为问题队列；`merged` 的旧写路径另由任务 3.4 移除。

## 单行接受与精确跳过

- `library.PrepareCandidate` 在必要资料加载前记录输入修订，计划绑定本次候选的来源 track/release；提交先检查输入仍活跃且修订未变，再进入任何收藏写分支。
- `PrepareSkipExisting` 只接受来源已验证的精确候选、输入 ID/revision。准备与提交都不出网，不做成员新增确认；计划绑定本地 ID、track 与 release 修订，目标删除、换绑（含换回）或发行关系变化时拒绝。
- `CommitAdmission` 统一按实际结果记终态：新建为 imported，已存在为 `skipped/existing_track`。不接受客户端指定终态，不以同 ISRC/Recording 复用其他 track。已有目标只写输入结果及 `operation_result`，不修改任何收藏表、封面、歌词或补全状态。
- 同一成功 plan ID/hash 重试返回原结果；另一旧计划、旧改搜/失败响应不能恢复终态。Runner 与在途改搜使用起始整数修订 CAS，旧同步队列写接口也始终拒绝终态。
- App 与 Runner 对纯已有结果不发收藏变更通知、不启动补全；独立任务/问题通知与前端完整跳过动作随后接入，不把这一步称为完整事件交付。

## 验证

`queue_schema_test.go` 使用临时文件 SQLite 验证整数修订、原始行不变、JSON/正整数约束、跨批成员重启保留、并发单活跃归属、终态/旧修订拒绝归入、历史目标保留、同 ISRC/Recording 多 track 共存及旧代际无改写拒绝。生产语义归并验收随对应任务补充。`queue_cas_test.go`、`library/single_input_test.go`、`app/single_input_test.go` 验证单行跳过、并发一次新建、重复计划幂等、删除/换绑/错误回滚、迟到响应拒绝和真实 App 离线跳过。无写入测试读取全部收藏表（包括 FTS 与封面目录）并在 Go 中排序比较，不依赖所有表都有 `rowid`。
