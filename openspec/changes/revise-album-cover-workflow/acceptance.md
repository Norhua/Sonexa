# 验收记录

日期：2026-09-23。代码：`edb175a` 加工作区未提交改动。平台：macOS 27.2（arm64），Go 1.26.7，Node 24.20.0。

## 8.3 全量自动验证

日志保存在本机 `tmp/cover-final-verification/`，不提交。

| 命令 | 结果 |
| --- | --- |
| `go test -count=1 -race ./internal/...` | 12 个包通过 |
| `go vet ./...` | 通过 |
| `~/.go/bin/wails generate module` | 通过；生成产物仍被 Git 忽略 |
| `cd frontend && npm test` | 6 个文件、37 项通过 |
| `cd frontend && npm run build` | 类型检查和 Vite 构建通过；主包约 925 kB，仍有超过 500 kB 告警 |
| `cd frontend && SONEXA_E2E_PREVIEW=1 npm run test:e2e` | Chromium/WebKit 共 102 项通过 |
| `openspec validate revise-album-cover-workflow --strict` | 通过 |
| `git diff --check` | 通过 |

此前全量 Go race 暴露的 `SQLITE_BUSY` 已修复：封面文件登记事务改为先写后读，`internal/store/cover_concurrency_test.go` 在修复前稳定复现、修复后通过。

同步规格时发现：发行组代表选择的“有封面”判据只读旧 `release.cover_path`，而新采用流程不再写该字段，升级后新采用的封面不再影响代表版本。`RepresentativeRelease` 改为同时计入采用记录；`TestRepresentativeReleaseCountsAdoptedCover` 修复前失败、修复后通过。修复后重跑 `go test -count=1 -race ./internal/...`（12 个包）与 `go vet ./...` 均通过；该改动只涉及 Go，前端结果沿用上表。

### 未解决的间歇失败

`image11-refinements.spec.ts:12`（侧栏按钮 Tab 后回车打开新建对话框）在 WebKit 并行运行时偶发失败：回车后对话框未出现。该测试不在本变更修改范围内。

| 代码 | 命令 | 结果 |
| --- | --- | --- |
| 当前工作区 | `npx playwright test tests/e2e/image11-refinements.spec.ts:12 --project=webkit --repeat-each=20` | 6/20 失败 |
| 变更前 `4b17276` | 同上（独立 worktree 构建） | 3/20 失败，失败位置和现象相同 |

结论：既有的 WebKit 键盘激活间歇失败，与封面改动无关；未修改该测试，记为剩余风险。

## 8.4 原生与真实来源验收（部分完成，未勾选）

### 已完成：真实来源与断网（2026-09-23）

在隔离 `HOME` 中使用日常数据库副本和本机设置副本（含 MusicBrainz 联系方式），通过真实 `App.initialize`、`StartCoverSearch`、AssetServer `variantId` 预览及 `AdoptCoverCandidate`/`PickAndAdoptCover` 执行。查询条件与界面预填一致：当前歌集第一首歌的曲名、署名和歌集名。共 99 个真实请求：coverartarchive.org 28、c.y.qq.com 12、itunes.apple.com 2、music.163.com 2、musicbrainz.org 2，其余为图片 CDN。日志在 `tmp/cover-live-acceptance/`（不提交）。

| 歌集（查询） | 来源 | 结果 | 预览（请求规格→实际） | 采用（请求规格→实际与文件） |
| --- | --- | --- | --- | --- |
| CLANNAD ORIGINAL SOUNDTRACK（潮鳴り／折戸伸治） | CAA | 20 张；Front 与 Booklet 分卡，规格 250/500/1200/original | 250→250×223、250→250×221 | 1200→1200×1073，`Front-caa-1200x1073-v9.jpg` |
| | iTunes | 有效空 | — | — |
| | QQ | 失败：`上游返回 qq HTTP 500` | — | — |
| | 163 | 2 张；original 与构造 250/500 | 250→250×250 | original→1600×1600，`Front-163-original-1600x1600-v1.jpg`；列表与详情均显示这次最终采用 |
| 花束 〜the best of doriko feat.初音ミク〜（bouquet） | CAA | 20 张；Front、Back 分卡 | 250→250×249、250→250×194 | — |
| | iTunes | 3 张（`jp:` 地区），规格 3000/1200/600/100 均为构造 | 100→**250×250**（实际尺寸与 URL 数字不同，按解码结果显示） | — |
| | QQ | 失败：`上游返回 qq HTTP 500` | — | — |
| | 163 | 有效空 | — | — |

自动取图（Nostalgia，未采用、未查询过 CAA）：列表请求时 CAA 该 Front 的 250 文件返回 HTTP 500（直接请求复现，另一张 Front 的 250 正常），该规格记为可重试的 error，退到同图 500×435，目录仍为 found；详情取得 1200 档，实际 1125×978（原图不足 1200）。

断网（替换全局 transport 使所有连接失败）：

- 已采用和已下载图片的列表、详情全部照常显示。
- 未查询过的歌集返回 502 并记录目录 error，不记为无图。
- 四来源搜索全部显示各自的网络错误，不显示为“未找到图片”。
- 本地导入（选择器替身返回真实文件）：取消不改版本；无读权限文件报 `permission denied`；伪装 JPG 报格式错误；有效 PNG 采用成功，删除原文件后详情仍显示 1000×1000 副本（`Front-local-original-1000x1000-v603.png`）。

`wails build` 在本机生成 `build/bin/sonexa.app`（darwin/arm64），构建通过。

### 阻塞一：QQ 公开搜索接口不可用

`GET https://c.y.qq.com/soso/fcgi-bin/client_search_cp` 在本次全部查询中返回 HTTP 500 且正文为空：应用搜索、`SONEXA_LIVE_COVERS=1 go test ./internal/covers/sources -run TestLiveCoverSources`（周杰伦，经有限重试后仍失败）和单次直接请求结果相同。QQ 图片地址 `y.qq.com/music/photo_new/...` 仍返回 200。9 月 22 日预检时该搜索接口返回 200。

应用行为符合失败隔离要求：QQ 显示错误和重试，其他来源结果可继续选择和采用。未更换请求头或接口去绕过限制。按设计，真实环境不可用须列为阻塞，不能凭离线 fixture 认定四来源完成。

### 阻塞二：需人工操作的原生项目

浏览器替身和上述 Go 链路不能替代以下原生检查：

1. 系统文件选择器：选择有效图片、取消、选择无权限文件。
2. 原文件移动：导入后移动或删除原图，重启仍显示副本。
3. 详情封面右键菜单、Shift+F10/菜单键、Escape 和焦点恢复（原生 WebView）。
4. 网络搜索大弹窗：打开、预览、分层 Escape、关闭后背景滚动和筛选保持。
5. 断开网络（关闭 Wi-Fi）后打开列表、详情和搜索弹窗。

使用隔离副本启动，避免改动日常曲库：

```sh
HOME="$PWD/tmp/cover-live-acceptance/home" ./build/bin/sonexa.app/Contents/MacOS/sonexa
```

## 8.5 升级、目录变更与回滚演练

所有操作在 `tmp/cover-upgrade-drill/` 的副本上进行（`HOME` 指向副本目录，出网被 HTTP 替身拒绝并计数）。旧版本为 `4b17276`（schema v1）的独立 worktree；新版本走真实 `App.initialize` → `store.Open` → `MigrateLegacy` → AssetServer handler 路径。日常数据库只复制，演练前后 SHA-256 一致（`14182ae9…`）。

### 数据集

| 数据集 | 内容 |
| --- | --- |
| 合成 v1 | 旧代码建库并用旧 `DiskCache.Adopt`/`SetCover` 采用：A（CAA 来源，PNG 内容却是 `.jpg` 名）、B 与 C 共用同一哈希文件、D（iTunes 来源，600×400，清单 complete 且获取于 90 天前）、E（采用文件已被删除）、F（无封面）；另有 90 天前的 iTunes/MusicBrainz 响应缓存，TTL 30 天 |
| 真实副本 | 日常数据库副本：286 首、206 个歌集、206 份完整清单、1407 条 MusicBrainz 缓存；没有已采用封面 |

### 结果

| 步骤 | 观察 | 结论 |
| --- | --- | --- |
| 迁移中断 | 首张旧图采用提交后立即 `SIGKILL`：schema 已到 v4，A 有 selection 与 `albums/release-1/Front-caa-original-700x700-v1.png`，迁移行仍为 pending；`adopted/` 原文件全部保留 | 中断不丢原文件 |
| 重启续迁 | A 标 complete 且 revision 仍为 1（未重复采用）；B、C 各自迁入自己的歌集目录；D 为 `Front-itunes-original-600x400-v3.jpg`；E 记 error 并在每次启动重试，显示无图；AssetServer 返回的字节与旧文件 SHA-256 一致 | 无丢图、共享旧文件分别迁移、按实际内容定扩展名，来源按旧 URL 恢复，无法识别者为 unknown |
| 日期 | 90 天前的 iTunes 缓存仍命中、`ttl_seconds` 全为 0；D 清单保持 complete；90 天前的 MusicBrainz 缓存按 30 天 TTL 未命中 | 无 iTunes 日期失效，通用 TTL 保留 |
| 收藏 | 合成库 7 首/6 歌集不变；真实副本升级前后曲目、歌集、发行组、署名、艺术家、清单、导入记录计数及曲目/歌集行摘要完全相同 | 无额外收藏或歌集 |
| 出网 | 各阶段 HTTP 替身计数均为 0（副本无联系方式，出网 gate 关闭） | 升级不出网 |
| 改目录（被拒） | E 的迁移未完成时保存新目录：返回“仍有 1 张旧封面未迁移”；设置未写入，旧目录继续提供全部图片 | 失败不提交半完成设置 |
| 修复后改目录 | 为 E 本地导入新图、重启后 E 标 complete；再次保存同一目标目录成功（上次残留副本经哈希校验后复用）；隐藏旧目录后重启，5 个歌集全部从新目录读出相同字节 | 采用文件不失联 |
| 旧版本打开新库 | 真实副本与合成库均返回“数据库版本高于当前程序支持的版本（数据库 v4，程序支持 v1）”，schema 仍为 v4 | 旧版本拒绝且不改库 |
| 备份回滚 | 恢复升级前的数据库和封面目录副本后，旧版本检查输出与升级前逐行一致；真实副本计数与摘要一致 | 回滚可用 |

演练工具保存在 `tmp/cover-upgrade-drill/harness/`（不提交）。

### 演练发现

- 升级前旧文件已缺失的歌集会一直阻止更改封面目录，直到用户为该歌集重新采用封面**并重启**；提示没有指出是哪个歌集。
- 改目录失败时，已复制到目标目录的文件不会清理，下次重试会校验复用。
- 回滚后，升级期间新采用的封面不会出现在旧版本中（符合设计的迁移计划第 7 条）。

## 9.3 证据汇总与交付说明

本记录接续 [implementation-modal-checkpoint.md](implementation-modal-checkpoint.md)；此前检查点中的暂停原因（菜单测试替身、`SQLITE_BUSY`）均已在上文处理。

### 规格与证据

Go 测试名均在 `internal/` 下；“E2E”指 `frontend/tests/e2e/`，“单元”指 `frontend/tests/unit/cover-search.test.ts`；“真实”指 8.4 的真实来源运行，“演练”指 8.5。

| 能力 · 需求 | 自动化证据 | 真实/演练/原生 |
| --- | --- | --- |
| cover-art · 来源与传输 | `TestProductionCoverHandlerQualityCacheAndGate`、`TestResolverNoMBAndSelectedOverride`、`TestPrefetchCancellationDoesNotCancelHandlerConsumer` | 真实：未预取的 Nostalgia 直接请求取图；演练：无 MB 身份不出网 |
| cover-art · 自动尝试顺序 | `TestResolverLocalThresholds`、`TestResolverMissingVariantsAndMedium`、`TestResolverUsageSharesIndexNotQuality`、`TestTetraCoverHandlerReplaysMediumOnlyIndex`、`TestResolverGroupFallbackPersistsActualRelease` | 真实：250 故障退到同图 500，详情取 1200 档 |
| cover-art · 手动搜索与采用 | `TestResolverAdopted800WinsOverAutomatic1200AndMissingFails`、`TestCoverAdoptionFallsBackWithinImageAndUpdatesOnlyTarget` | 真实：CAA、163 采用，最终选择同时用于列表和详情 |
| cover-art · 并发与缓存 | `TestSharedConsumerCancellationDoesNotCancelOther`、`TestResizeTracksRunningTransfers`、`TestNestedDirectoryDoesNotHoldTransferSlot`、`TestPoolCloseCancelsAndDrains`、`TestCoverPreviewAdoptionAndAutomaticImagesSharePool` | — |
| cover-art · 图片状态 | `TestResolverNegativeMemoryAndErrors`、`TestFallbackRetainsErrorsUnlessImageSucceeds`、`TestTargetOutcomeRevisionAndLegacyPathRejection`；E2E `image11-refinements` 懒加载与迟到结果两项 | 真实断网：未查询歌集记 error 而非无图 |
| cover-art · CAA 查询记忆与规格目录 | `TestCoverCatalogStateSurvivesRestart`、`TestResolverCatalogReopensAndRefreshFailurePreservesSnapshot`、`TestCoverCatalogPreservesImagesAndVariants`、`TestCoverCatalogEmptyInvalidAndRealMedium` | — |
| cover-search · 封面右键入口 | E2E `image11-refinements`（旧图标移除与键盘菜单）、`cover-search`（nil 区域键盘导入、无歌集禁用） | 原生：待人工验收 |
| cover-search · 系统文件选择与导入 | `TestPickAndAdoptCoverUsesSystemPickerAndCopiesWithoutNetwork`、`TestPickerErrorPropagates` | 真实文件：取消、无权限、伪装文件、删除原图；原生对话框待验收 |
| cover-search · 网络搜索大弹窗 | E2E `cover-search`（弹窗、分层 Escape、条件与地区、返回筛选）、`table-refinements`（返回取消会话）；`TestCoverSearchProgressAndAuthorization`（无 MB 绑定的本地歌集） | 原生：待人工验收 |
| cover-search · 四来源结果与失败隔离 | `TestCoverSearchEmptyAndErrorsStayDistinct`、`TestCoverSearchCapsCardsAndRetryKeepsOtherAuthorization`、`TestWebSourceFixturesAndErrors`、`TestCAASourceBoundsReleasesAndPreservesImages`、`TestITunesSourceCountryVariantsAndImageIdentity`；E2E `cover-grid` 空与失败 | 真实：QQ 失败时 CAA/163 结果可用；断网时四来源均为错误 |
| cover-search · 图片预览与规格标签 | E2E `cover-grid`（image12 徽标、最小档预览、实际尺寸、Front/Back 分卡） | 真实：iTunes 100 档实为 250×250，CAA Front/Back/Booklet 分卡 |
| cover-search · 显式采用与会话隔离 | `TestCoverAdoptionFailuresKeepSelectionAndNeverCrossImages`、`TestCoverAdoptionConcurrentRevisionAndDeletedTarget`、`TestCoverSearchCancelledDoesNotEmitOrReopen`；单元三项；E2E `cover-updates` | — |
| release-catalog · 清单有效期 | `TestITunesAlbumUsesStoredStateRegardlessOfAge`、`TestForceTracklistBypassesExistingResponse`、`TestResponsesHaveNoExpiryAndExplicitRefreshBypassesCache` | 演练：90 天前的完整 iTunes 清单保持 complete |
| release-catalog · 共享封面 | `TestCoverDTOsShareReleaseAndRevisionWithoutChangingCandidateIdentity`、`TestRepresentativeReleaseCountsAdoptedCover`；E2E `cover-updates`（同歌集更新、其他歌集不变） | 真实：MB 歌集采用 163 图，文件来源为 163 |
| app-shell · 详情抽屉 | E2E `table-refinements`（迟到详情、返回搜索）、`image11-refinements`（封面布局与菜单） | 原生：待人工验收 |
| data-store · 数据位置与迁移 | `TestCoverSettingsMovePreservesSelectionAndFailedSave`、`TestLibraryCacheClearTTLAndMoveRoot`、`TestCoverMigrationFailureRollsBackAllTables` | 演练：改目录被拒与成功、旧目录隐藏后仍可读 |
| data-store · 响应缓存 | `TestITunesCacheMigrationRemovesOnlyItsExpiry`、`TestResponsesHaveNoExpiryAndExplicitRefreshBypassesCache` | 演练：iTunes 缓存命中，MusicBrainz 按 TTL 过期 |
| data-store · 删除与清理范围 | `TestLibraryCacheClearTTLAndMoveRoot`、`TestProductionCoverHandlerQualityCacheAndGate`（清缓存后按目录补下载） | — |
| data-store · 专辑图片目录与文件命名 | `TestAlbumFilesNamesAndIsolation`、`TestAlbumFilesRejectEscapes`、`TestInspectImageFormatsAndLimits` | 真实与演练的落盘文件名 |
| data-store · 历史封面兼容与原子采用 | `TestLegacyCoverMigrationResumesWithoutDeletingOldFiles`、`TestCoverMigrationPreservesLegacyRelease`、`TestLibraryImagesAdoptionAtomicAndRepeatable`、`TestAlbumFilesBadIdentityAndUnwritableDirectory`、`TestSaveCoverFileRollbackRestoresVariantAfterFileConflict` | 演练：`SIGKILL` 中断后续迁 |
| data-store · 未收藏候选的封面数据 | `TestLibraryImagesReuseAfterCandidateAdmission`、`TestManualPreviewNeverBecomesAutomaticAlbumImage` | — |
| settings · 导出与缓存设置 | `TestLibraryCacheCapacityAppliesImmediately`、`TestOldITunesTTLSettingIsIgnoredAndRemovedOnSave`、`TestResizeTracksRunningTransfers` | — |
| settings · 主题与来源说明 | `TestAboutKeepsAttributionWithoutITunesExpiryClaims` | — |
| itunes · 缓存与来源说明 | `TestResponsesHaveNoExpiryAndExplicitRefreshBypassesCache`、`TestAboutKeepsAttributionWithoutITunesExpiryClaims`、`TestITunesAlbumUsesStoredStateRegardlessOfAge` | 演练：旧 iTunes 数据跨期仍可用 |
| provider-registry · 接口与注册表 | `TestCoverSourceRegistryDoesNotExtendMetadataProviders` | — |
| provider-registry · 后端请求与共享限流 | `TestCoverOutboundGateAcrossBindingsAndAssets`、`TestCoverSearchRechecksGateAfterStart`、`TestCoverRedirectRechecksOutboundGate`、`TestRequestGateKeepsCacheAndChecksHeadAndQueuedRequests`、`TestCoverSearchSharesMetadataQuotaButCAADirectoryDoesNot`、`TestWebSourceQuotasAreIndependentAndSharedAcrossInstances`、`TestWebSourceRetryAfterAndCancellation`；E2E `cover-search`（出网禁用） | — |

以上测试均包含在 8.3 的全量运行中。

### QQ / 163 可用性

- 163：公开专辑搜索与 `picUrl` 在 2026-09-22 预检和 2026-09-23 验收中均可用；原图 1600×1600、500 档均能解码。部分图片地址以 `.jpg` 结尾而内容为 PNG，按实际解码结果保存。
- QQ：2026-09-22 预检可用；2026-09-23 搜索接口持续返回 HTTP 500，图片地址仍可访问。按用户决定暂视为临时故障，待复测（G14）。

两者都是非官方公开接口，可能随时变更或限制；应用不使用登录、代理或伪装请求头。

### 升级与回滚说明

1. 升级前关闭应用，复制整个 `~/Library/Application Support/Sonexa/`（数据库、封面目录、settings.json）。
2. 首次启动自动迁移到 v4，并把旧 `covers/adopted/` 中的采用图片复制到 `covers/albums/release-<id>/`；旧文件保留。迁移中断会在下次启动继续。
3. 旧文件已缺失的歌集记为迁移失败并在每次启动重试；重新采用封面并重启后解除，期间不能更改封面目录（G15）。
4. 回滚：旧版本会拒绝打开 v4 数据库。关闭应用，用第 1 步的备份整体替换后再运行旧版本；升级后新采用的封面不会出现在旧版本中。

### 剩余风险

- QQ 搜索接口不可用，四来源中的 QQ 尚未通过真实验收（G14）。
- 原生文件选择器、原生 WebView 中的右键与键盘、搜索弹窗和真实断网尚未人工验收（G14）。
- 旧文件缺失时阻止目录变更、失败的目录变更留下副本（G15）；列表封面无视口优先调度（G05）。
- WebKit 侧栏键盘测试的既有间歇失败；前端主包超过 500 kB 的构建告警。
- CAA 个别缩略图偶发 5xx，按规则退到同图其他档位并在之后重试。
- `prompt/start-prompt.md` §3.4 与 `prompt/features.md` F7 仍是初始需求材料中的 v1 封面策略，现行行为以主规格为准。
- `store.SetCover`/`SetReleaseCover` 及旧 `DiskCache` 采用路径已无业务调用，仅剩测试；前端行数据仍带未使用的 `adoptedCoverPath`。
- 删除 iTunes 期限说明不代表已对第三方服务条款作出合规结论。
