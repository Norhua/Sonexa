# 实施记录

## 上游契约与来源适配（2026-10-01）

完成任务 1.1–1.7：公共领域快照、来源契约与身份解析、三来源转换、演唱属性保留和封面策略注入。研究响应作为 MusicBrainz 固定样本纳入版本控制，两组 14/15 首发行及独占曲位置已离线验证。

验证通过：

- `go test ./internal/domain ./internal/provider/...`
- `go test ./...`
- `go test -race ./internal/provider/... ./internal/covers/...`
- `git diff --check`

当前为分阶段实施：新快照契约已可调用，旧业务读写 DTO 暂时保留，待存储及用例层切换后移除；本阶段没有重建数据库或修改编辑器。以上测试不代表新准入、当前值保护或新编辑流程已经实现。未进行真实上游网络与原生窗口验收。

## 三层存储与文件服务（2026-10-02）

完成任务 2.1–2.9：重建空库初始结构并加入模型代际检查；身份、当前值和快照独立保存；基本信息可空，具体发行始终归本地组。旧模型明确拒绝打开，不转换或删除历史数据。

统一快照事务保护逐字段及逐角色用户值，处理 unknown/empty/value、部分清单与完整结构变更，保存多值标识、CV 署名区间、角色来源身份与属性。显式保存与有效快照相同的值恢复 follow。年份读取歌曲当前值，专辑署名与事实来自自身发行；歌词不进入索引或导出。解绑保留当前值并撤销旧快照，删除只清理无引用歌词及孤立本地实体。

歌词使用独立草稿、哈希不可变文件及事务引用，测试实际关闭/重开数据库、写盘失败、提交失败、并发读取和连接共用。封面来源目录映射回到适配器，新增暂存及提交回调接口，支持取消、失败重试、目录切换与同事务本地图片身份登记；现有独立封面采用入口继续可用。

验证通过：

- `go test ./...`
- `go test -race ./internal/store ./internal/covers ./internal/app`
- `go vet ./...`
- `git diff --check`

新模型的底层规则及文件接口已完成，但旧业务创建、成员迁移和编辑入口仍处于切换过程。第 3–7 阶段将实现并接入准入/保存计划、所有入口、多版本读取和两栏编辑器；此提交不宣称这些流程已完成，也未执行原生窗口验收。

## 归组与准入计划（2026-10-02）

完成 3.1–3.3、3.5–3.7：library 用例层、薄 App 计划绑定、双路径证据及可重建索引、完整成员影响、显式合组、15 分钟服务端不可变计划和有界确认凭据、事务内身份/证据/成员重验与幂等结果。导入终态已可随准入事务保存。迟到补全只保存事实及独立归组提议，提议处理重新读取有效事实，显式批准后才迁移成员。

3.4 的按需摘要与纯选版规则已实现并有测试：候选内已有发行优先、总曲数、非世界国家、稳定身份顺序，保留未知与位置/录音歧义，不枚举整组。该项仍未勾选，待 4.2 将 runner 接入并移除旧实体过滤直入规则。

验证通过 `go test ./...` 与 `go test -race ./internal/library ./internal/store ./internal/app`。新用例已测试并发新组、并发成员、精确曲目重复、修订变化、跨计划凭据、过期、提交故障及队列原子性。旧公开创建/编辑入口仍将在第 4、6 阶段切换，当前不能宣称入口约束已经全面完成。

## 导入、准入入口与精确补全（2026-10-02）

完成 3.4、4.1–4.5，累计 28/53 项。本轮按用户要求完成测试和阶段提交后停止，尚未完成整个变更。

- runner 接入 library 的按需摘要、同组/同内容判断及选版；匹配层不再使用实体载体过滤决定入库。自动准入遇到新增发行、资料不足、证据冲突或未知总数时进入复核，新增发行的计划依据随条目保存，四类进度统计互不重叠。同批前序提交会影响后续候选准入。
- 导入保留有有效映射数据但缺曲名的行，分别统计曲名/艺术家/专辑缺失。CSV 物理空行及 XLSX 空记录单独跳过，支持前置空行和带换行的引号单元格，原始解析行不变；新增碟号入队字段。文件行数保留，进度分母取实际队列数量。
- CandidateRow 保留专辑/组署名、角色署名数组、发行总数与每碟数量、媒体、厂牌和标识数组。搜索添加、复核接受、无需匹配与手动创建统一委托准入计划；旧单步 API 在需要额外确认时返回明确错误，不能替前端批准。复核终态与收藏在同一事务中提交，本地三项基本信息均可空，歌曲署名不再推断为专辑署名。
- 灰色行改为传入发行 ID、碟号及轨号，由后端从有效绑定的发行快照读取清单事实，保留所属发行与 pending 状态。AlbumsView 调用同步更新。
- 抽出仅同步既存发行事实的 ApplyReleaseSnapshot。FetchTracklist 不再创建发行/组、不再按旧 TrackTotal 猜测完整性；后台、导入即时和手动刷新均调用来源 Complete，再按精确歌曲身份及请求前修订提交。清单刷新不改任意歌曲，失败保留已有事实。
- 修复 SaveSettings 持锁重建 runner 时的自死锁，重用单个 library 服务，保留已经生成的准入计划。更新使用伪标识的旧测试为格式有效的 UUID，并使用已准入发行验证 force 缓存刷新。

本轮最终验证通过：

- `go test ./... -timeout=120s`（`/tmp/sonexa-stage4-full.log`）
- `go test -race ./internal/importer ./internal/library ./internal/store ./internal/app -timeout=180s`（`/tmp/sonexa-stage4-race.log`）
- `go vet ./...`（`/tmp/sonexa-stage4-vet.log`）
- `/Users/amadeus/.go/bin/wails generate module`（本机 PATH 未包含 Wails；生成文件按仓库规则不提交）
- `cd frontend && npm test`：7 个文件、39 个测试通过（`/tmp/sonexa-stage4-frontend-tests.log`）
- `cd frontend && npm run build`：类型检查与 Vite 构建通过（`/tmp/sonexa-stage4-frontend-build.log`）
- `git diff --check`

绑定生成后修正两处旧 `Models.store.CoverVariant` 引用为 `Models.domain.CoverVariant`。Wails v2 对部分 `Field[[]T]` 字段仍输出 `Not found` 警告，生成模型降为 any，并产生不能用于这些字段的构造转换；现有前端只使用类型与原始返回对象，没有调用这些模型构造器。后续第 6 阶段应提供稳定的前端 DTO 或修正生成方式，并覆盖序列化/构造用例，不能把本轮生成成功视为新编辑 API 已验收。Vite 仍有大于 500 kB 的 bundle 提示。

### 下个会话继续的位置

先读本变更 tasks/design/specs，使用 `$openspec-apply-change unify-upstream-and-redesign-track-editor` 继续。4.6 尚未完成，因为旧编辑与改名入口仍可能直接写身份或迁移成员；需要结合第 6 阶段统一保存计划移除这些旁路，不能宣称所有业务写入口已经受准入保护。

重点待处理：

1. `internal/app/bindings_edit.go` 的 ApplyEdits、SetProvenanceManual、ClearProvenance 等旧立即写入接口，以及 `bindings_library.go` 的 ApplyAlbumRename/MoveTrackToRelease 路径。
2. 第 5 阶段多版本分区读取与共用确认 UI。当前搜索/复核/手动创建页面仍调用旧单步接口，遇到需要确认的新增版本会显示错误；必须改成 Prepare → 确认 → Commit 的共用交互，并接上归组冲突与待处理提议。4.3/4.4 的后端入口约束已验证，5.3 的界面流程尚未实现。
3. 新导入缺失统计和复核原因已由后端返回，5.6 仍需前端展示与流程验收；来源能力和代表展示偏好亦待第 5 阶段完成。
4. 第 6、7 阶段编辑会话、纯上游预览、共享影响及映射计划、原子文件保存与两栏编辑器尚未实施。
5. 未执行本轮 Playwright、完整 wails build 或原生窗口验收；第 8 阶段仍全部未勾选，主规格/README/known-gaps 的最终同步待实现完成后进行。

生产仅使用一个 library.Service；测试如需额外 runner，应注入 App 现有 service，避免对同一个 Store 重新安装计划签名验证器。`GetReleaseSnapshot` 只读取有效绑定快照，旧 `ReleaseSnapshot` 当前仍供提议接口使用，后续可统一其读取约束。

## 多版本读取与共用交互（2026-10-02）

完成 5.1–5.6，累计 34/53 项：

- 专辑名单按本地组统计所有 track ID，并区分在库成员数与有收藏版本数；代表仅提供自己的摘要事实和封面，不再借其他发行补值。新增 GetAlbumGroupDetail，各成员返回各自字段、每碟数量、封面、清单与收藏。完整清单也保留清单外收藏并读取用户当前曲名；所属发行参与清单精确身份判定。
- AlbumsView 按发行分区，默认展开代表清单，其他版本按需展开；所有收藏始终可见，标签覆盖各分区，灰色行传入自身发行和位置。DetailDrawer 使用所属发行资料。只显示已有的状态持久化，迟到详情不切换专辑。
- 搜索、灰色行、复核接受、无需匹配、本地创建和归组提议共用 Prepare → 确认 → Commit。冲突展示各组完整成员和证据，先重新生成合组方案，再单独确认成员变化。新增 ReplanAdmissionGroup 只接受服务端已保存计划，取消保留输入，过期/失败可以重新预览。新建曲目允许全部基本字段为空。
- 来源默认值和标识提示读取注册表，角色缺失提示由能力决定。匹配设置明确国家/载体仅影响展示代表；测试切换偏好改变代表但不改变自动选版和既有绑定。
- 导入向导区分缺失与空行跳过；复核直接展示新增发行、已有成员、拟加入版本与判组证据。导入证据保留拟加入发行快照，旧基线读取仍可用。

验证：Go 全量、library/store/app/importer race、go vet、前端 39 项单元测试与构建通过。Chromium/WebKit 的新准入、多版本、能力差异及既有交互测试通过；全量 Playwright 首跑中 8 项失败均为旧文案、字段数量及单步接口断言，相关 16 项修正后通过，其余 110 项首跑通过（日志 `/tmp/sonexa-stage5-all-e2e.log`、`/tmp/sonexa-stage5-e2e-fix.log`）。绑定已重新生成，现有泛型转换警告及 Vite 大包提示仍保留，第 6 阶段需要收敛。

此阶段未实施新编辑会话/保存计划，4.6 及第 6–8 阶段仍待完成。浏览测试使用 Wails 替身，不代表原生窗口、系统文件选择或真实网络验收通过。

## 编辑会话与影响预览（2026-10-02）

完成 6.1–6.3，累计 37/53。新编辑数据暂挂在旧 GetEditSession 的 editor 字段，保持现有界面可编译，待第 7 阶段替换界面后随 6.6 删除旧适配。

- ReadEditorSession 在读事务与歌词文件锁内取得当前字段、有效快照、来源状态、角色身份/属性、歌词和发行/组计数，返回歌曲、发行、组、快照及封面修订。打开编辑不出网。
- SearchEditCandidates 返回全部候选；PreviewEditCandidate 纯转换；LoadEditUpstream 经 RefCodec 解析和精确身份验证，完成资料只保存到 15 分钟服务端预览，不写正式实体。新 DTO 用 EditValue 的 state/valueJson 保留任意字段值，避免泛型切片构造器错误；Vitest 实际调用 Wails 生成构造器并验证角色/unknown/empty 往返。
- PreviewEditSave 检查只读字段、数值、角色、碟结构长度及源修订；清歌曲保留发行，整发行解除列出全部歌曲，部分解除准备无上游身份的本地发行。共享专辑署名/类型/结构/封面/绑定均需要范围选择。无名发行可准备，单曲换版仍检查成员准入，复用目标默认保护目标当前值。
- 6.4 已有上游换版对应实现及测试，但未勾选：精确歌曲身份、同来源录音、ISRC 优先，文本/时长仅建议；重复录音、目标已收藏、多个源占同一目标均提示冲突。两组研究 fixture 的正反方向验证通过，14 首对应及 White Love/深愛反向留原正确。手动结构变化的逐曲方案仍待下述设计选择。

### 待用户选择的结构修改语义

已在本轮发起异步澄清，尚未得到答复。`specs/editing/track-editing/spec.md` 的“碟数与位置一致性”允许调整、清空位置或“保留在原发行”，但“修改全部”默认原地修改共享发行。若一部分歌曲保留原发行和原结构，其他歌曲采用新结构，不能在同一发行的 current 中同时保存两套结构。

推荐：这种混合方案为采用新结构的歌曲新建本地发行，原发行和留原歌曲保留原结构；不复制来源身份，并执行同组新增成员确认。备选：原地修改时仅允许调整/清空位置，保留原结构需改走新建专辑，但这需要调整规格中允许留原的范围。未擅自采用其中一项，6.4、6.5 及相应前端流程继续保留未完成。

最终检查通过：

- `go test ./... -timeout=120s`（`/tmp/sonexa-editor-full.log`）
- `go test -race ./internal/library ./internal/store ./internal/app -timeout=180s`（`/tmp/sonexa-editor-race.log`）
- `go vet ./...`（`/tmp/sonexa-editor-vet.log`）
- `/Users/amadeus/.go/bin/wails generate module`（`/tmp/sonexa-editor-bindings.log`）
- `cd frontend && npm test`：8 个文件、40 项通过
- `cd frontend && npm run build`
- `git diff --check`

旧 AdmissionPlan 等 DTO 的泛型绑定警告仍存在，新 EditorSession/EditPreview 构造器已单独验证。尚未实现 SaveEdit、文件草稿原子保存、旧写入口替换和两栏新界面；不能把以上预览测试理解为保存已经完成。第 8 阶段与原生验收保持未勾选。


## 新建专辑与明确迁入预览（2026-10-02）

依据规划提交 448d47a 完成重新打开的 6.2、6.3，以及 6.4，累计 38/53。上文“待用户选择的结构修改语义”仅为历史记录，已由最新规划解决。

- 删除新编辑 DTO 中的 single/all 范围与 ModifyTarget 开关。专辑实际改动一律返回新建专辑确认要求，单首不例外；源列表包括原具体发行全部收藏，无原发行时仅当前歌曲。全选或部分选择均准备新本地发行，原发行不原地解除绑定。
- 精确新上游目标存在时复用，返回目标当前字段并固定保护它们；空收藏目标也能读取当前值。仅清歌曲绑定继续保留发行。成员准入预览独立保留。
- EditMapping.Selected 明确表达迁入，不再用自动对应或列表焦点代替批准。对应建议按身份、录音、ISRC、文本提供；未选择的行保留全部原值。影响结果返回明确迁入 ID、前后身份及歌曲修订，空集合不可提交。
- 本地修改支持逐曲可空碟号/音轨号；上游换版固定为目标清单位置，即使当前歌曲草稿含旧位置也不会覆盖对应位置。校验所选歌曲范围和完整位置，全部重复参与者均有冲突；跨碟同轨号、未知及未选歌曲不误报。目标歌曲身份检查覆盖全库。
- 两组 14/15 首正反向测试改为显式选择，并验证自动建议不迁入、独占曲留原、用户年份保护；新增本地部分/全选、重复位置、跨碟、未知、越界、目标占用及同目标冲突用例。

阶段验证：Go 全量测试、library/store/app race、go vet、Wails 绑定生成、前端 40 项单元测试及构建通过。日志位于 `/tmp/sonexa-editor-selection-tests.log`、`/tmp/sonexa-editor-selection-race.log`、`/tmp/sonexa-editor-selection-bindings.log`。Wails 旧泛型 DTO 的 Not found 警告和 Vite 大包提示仍存在。

以上为后端草稿及影响方案，不是保存实现。6.5 原子保存、6.6/4.6 旧写入口替换审计、第 7 阶段两栏界面及第 8 阶段完整验收仍未完成。旧编辑界面仍在使用旧保存入口，本次未声称新交互已经可供用户端到端使用，未执行原生验收。


## 编辑计划与原子保存（2026-10-02）

完成 6.5，累计 39/53。新增 App `PlanEditSave` / `SaveEdit`，复用已有准入计划签名、15 分钟有效期、独立新增成员批准和幂等结果。普通准入提交明确拒绝编辑计划，不能利用它绕开编辑事务。

- 计划固定字段、明确迁入集合、位置、目标快照、原/目标实体修订及文件动作；读取到互相矛盾的修订时拒绝生成计划。新增发行预先选择 JavaScript 安全整数范围内的随机 ID，但不提前创建实体，使封面能在 SQL 事务前准备到最终发行目录，提交时检查 ID 冲突。
- 同一 SQL 事务重验签名、有效期、源成员集合、歌曲/发行/组/快照/封面修订、实时归组证据、成员批准、目标身份和完整位置占用。只移动所选歌曲，不修改原发行和未选歌曲；新本地目标清除不一致歌曲绑定，复用精确上游目标时保护其当前字段。
- 当前值、角色、身份、有效快照、标识投影、FTS、成员决策及提交结果一起保存；同身份刷新保留未知字段的已有快照，换身份时清理失效事实并重新判定保留值来源。成功后发送统一曲库刷新事件。
- 歌词与封面先准备不可变文件，事务只切换引用，失败清理无引用产物。新本地专辑保留原采用封面时复制为独立目标文件；原采用文件不删除。封面/歌词与字段同时提交，数据库末尾故障可回滚并重试。
- 准入补充草稿专辑名称/署名证据，同时保留上游身份与事实证据；目标已有收藏的实际当前位置也参加影响预览和提交冲突检查。

验证通过：

- `go test ./... -timeout=120s`（`/tmp/sonexa-editor-save-full.log`）
- `go test -race ./internal/library ./internal/store ./internal/app ./internal/covers -timeout=180s`（`/tmp/sonexa-editor-save-race.log`）
- `go vet ./...`
- `/Users/amadeus/.go/bin/wails generate module`（`/tmp/sonexa-editor-save-bindings.log`）
- `cd frontend && npm test`：40 项通过；`npm run build` 通过

新增真实 App 测试覆盖部分迁入、未批准不写入、修订过期、幂等、缺失封面失败、歌词/封面共同保存、事务末尾故障回滚和原封面保留；library 测试覆盖两组 14/15 首正反向实际保存、目标用户值保护、篡改计划、旧准入入口拒绝编辑计划及并发成员变化。

本阶段未替换旧编辑界面或旧写入口，6.6/4.6 与第 7 阶段仍待完成，因此不宣称新流程已经端到端可用。Wails 旧泛型 DTO 警告、Vite 大包提示仍保留，未执行本轮 Playwright 或原生验收，第 8 阶段仍未勾选。


## 统一编辑入口、两栏界面与软件验收（2026-10-02）

完成 4.6、6.6、7.1–7.7、8.1、8.2、8.4、8.5，累计 52/53；仅 8.3 原生验收尚未完成。本轮按用户要求持续推进至实际工具访问阻碍，不归档变更。

- 移除 App 的 ApplyEdits、RematchTrack、ClearProvenance、SetProvenanceManual、UpdateTrackBasic、PlanAlbumRename、ApplyAlbumRename；旧详情辅助写方法改为包内函数。生产创建/迁移调用均经过 library 准入或编辑计划，存储旧构造辅助仅供测试装配使用。新增反射审计，验证旧写绑定不再暴露，伪造保存计划不能提交。
- 新 EditDialog 与 useEditDraft 分离基线、当前草稿、左侧预览及有效绑定快照。来源按注册表排列；版本浏览按来源接口能力显示，清单标明本版本、其他版本或未收藏。迟到搜索/预览不覆盖新选择，重新预览保留手工字段。
- “使用”先确认，再一次应用确定值；未知保留、明确空值清空，可只换绑定。上游封面先读取独立上游预览并暂存，不借当前用户采用作为上游值，也不提前采用。歌曲、角色、专辑类型/结构、歌词及本地封面均进入草稿；演唱者标签与署名独立，保留角色属性。
- 专辑改动只提供新建专辑/取消，进入左右影响页。列表焦点与迁入勾选独立，已选绿字、未选灰字，冲突图标有可访问说明；位置方案改变期间禁用提交。单首、精确目标复用及新增成员取消都保留草稿。未解析确认的绑定文本也触发未保存提示，保存不会静默忽略它。
- 保存/关闭/Esc 统一走未保存决策；正在提交时禁止退出。错误保留输入，成功刷新全库视图。新增成员确认共用 admission store，并按计划类型委托 SaveEdit。
- 准入计划与归组提议改为具体展示 DTO，完整快照及签名不再暴露给前端模型；Wails 生成不再出现旧泛型数组 Not found 警告，构造器测试通过。
- 已合并 16 个 delta 能力至主规格，新增 library/admission 和 library/release-grouping；更新 Purpose、README、架构与核对记录。G01/G02/G03/G06/G08 移除，G11 保留专门署名人工确认缺口，G15 改为旧模型不转换的边界。

验证证据：

- `go test ./... -timeout=120s`：通过（`/tmp/sonexa-editor-ui-full.log`）。
- `go test -race ./internal/library ./internal/store ./internal/app ./internal/covers -timeout=180s`：通过（`/tmp/sonexa-editor-ui-race-final.log`）；`go vet ./...` 通过。
- `npm test`：9 个文件、44 项通过；`npm run build` 通过，仍有 Vite 大包提示。
- Chromium/WebKit 全量静态前端回归 132/132 通过（`/tmp/sonexa-editor-ui-static-final.log`）；随后补严绑定原始输入保护，相关编辑回归 14/14 通过（`/tmp/sonexa-editor-ui-last-e2e.log`）。初轮旧文案失败已更新，构建并行时的三个列表失败在静态前端复跑通过；以上最后结果为准。
- 浏览器截图：`frontend/test-results/editor-preview-and-confirmed-use-dirty-exit-and-cancellation-{chromium,webkit}/editor-desktop.png`，不代表原生截图。
- `openspec validate --specs --strict`：26/26 通过；变更严格校验与 git diff 检查通过。
- `/Users/amadeus/.go/bin/wails build`：darwin/arm64 构建成功（`/tmp/sonexa-editor-native-build-final.log`）。其后绑定文本保护的前端构建及浏览器检查通过，恢复原生验收时需重建最新原生包。

### 原生验收停止原因

调用 Computer Use 选择 `build/bin/sonexa.app` 时，工具明确拒绝：`Computer Use was not approved to use sonexa`。依 openspec-apply-change 的“Error or blocker encountered → report and wait for guidance”停止原生操作；没有改用其他技术绕过访问限制，没有修改或删除用户历史应用数据。

8.3 保持未勾选：原生输入法、窗口尺寸、嵌套弹窗、未保存退出、系统文件选择及歌词/封面离线读取均不能由浏览器替身和构建结果代替。恢复前需允许 Computer Use 访问 Sonexa，并使用隔离的新模型验收数据；本机历史应用目录不应被隐式清空。
