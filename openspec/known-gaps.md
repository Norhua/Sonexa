# v1 已知实现缺口与后续验收

## 状态与边界

本表承接 [v1-deviations.md](../change_record/v1-deviations.md) 的 19.6、19.10、20.8、21.3–21.4 及后续限制。2026-09-19 的文档同步静态核对了关键生产路径；本次不修业务代码，不以测试全绿或任务勾选消除缺口。

用户明确允许「同步后归档，保留已知缺口」。主规格中的相关 SHALL 是仍应满足的目标，不是已完成的承诺；各受影响规格的 Implementation status 指向本表。这里的修复建议不是另一个已完成任务清单。

## G01 — 用户锁与歌集来源读取（高风险，未修复）

**影响**：library/track-library、editing/track-editing、后台补全。

- 当前 `internal/store/enrich.go:18` 的 `WriteDetailColumns` 直接更新曲名、署名及共享歌集字段，不读取相应锁；`applyDetailMeta` 的局部过滤挡不住该路径。
- `internal/store/create.go:271` 的 `GetMetadata` 只读取曲目 JSON 来源/锁，未提供歌集字段对应的来源/锁，编辑会话可能错误决定默认填充。
- 先前偏差核验已证明锁定标题仍被真实详情应用覆盖；本轮静态确认写入路径未改，不把本次 race 通过当作修复证明。

**后续验收**：经生产详情刷新、导入即时补全、后台补全测试锁定字段不变；同歌集多首共享字段均受歌集锁保护；编辑会话按字段归属读取锁和来源。

## G02 — 灰色行加入缺少歌集与位置（高风险，未修复）

**影响**：library/track-library、library/browse-views、matching/provenance。

- `bindings_albums.go:364` 的 `AddGhostRowToLibrary` 只接受来源、track ref、曲名、署名、时长、recording ref，未带已知发行、碟号和音轨号。
- 虽经统一 `AdmitCandidate`，新曲目仍可能 `release_id = NULL`、补全为 absent；清单依 match_ref 识别拥有不等于真实歌集归属、计数与后台工作集正确。
- `TestAllAdmissionPathsWriteIdenticalTracks` 的完整曲目+歌集相等只覆盖导入/搜索/复核；灰色行只核对共有曲目字段，不能证明四路完整一致。

**后续验收**：从当前清单带入发行与位置，验证真实歌集归属、专辑计数、封面共享及 pending 工作集，不只比标题。

## G03 — 编辑年份、重新匹配与保存事务（高风险，未修复）

**影响**：editing/track-editing、matching/provenance、library/track-library。

- `bindings_edit.go` 展示 year，但保存白名单未映射为歌集 releaseDate，因此年份修改不落盘。
- `RematchTrack` 用候选填字段，`buildEditSession` 仍返回旧匹配身份；普通更换后保存可能不更换溯源，跨源字段可能记旧来源。
- `frontend/src/views/EditDialog.vue:156` 清空溯源立即调用持久化 API，不等保存；随后取消不能恢复。
- `ApplyEdits` 先逐步写字段，再校验标识/共享改名，后续失败不能整体回滚。对话框测试只证明交互，不证明生产事务。

**后续验收**：统一草稿保存候选身份、待清空状态、年份/日期映射；全部校验先行、曲目/歌集/身份原子提交。生产入口覆盖清空后取消、非法标识、未确认共享改名、跨源更换、年份保存。

## G04 — 匹配设置的实际生效范围（未修复/待决策）

**影响**：platform/settings、matching/confidence。

- 高置信无数值门槛是已确定设计，不是缺陷，已修正主规格。
- `ReviewThreshold`（界面默认 75）可保存，但 `internal/matching/` 没有消费它；其用途需要决定后接线或删除，不能编造它影响分流。
- 权重与相似度后端可配置，设置界面没有对应控件，原「可在设置调整」目标未完成。

**后续验收**：明确有效配置契约，再验证配置经保存进入生产 Engine；不以序列化成功代替算法生效。

## G05 — 封面质量、预取与并发边界（未修复）

**影响**：upstream/cover-art、upstream/provider-registry、upstream/musicbrainz。

- 自动抽屉与列表仍用 250px 目标；抽屉 CSS 放到 280px 不等于自动最高质量。手动选择/采用的高档退链是独立能力。
- 搜索页有批量预取，复核无对应调用；没有生产前端 `CancelCovers` 调用/真实可见行跟踪。删除 `CancelCovers([])` 误取消修复不等于实现按视口取消。
- 自动 CAA 索引与图片在同一池/singleflight 流程；手动 `SearchCovers` 索引经独立 Client 键，不受该图片池统一并发控制。

**后续验收**：区分显示尺寸与请求档位，补自动详情取高质量、复核预取及真实滚动取消/降级测试；将所有索引路径纳入明确并发约束。

## G06 — 候选字段与逐字段来源未完整落库（未修复）

**影响**：library/track-library、matching/provenance、歌集模型。

- `internal/store/admit.go` 未把 Candidate.Year 写入歌集日期，以 ArtistName 同时填歌集署名；候选没有独立的歌集署名字段。
- 初次入库未逐字段记录 provider 来源；不用 import 补缺是已实现原则，不等于候选全部字段均无损保存。

**后续验收**：明确候选曲目/发行字段契约，补年份日期、独立歌集署名和逐字段来源映射；包含只给 Year、不带 FirstReleaseDate 的样例。

## G07 — 导出深度与大规模检索（未修复/未验收）

**影响**：library/export、library/search、platform/app-shell。

- `frontend/src/App.vue` 导出固定 `depth: 'full'`；后端支持两种深度，但前端无选择入口。
- 无生产分页/虚拟滚动与大曲库性能基准；`internal/store/library.go:214` 的快速 FTS 仅取最多 2000 个 id，界面没有完整截断或分页契约。
- 三栏页只有名单查找，没有独立曲目 FTS 输入。这是当前已记录界面边界，不自行添加新布局或宣称输入已覆盖两者。

**后续验收**：补导出字段深度入口；明确大结果分页/截断语义，联测排序、键盘锚点与文字拖选；如新增三栏曲目搜索先确定布局。

## G08 — 歌集配套编辑与署名成员（未修复）

**影响**：library/artists、library/release-catalog、library/browse-views、editing/track-editing。

- release_group.release_count 有列、读写原语与条件展示，但没有主动获取、详情字段或手动编辑入口，`SetReleaseGroupCount` 无生产调用方。
- `release_credit` 有模型与原语，`SetReleaseCredits` 无生产调用；不能声称歌集署名已可点击跳转。
- 没有独立歌集编辑入口。现有编辑字段为：曲名、曲目署名、碟号、音轨号、时长、风格、注释；歌集名、年份、厂牌、目录号、载体、国家、类型。
- 歌集艺术家、碟数、总音轨数、封面路径缺少编辑控件。碟数/总数的后端分支不代表前端已可编辑；详情重取封面也不等于手填路径。
- 类型归一为 album/ep/single/broadcast/other，未知类型 other、缺失留空；已有支持不掩盖上述入口缺口。

**后续验收**：为各能力补真实调用链，再测试共享歌集下多首曲目的显示与影响范围。未有数据的版本数不虚构显示。

## G09 — 出网禁用入口与统一错误提示（未修复）

**影响**：platform/app-shell、platform/settings、upstream/cover-art。

- 后端有出网前置条件，但抽屉重取封面按钮仍未按 outboundAllowed 禁用并提供设置跳转。
- 搜索、复核等页面仍有局部错误提示，没有全部统一到全局提示位置。
- 把按钮改为行内图标不代表禁用态已接线。

**后续验收**：在联系方式为空、网络失败时覆盖所有入口与设置跳转，核对错误去重和显示位置。

## G10 — iTunes 已收藏元数据生命周期（未修复/需明确条款边界）

**影响**：platform/data-store、upstream/itunes。

原始响应缓存与清单按 TTL 处理，但已入库 track/release 元数据无统一过期清理/重取。主规格继续保留短期留存目标，不以缓存测试宣称全部 iTunes 元数据合规。本次没有重新研究外部条款，未来需同时核验适用条款与已收藏元数据边界，再设计生命周期。

**后续验收**：过期后的缓存、清单、已收藏字段分别验证；不误删用户自写字段、来源锁与收藏身份。

## G11 — 不确定署名与无详情曲目的成员接线（未修复）

**影响**：matching/text-processing、library/artists。

- `MarkTrackForReview` 只写 enrich_error，`TracksNeedingReview` 未接入复核页面，成功补全的 SetEnrichState 又可能清掉该标记。
- 无详情的手动/无需匹配曲目初次创建没有建立署名成员，完整原串可搜索不等于已形成可点击艺术家关系。

**后续验收**：独立持久化不确定标记，提供实际复核入口并验证成功补全不吞标记；补无详情入库的成员与 FTS 联动，继续遵守完整 CV 身份和不猜演唱规则。

## 保留观察，不冒充已确定修复计划

- 候选快照仍兼容 `[]ScoredCandidate`、`[]CandidateRow` 和简化本地候选三种形状；重载保真已修，不代表序列化模型已经统一。
- `SearchUpstream` 后端仍有多 provider 并发/事件形状，前端每次只传一个；没有为了同步单选规格顺手改 API。
- 主题以 localStorage 同步启动，保存设置时回写当前值；未建立另一套跨存储冲突协议。不要虚构 settings.json 为唯一真源或取消设置会回滚主题。
- 补全状态当前只显示聚合进度，无逐曲目状态列；不因记录状态机而恢复已删除的状态列。
- `scope=playlist` 后端继续兼容，界面已合为当前列表；不需为文档合并删兼容 API。
- 旧候选快照不自动重走新闸门，旧污染开发库无自动迁移或修复；不因归档清理用户数据。
- 补全工作器与详情应用仍在 main 包；整体写入不保证跨所有步骤的原子事务。
- 原生 Wails 的红绿灯/拖窗口、输入法、系统缩放、文件对话框与真实规模仍需单独验收；WebKit 自动化只覆盖 Wails 桩件中的浏览器行为。

## 已关闭的历史缺口

偏差 21.3 A 的非首曲目标错配/清单丢失已由 23.1 修复；21.3 C 的署名推断 vocal 已由 24.3 修复；25.1 修复旧请求覆盖与浅色加载闪黑；27.4 修复 display:none 阻止 lazy 图片请求。对应回归本次重新通过，不将其余 G01–G11 连带标为完成。
