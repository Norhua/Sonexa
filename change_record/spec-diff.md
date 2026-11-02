# 代码与主规格差异（共享文档）

本文件汇总尚未合并进 `openspec/specs/` 的实现变化；后续会话共同维护，不按轮次拆分。
本轮来源：`1-why.md`、`1-tasks.md`、`2-why.md`、`2-tasks.md`、`3-why.md`、`3-tasks.md`、`4-why.md`、`4-tasks.md`、`5-why.md`、`5-tasks.md`、`6-why.md`、`6-tasks.md`。**主规格尚未修改，不代表已经 archive。**

## SD-1：输入、活动条目与最终收藏分开，跨批次去重

待同步：
- `openspec/specs/import/table-import/spec.md`：预览、提交与统计
- `openspec/specs/matching/pipeline/spec.md`：导入处理与状态、持久化、进度与日志
- `openspec/specs/platform/data-store/spec.md`：补全与复核状态

实现：`store/queue.go`、`store/import_identity.go`、`importer/runner.go`。

- 导入在事务中保留每次有效输入及批次统计，但相同活动目标只有一个 queued/matching/review/failed 主条目。被吸收输入为 `merged`，用 `merged_item_id` 指向主条目，**不同于旧 `merged_into` 所表示的收藏目标**；主条目接受、跳过或丢弃不复活被吸收输入。
- 完全一致的已映射可检索文本可合并；不同写法须完整曲名、艺术家、专辑严格等价，允许译名/读音等非版本注释，不使用模糊分数、文件名或原始数组相同代替字段语义。
- 保护 Live、Remix、伴奏、Remastered、Deluxe/Edition 等版本差异；截断字段不能与另一完整写法构成精确证据；CV 的声优、主署名省略 feat. 等搜索旁证不单独证明完整署名相同。已知碟号、音轨号或超过 3 秒的时长冲突不合并。缺失字段不把多个已知位置桥接为一个目标。
- 另一条路径是在自动选版已得出明确曲目后，用 `provider + match_ref` 关联活动条目；所有输入与候选的曲名、专辑上下文须一致，位置/时长不冲突。仅候选列表有交集、同 recording 或同 ISRC 不合并；不同艺术家署名可借相同已解析 track 得到确认。
- 手动改搜开始或“无一匹配”清除旧自动解析身份，避免用户已否定的结果继续成为合并依据。当前实现不把用户修改后的搜索文字当作原始输入身份。
- 每次有效输入仍计入其批次分母；被吸收输入计入 `merged`，已入库重复计入 `skipped`，两者计入已处理。完成日志在原入库/复核/未匹配/失败四类之外列出跳过、合并输入和移出，不能继续声称原四类覆盖全部输入。

场景：相同表格重复导入、不同文件的相同输入、带翻译/注音的异写以及并发提交均只保留一个相同活动目标；不同原始内容可在主条目看到，原文不因去重或搜索改写。

## SD-2：不保存源文件位置，复核显示原始内容

待同步：
- `openspec/specs/import/table-import/spec.md`：入口与文件、预览、提交与统计
- `openspec/specs/review/review-queue/spec.md`：队列浏览
- `openspec/specs/platform/data-store/spec.md`：补全与复核状态

- 文件路径只在选择、解析和提交读取期间使用；提交不写入批次来源。原有 `filename` 存储/DTO 字段保留空值，不承载路径或文件名。文件移动、删除或修改不影响已提交任务。
- 源文件行号不写入条目；原 `row_index` 只存内部有效输入入队序号，用于稳定处理顺序，不表示源文件位置。
- 复核 `originalInputs` 聚合主条目和被吸收输入的原始列内容，按完整内容去重。完全重复只显示一行，异写指向同目标时显示多行，保留列内空白和文字。
- 导入队列和历史列表显示内部批次 ID，不再显示文件路径。预览阶段仍可显示源行号以辅助用户确认，汇总输入行数不属于源位置记录。

## SD-3：候选本地身份只认 track 精确绑定

待同步：
- `openspec/specs/matching/provenance/spec.md`：Purpose、候选本地识别、重复入库处理
- `openspec/specs/review/review-queue/spec.md`：候选与请求归属、处理动作
- `openspec/specs/matching/pipeline/spec.md`：导入处理与状态

- `ResolveLocal` 只查询来源 + track 标识；不通过 ISRC 或无绑定文本命中标注本地，不再生成疑似本地候选。ISRC 是 recording 级搜索线索，不是收藏或发行位置身份。
- 复核候选行：未绑定显示“接受”，精确绑定显示本地/上游徽章及“跳过”。搜索页仍显示“加入曲库”/“已在库中”。
- 删除复核 `MergeCandidate` 绑定，新增 `SkipReviewCandidate`：服务端重新查当前精确绑定，不信任前端 localTrackId；成功只将条目标记 skipped 并记录 track_id，不修改收藏字段或新增收藏；绑定已失效则拒绝。
- 用户明确选择仅修改候选行按钮。**保留“无需匹配”“移出队列”“无一匹配”和失败重试**，新增发行仍走原额外确认流程。
- **轮次 2 替换旧的“输入曲名/专辑双精确”判据**：导入前先离线扫描所有来源收藏（含 QQ、MB、无绑定本地），独立比较当前值及身份核对有效的上游快照。全库三字段严格等价、无已知位置/时长/版本冲突且目标唯一时直接 skipped，不依赖默认 MB 返回结果或网络成功。
- 原始输入已被接受/跳过的确认关系优先；精确来源 track 候选是其次的额外证据，三字段相同是基础证据。每一条被合并输入须分别支持同一唯一最高证据目标，不能借邻居输入或“只有一个已在库”消除歧义。
- 精确候选能证明绑定时，曲名严格相同、艺术家可靠一致且专辑是有意义的完整词边界简称可确认（`LINES` / `Trinoline Original Soundtrack ~LINES~`），不使用任意子串、Jaro-Winkler 或泛称 Soundtrack/Album 等决定相同 track。Original Soundtrack/Original Game Soundtrack 等为专辑类型描述，不当作录音 Original 版本冲突。缺字段不增加证据，Live/Remix/重制/豪华版与位置/时长冲突仍拒绝。
- 只有不同来源的 recording/ISRC 交集、明显不同艺术家/专辑、多个等强度目标仍保留复核；`ResolveLocal` 的徽章仍只认精确绑定，离线重复排除不建立另一个上游绑定、不切换收藏主来源。
- 自动导入准入计划在最终写事务重做同一证据检查，包括其他来源刚刚入库的情况；确认则返回已有收藏并 skipped，不能证明的现存精确身份返回 plan_stale，不能借唯一约束绕过输入核验。

## SD-4：自动决策优先完整精确证据，而非单纯高分

待同步：
- `openspec/specs/matching/confidence/spec.md`：文本匹配程度、评分与高置信、配置与代表发行
- `openspec/specs/matching/pipeline/spec.md`：自动入库判定

实现：`matching/import_identity.go` 的 `AutomaticCandidates`、`library/selection.go`。

- 高置信结构条件和分数权重不变，人工候选池不因自动优先级而删掉结果。
- 自动决策存在曲名、艺术家、专辑三字段均精确的高置信候选时，只使用这一子集；没有时沿用全部高置信。不是选最高分，不用阈值差值决定唯一候选。
- 专辑比较增加去非版本注释及异写后的精确识别；普通词元包含、前缀和相似度仍仅为模糊。
- 筛出的集合继续执行同组、同曲目内容、同发行多位置、已有发行优先、总曲数/国家/稳定身份及准入检查。多个完整精确候选跨组仍复核，向已有组增加新发行仍需要用户确认。
- `3253212 / reche / 3253212` 的单曲三字段精确，两个 live 专辑仅艺术家一致，不再制造跨组冲突；单曲通过成员准入即可自动导入。`you` 的截断专辑不构成完整三字段精确，仍遵守跨组复核规则。

## SD-5：复核搜索按条目隔离，终态与请求顺序受保护

待同步：
- `openspec/specs/review/review-queue/spec.md`：搜索草稿、候选与请求归属、刷新与搜索竞争
- `openspec/specs/matching/pipeline/spec.md`：持久化、重试与恢复
- `openspec/specs/library/admission/spec.md`：有界批准与并发重验

- 页面内按条目 ID 保存文本草稿、来源、地区、手搜标记和展开状态；切换条目恢复自己的状态，不沿用上一条来源，不因恢复而自动出网。
- 后端保存实际搜索来源/地区及是否为手搜；空候选也能在重载时恢复来源。首次自动匹配来源取执行搜索的提供者 ID，不取页面默认选择。
- 表格列按**实际结果**来源的能力并集显示；搜索选择已改变但还没查询时，旧结果不被伪装成新来源。
- 重新搜索按钮在整个请求期间禁用，显示“搜索中…”及 Lucide 旋转图标、aria-busy；成功/错误结束后恢复。
- 地区建议及搜索响应都核对当前条目/请求序号；后端 search_revision 拒绝同条目旧请求覆盖新请求，条件写入拒绝对已处理或已重排条目保存搜索结果。
- MarkReview、RetryItem、RecordFailure 等不能使 imported/skipped/merged/discarded 恢复活动状态。合并新增输入会更新主条目修订，旧准入计划须重新准备；后台自动导入 plan_stale 重新排队决策，不错误标注成新增发行冲突。

## SD-6：可失效的输入确认关系与已有复核整理（轮次 2）

待同步：`matching/pipeline`、`matching/provenance`、`review/review-queue`、`library/admission`、`platform/data-store`。

- 新增编号 3 的 `import_evidence` 事务迁移：`import_confirmation` 保存不可变输入 ID、收藏 ID、身份/位置摘要和 JSON 判定依据，FK 保护原始条目及收藏关系。旧终态记录不盲目回填为确认关系。
- 接受、无需匹配、明确跳过、自动成功入库或自动排除重复，在对应写事务中保存全部被吸收输入的确认关系；失败不留下半个关系或错误终态。
- 确认关系在删除曲目、换绑、改变具体发行/发行上游或**既有已知**曲目位置时失效，使用时再核对摘要。轮次 6 修复：未知位置补全为已知不视为换位置；普通显示改名和无关封面/歌词更新不使确认关系失效；比较接受时目标与当前目标，已知位置/时长和新增版本冲突不可跨越，不能以原始输入未注明版本否定用户已经确认的映射。详情见 SD-9。
- 快照只在 track 身份、snapshot 所属具体 release、当前 release 身份及 release snapshot 全相符时参与，不能用失效快照，也不能混搭当前曲名与快照专辑构造不存在的匹配。当前明确位置冲突仍否决。
- 读取复核队列或任务汇总前，事务中整理未手动改搜的 review/failed 条目，以原始输入和当前本地证据排除已无必要的待办；不触发网络，不重做终态。
- 手动搜索开始就标记手工会话，读取整理不会移除在途或已改搜条目。手动搜索本身不是接受/跳过；保留上一轮请求序号及终态保护。
- 复核页监听库变化，后台新增收藏时重读队列与汇总；卸载取消订阅。规则修复前遗留的未改搜重复复核可在再次打开时整理，不必重导文件。
- 判定实现：`matching/owned_import.go`（纯业务证据）、`store/owned_import.go`（全库读模型、确认关系、事务整理）、`migration_import_evidence.go`；不新增上游分发分支或为去重调用其他上游。

## SD-7：元数据人工召回与自动身份证据分离（轮次 3）

待同步：
- `openspec/specs/matching/confidence/spec.md`：曲名与辅助字段检查、评分与高置信、独立歌词边界
- `openspec/specs/matching/text-processing/spec.md`：版本识别、执行与去重
- `openspec/specs/matching/pipeline/spec.md`：共用检索规则、自动入库判定
- `openspec/specs/upstream/musicbrainz/spec.md`：查询与结果粒度
- `openspec/specs/review/review-queue/spec.md`：候选说明及快照
- `openspec/design.md`：OpenCC 公共字典位置更新为 `internal/textmatch/data/opencc/`

实现：`matching/metadata_recall.go`、`matching/confidence.go`、`provider/musicbrainz/musicbrainz.go`、`textmatch/han.go`、`app/bindings_{upstream,review}.go`。

- **替换主规格“版本集合不同即从元数据候选列表删除”**：严格 `CompareTitle/ScoreCandidate` 仍用于身份和歌词，元数据四入口改用 `ScoreMetadataCandidate`，在严格匹配失败时有受限的人工召回路径。
- 输入未注明版本时，可从候选的括号或破折号版本描述中识别主体；主体须精确相同（允许既有非版本译名），不能凭任意子串、宽松相似度或截断扩展。输入明确写了版本时不放宽为裸曲或另一版本；普通标题中的 Live 等词不任意删除。
- 上游可提供 `Candidate.SearchTitles` 检索异写旁证。MB 用当前 track 所属 recording 的实际标题，避免罗马字 track 标题丢掉日文搜索依据；不替换 track 标题、发行或身份，不把同 recording 的四个位置去重成一个。该旁证只用于召回，不作自动身份事实。
- 艺术家比较增加直接、唯一的简繁单字对应作为人工召回的模糊旁证。复用并迁移既有 OpenCC v1.1.9 字典及许可证到 `internal/textmatch`；多义映射与两个繁体同归一个简体不判等；原始字符串、全局 Normalize、身份判重均不改写。歌词复用公共字符函数，保留原有禁止含假名/谚文等局部限制。
- 辅助字段检查仍保留：可比较的艺术家/专辑至少一处相符，或两处都不可比。不能只凭标题相同把不相关艺术家/专辑全部放进池子；没有为截图专辑翻译建立特例。
- 新召回候选 `Score.ReviewOnly=true`，强制 `HighConf=false`，`AutomaticCandidates` 再次排除。即使数字分数很高也不能自动入库；已有严格候选的自动优先规则不变。候选继续按来源 + track 标识去重，渐进检索有可展示候选即可停止。
- `CandidateRow.reviewOnly` 和现有 note 保存“仅供人工确认：版本未注明或使用检索异写旁证”；导入快照、手搜快照、队列重读、本地徽章刷新及编辑结果保持提示数据。**轮次 5 替换共享候选表格的展示约定：复核/上游搜索来源列不再渲染 note，只保留徽章（见 SD-8）；不删除后端数据或改变其他入口。** 人工接受/跳过仍执行原有精确身份、发行成员准入和事务检查，不因 ReviewOnly 禁止用户选择。
- 版本识别先 NFKC 并接受汉字/假名与英文词的相邻边界，`ハーサカver.` 与全角 `Ｖｅｒ．` 可识别，括号分类同样保留；英文单词子串（Never/Silver 等）仍不误判。

场景：截图原始输入在真实 MB 首次锚定请求返回 8 条人工候选，其中 4 条为目标演唱录音、4 条为同署名伴奏；目标 CD 第 2 轨确实可见，自动候选为 0。测试同时覆盖 4 个发行中的罗马字 track 标题、其他版本保护及所有应用入口。

既有队列只保存先前过滤后的快照，修复不自动出网重做它们；旧空结果需用户在新版点击重新搜索，新导入直接走新规则。没有数据库结构变更。

## 轮次 4 核对证据：同组多发行并列的真实选版（无新增行为差异）

- 本轮仅补测试、真实响应和说明，不修改生产选版/去重逻辑。主 `matching/pipeline` 已明确允许总数/非世界国家并列后按稳定身份顺序决定，此项不应在同步时误写为“多条同组发行必须人工”。
- `internal/app/release_selection_test.go` 重放真实「世界中の雨」搜索：11 个原版发行均 release 总曲数 4、CD、JP、同 recording/group，伴奏候选仅供人工；实际 provider → Engine → library → SubmitImport/Runner → 准入事务在空库选择 `07088ba6-2c40-356a-8d0c-ef72d8a21d93` 的 `5e5f141f-cdaa-3346-9f9a-43b572006e9b`。
- 该选择的决定性步骤是 `provider + release_ref + match_ref` 字典序，并非总曲数或国家优胜。最小 track MBID 另属 `70e8ff92-...`，不能将排序误解为只按 track ID。每种输入 22 组旋转/反转不改变结果；预置非最小发行、改总数/国家等明确反事实可改变结果，显示各层优先级。
- 旧 `csv_import_test.go` 只做 Engine 候选分类而不执行 library，现更名为 `TestCSVCandidateClassification` 并纠正必进复核/端到端的误导性说明；保留原分类断言，不将其等同于最终导入结果。matching 对应单测说明也同步纠正。
- 来源、完整 11 发行身份表、SHA-256 和详细结论见 `testdata/golden/responses/musicbrainz/sekaijuu_no_ame_selection.md`；复现命令和测试结果见 `4-tasks.md`。本轮真实来源取数两次（一次搜索、一次所选发行详情），后续所有回归离线。

## SD-8：候选来源列只显示徽章，入队原因与标题同行（轮次 5）

待同步：
- `openspec/specs/review/review-queue/spec.md`：队列浏览、候选与请求归属
- `openspec/specs/upstream/search/spec.md`：候选展示（共享表格）

实现：`frontend/src/components/CandidateTable.vue`、`frontend/src/views/ReviewView.vue`。

- 共享候选表格来源列只显示原有来源/本地徽章，不显示 `CandidateRow.note` 中的人工确认、疑似说明等文字，也不新增说明列或替代提示；复核与上游搜索一致。徽章、置信度、候选身份和接受/跳过/已在库中动作不变。
- 入队原因显示在当前原始曲名右侧，与标题同一行、基线对齐，使用 12px 灰色小字（标题 16px），不再单独使用带边框的原因区域。过长的标题或原因以省略号收尾，通过原生 title 保留全文，不挤出状态标签。
- 已有专辑组、已有发行、拟加入发行和判组依据继续直接展示在原始输入下方，无独立边框；只在存在实际证据时渲染详情区域。无原因不渲染原因文本，空/无效证据不留下空详情区域。
- 只改变展示，不改变 `reviewOnly` 的后端数据、自动匹配排除规则、人工准入或快照持久化；歌词等其他组件不受影响。此项替换 SD-7 中旧的候选来源列提示展示约定，不得同步成解除人工确认限制。

验证见 `5-tasks.md`：双浏览器覆盖亮/暗主题、1440/960 宽度、长短文本、缺原因/空证据、完整新发行证据、人工接受、本地跳过及上游搜索本地跳转；未运行原生 Wails 窗口人工验收。

## SD-9：人工确认后重复导入以接受时目标为校验基线（轮次 6）

待同步：`matching/pipeline`、`matching/provenance`、`review/review-queue`、`library/admission`、`platform/data-store`。

实现：`matching/confirmation.go`、`store/confirmation_context.go`、`store/owned_import.go`、`store/migration_confirmation_context.go`。

- 已确认输入可能与所选 track 存在版本未注明、异写或平台专辑名差异。重复导入先严格核对新输入与已确认原始输入，再确认原收藏绑定及接受之后的目标事实没有冲突；不再重新要求原始输入与当前目标版本相同。
- 编号 4 迁移为 `import_confirmation` 添加 `target_context`，同准入/明确跳过事务保存接受时的当前值、身份核验过的快照及绑定摘要，包括所有被吸收输入。判重决策版本变为 `owned-import-v3`，有基线的确认记录增加 approved_target_context 证据。
- 目标上下文校验比较“接受时当前值→当前值”和“接受时有效快照→有效快照”，不混用有利字段。已有有效快照不能变为失效快照；接受时无有效快照而后来补全时，以接受时目标事实核对新增快照。
- 版本检查保护完整括号/破折号说明，不仅检查 Ver/Remix 等关键词，因此 Singer A Ver.→Singer B Ver. 不能继续借旧确认；非版本显示改名/译名和字符形式差异不因名称本身失效。
- 碟号/音轨号从未知补齐不删除确认，绑定键允许该方向的补全；已知位置变更或清空仍由 trigger 删除，位置改回也不会恢复。明确时长冲突、原始/新输入提出的位置冲突同样不能绕过；删除、换绑和具体发行变化维持原失效规则。
- 不改 AutomaticCandidates、高置信、ReviewOnly、同 recording 多发行选版、总数/国家/稳定顺序规则。不存在有效确认关系时，人工召回的相似版本仍不能仅因已在库就自动跳过；一个输入指向多个有效确认目标仍复核。
- v3 历史确认没有接受时目标上下文，不从今天的数据伪造过去的批准，迁移后沿用旧保守检查；已经被旧 trigger 删除的关系不恢复。受影响条目可能需要在新版明确接受/跳过一次，之后重导不再因同一已确认版本差异进入复核。

验证：`6-tasks.md` 记录三条修复前失败与修复后通过；真实 MB 样例首次仍复核，人工接受后立即重导/补全后重导均 skipped=1，清 HTTP 缓存后不新增请求；迁移、回滚、重开、并发、多目标及身份/版本/位置失效反例通过。第 4 轮 11 发行选版结果保持不变，无公网或原生窗口测试。

## 验证与待确认边界

轮次 1–2 详细命令及结果见 `1-tasks.md`、`2-tasks.md`；其验证使用空测试数据库、离线上游桩和 Chromium/WebKit Wails 替身，未请求真实上游。轮次 3 见 `3-tasks.md`：新增真实 MB 响应及真实 Engine 联网验证（两次请求总计），离线四入口/快照/人工接受回归，全量 Go race、vet、前端构建/单测及双浏览器回归通过。均未读取或修改用户运行中的应用数据库，也未运行原生窗口人工测试。不承诺从无证据、任意完全不同名称猜出相同 track，不能确认时保留人工处理；大批量输入性能尚未专门测量。
