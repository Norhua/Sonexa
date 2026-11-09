# 歌单消费能力

`playlists.Provider` 由业务声明，只依赖 `provider.Upstream`，不要求元数据、封面或歌词能力。具体平台解释链接、原生字段、分页和错误；业务、存储、绑定不构造平台请求或启动 Python。集合构造拒绝重复身份，来源列表和请求查找使用同一登记；运行环境不可用通过来源可用性报告，不伪装为空歌单。

## 快照契约

- `Ref` 为平台及规范歌单 ID；`Entry.TrackRef` 为平台歌曲身份，不是本地元数据绑定。
- `EntryKey` 区分同曲出现。适配器在没有稳定原生条目 ID 时按歌曲 Ref + 出现序号生成，位置只是顺序。
- 基础字段用 nil 表示未知，已知空值保留空；署名保持原顺序，毫秒时长不默认造 0。
- `Completeness` 同时保存声明总数、原始位置数、普通歌曲数、明确排除数与页数。来源必须证明完整，业务 `Validate` 再核对身份、位置覆盖、出现键唯一、顺序及大小上限；零项只有明确完整零总数才有效。
- 错误统一为 input/access/restricted/incomplete/temporary/cancelled/runtime_unavailable/resource，只交付可展示的脱敏诊断。失败不交付可提交的部分快照。

当前应用已登记 QQ/Spotify 歌单能力，QQ 与其他能力共享实例；Spotify 运行环境缺失时仅标记该来源不可用。App 长期持有 Fetcher，限制总并发 2、同源 1，设置换集合不重建池，关闭取消排队/在途工作后等待退出。

## 数据与导入需求存储

新库代际为 `streaming-playlists-v1`。旧代际直接拒绝打开，不转换或自动清理历史数据。

| 表 | 责任与约束 |
| --- | --- |
| `synced_playlist` | 平台 + 规范 ID 唯一；保存上游名、别名、修订、获取/确认时间、摘要、完整性与排除项。不存分享链接。 |
| `synced_playlist_entry` | 歌单 + 出现键唯一；保留顺序和最新基础信息；同 track 可有多个出现。`imported` 必须引用有效 track，`pending/ignored` 不得有 track。只有 imported 可以上游缺席。track FK 使用 RESTRICT，禁止直接物理删除破坏同步关联。 |
| `import_batch` | 文件来源固定 `file → library`；同步为 `synced_playlist → synced_playlist`，保存创建时目标显示摘要；filename 始终为空。统计分母来自实际保存的输入，不因撤销需求缩小。 |
| `import_item` | 每次尝试的原始列与映射字段不可改写；`merged_item_id` 指向共享调度主条目，合并输入始终保留在原批次。 |
| `import_destination` | 每个输入自己的目标和 active/delivered/cancelled 状态、修订及显示摘要；一个同步出现最多有一个活动尝试。删除条目前在同一事务撤销并断开目标 FK。历史摘要用于审计。 |

`pending` 包括 queued/matching/review/failed，具体任务状态仍由导入队列保存。
`ignored` 不应有活动需求，恢复须用最新已确认快照建立新输入，而不是复活旧 discarded/merged。
`imported` 的可见性由有效 track 引用决定，不由任务的 imported/skipped 名称决定。
目前已实现这些状态的存储约束和事务原语；完整的用户忽略/恢复、上游增删确认与结果投递仍待后续任务接入，不能把此处的模型说明当作端到端完成。

活动证据只读取 active 目标的输入。撤销主输入目标但存在其他需求时，主条目保留为调度壳；
按输入 ID 选择仍有效的确定性代表，仅在返回值覆盖匹配字段，不改持久化原文。
原始内容聚合仍保留全部历史输入，已保存的手动搜索来源/候选/草稿不被清除；目标变化递增修订，拒绝旧搜索结果。
全部目标消失才将主条目置 discarded；被吸收输入保持 merged，不重复累计移出，不继续出现在复核队列。
历史终态记录用于审计、确认与原批次统计，不是新的活动需求。

### 本地实体与音乐库范围

`track` 存在表示本地实体，不等于收藏。SQL 无 in_library 默认值；低层创建输入显式指定归属，
普通本地创建明确入库，准入新建从持久化有效需求决定：有 library 目标才入库，仅同步不入库。
`created_at` 表示创建时间；`library_added_at` 为首次入库时间，从未入库为 NULL，取消/再次入库保留首次时间。
Track/TrackView/详情分别投影两种时间；跨前端时间为 RFC3339，未有首次时间的 AddedAt 为空。

`track_read` 与 FTS 内容保留所有本地 track，身份判重、详情、编辑、补全和真实结构引用不得按音乐库过滤。
列表/计数/搜索/导出和引用感知删除的音乐库范围将在任务 8 集中接入，当前还不能将同步存储暴露为可用 UI。

### 本批离线证据

`internal/store/synced_model_test.go` 覆盖归属、两条创建路径、首次时间、旧代际拒绝、跨平台 ID、
重复出现、状态/FK 约束、重启、快照与不可变输入分离、重复活动需求、事务回滚，以及撤销主目标后的证据/草稿/统计保留。
既有 store/importer/library/app/acceptance 回归共同验证普通文件与本地创建路径，不替代网络或安装包验收。
