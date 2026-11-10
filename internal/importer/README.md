# 导入与复核

文件输入与同步歌单共用 `Runner → library` 准入流程，不按上游分发、不把同步输入写成临时 CSV。
自动匹配仍仅使用 MusicBrainz；歌单平台身份只用于同步，不直接成为元数据绑定。

- 每次输入保存原始内容和自己的目标。`merged_item_id` 只是共享调度关系，merged 输入仍计入原批次。
- 有效代表输入来自 active 目标，不改原文；撤销最初主目标后，主 ID 和手动草稿继续服务剩余目标。
- 判重覆盖全 track。已入库与未入库使用同一严格版本、位置、时长和确认上下文证据，入库状态不参与消歧。
- 新建结果称“完成导入”：含有效音乐库目标才入库，仅同步则不入库。重复结果称“复用本地，未自动入库”，不改现有归属。
- `deliverImportTx` 原子结束主任务、记录全部有效输入确认并投递同步引用；被吸收输入不改回活动状态。
- `MarkDiscarded` 撤销 UI 列出的全部有效目标，同步项记 ignored。恢复必须建立新尝试，不复活历史终态。
- 目标变化提交后通知 Runner，修订失效的在途匹配收到取消。有剩余需求则释放 matching claim 后重排；所有目标消失不创建孤立 track。
- 批次统计按输入目标计移出，仍保留历史分母；Runner 判断是否仍有工作使用实际调度状态，不能把已取消主目标的统计当作共享任务已结束。
- 新建和跳过均在提交后立即通知本地实体失效；通知不等待后续补全。暂停/重启只重排未结束工作。

复核 `importDestinations` 返回完整有效输入目标，页面可以按歌单 ID 合并展示但不能改写目标。
无需匹配使用有效代表的字段，原始输入聚合仍保留所有来源供审计；手搜草稿继续按共享主 ID 保存。
准入计划在服务端封存目标与修订，最终事务在“命中已有身份”的短路前也必须重验。

## 读取性能与历史复核率

- 离线整理以当前值、有效快照和历史确认原文建立保守曲名索引，取**全部有效输入候选集合的并集**，再使用原证据函数判定；不能只按首输入过滤从而丢失别的输入歧义。
- `review_revision` 由数据库触发器随相关事实提交/回滚，跨连接生效。同一修订的重复汇总/队列读取不重复整理，封面与响应缓存写入不触发无关判重；实际终态投递仍在写事务内完成。
- 队列输入/目标使用两个只读查询，候选本地身份/入库/封面修订批量查询；计数不加载候选 JSON。同步列表一条关联查询，不为只读预览抢占 `BEGIN IMMEDIATE` 写锁。
- `review_entered` 与 `processed_once` 为每次有效输入的历史布尔事实。review/failed 进入复核页时对所有活动共享输入累计一次；合入正在复核的任务也累计。接受、移出、取消及重试不清零；忽略恢复属于新的输入尝试。
- `BatchStats.reviewEntered / processedOnce` 用于历史介入率；`review` 是当前待选数、`failed` 单列。原 `Processed()` 继续服务调度进度，不把历史完成事实误当作重试已完成。零历史处理显示“—”/“0 / 0”。

验证入口：`internal/store/{review_performance,owned_import,import_identity,import_delivery,confirmation_context}_test.go`、
`internal/library/{admission,import_targets}_test.go`、`internal/importer/streaming*_test.go`、
`internal/app/import_destinations_test.go`。这些是离线契约/绑定测试，不替代前端与原生验收。
