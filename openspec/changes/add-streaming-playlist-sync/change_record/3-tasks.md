# 3 — 执行计划与验证

## 执行计划

- [x] 3.1 完整梳理 Runner、评分/候选筛选、library 选版、去重/活动合并、准入提交和手动复核状态，确认规则位置与行为边界。
- [x] 3.2 扩展批量本地身份读模型，提供精确 release group 存在状态和全 track 的发行引用事实；覆盖来源隔离、空身份、同步专属、删除/换绑及顺序。
- [x] 3.3 在复核队列与改搜结果来源列增加两种独立标记，保持 track 徽章/接受/跳过语义并支持事件刷新，补绑定和前端回归。
- [x] 3.4 将自动选版已有发行偏好扩展到所有本地曲目引用的发行，位置在证据/歧义检查之后；补纯选择、Runner 文件/同步、多候选冲突、准入和提交失效回归。
- [x] 3.5 执行相关后端测试及 race、前端单测/构建和 Chromium/WebKit 定向回归，记录通过、失败和未验证边界。
- [x] 3.6 更新共享 spec-diff 与模块说明，检查差异并中文约定式提交，排除原有 go.mod 改动。

## 状态机排查记录（修改前）

1. 输入建立时 `enqueueImportInputTx/mergeTextInputTx` 做严格文本活动合并；保留每个输入及有效目标。queued 由 `ClaimNextItem` 事务认领为 matching。
2. Runner 先核对目标修订和有效代表输入，再 `ResolveImport(nil)` 做全 track 当前值、有效快照和确认关系判重。可靠重复为 skipped，活动同目标为 merged；不改 in_library。
3. `Engine.Run` 进行共用检索变体、曲名/旁证闸门和评分。`AutomaticCandidates` 只取 HighConf 且非 ReviewOnly，有三字段精确子集则优先该子集，完整人工候选池不删。
4. Runner 检索后再次 `ResolveImport`；唯一自动候选可登记 resolved track，但各有效输入须与候选曲名/专辑、位置/时长兼容，不能用 recording、ISRC 或候选交集合并。
5. 最终分流不是旧 `LadderConfig.Decide`，而是 `library.SelectCandidate`：补必要摘要并解析组证据 → 跨组/不同 recording/曲名版本/同发行多 track 位置歧义检查 → 已有发行子集 → 总曲数降序、已知非世界国家优先、provider/release_ref/match_ref 稳定顺序。
6. **缺口**：已有发行使用 `ReleaseInLibrary`，同步专属发行不会被优先。无需另造状态；修改该偏好范围即可让符合其余条件的候选复用已有发行，省掉新成员确认。
7. 选定后 Runner 第三次 `ResolveImport`，精确 track 已存在但原始输入无法确认时仍复核。准入计划检查所有真实组成员、新映射、有效目标；CommitAdmission 事务重验和投递，plan_stale 重新排队，新成员/冲突进 review，请求失败按退避返回 queued 或达上限 failed。
8. 读取 review/failed 只离线消除可靠重复；手动改搜保护，不自动接受。显式接受仍保留用户所选版本，经过相同准入；忽略/目标撤销、暂停、重启及迟到回调的终态保护不变。

## 验证记录

### 首轮定向验证

- store / library / app 定向测试通过：精确且相互独立的发行/组标记、来源隔离、空身份、原生多来源映射、同步专属、删除清理、顺序及换绑；手动/自动候选快照实时投影、改搜不自动接受；已有发行偏好不绕过低置信、ReviewOnly、三字段精确子集、跨组、不同录音、版本和同发行多位置歧义；多已有发行仍比较总数，删除后的旧计划失效。
- Runner 新增两项测试（含文件/同步/混合三种目标）通过：复用同步专属发行但创建不同歌曲、原有归属不变、目标原子投递、不创建新版本、终态不重做；同发行不同录音仍复核。
- `~/.go/bin/wails generate module`、前端 `npm test`（21 文件/111 项）、`npm run build` 通过。构建仍有既存大 chunk 提示。
- Chromium/WebKit 与后端更广范围/race 在首轮定向之后执行，结果见下；以上定向通过不单独表示全量通过。

### 扩大回归与调试

- `go test -count=1 ./internal/...` 全量通过。
- `go test -count=1 -race ./internal/library ./internal/importer ./internal/app ./internal/store ./internal/matching ./internal/acceptance` 六个相关包全量通过，随后 `go vet ./...` 通过；未声称所有 internal 包均做了 race。
- 首轮浏览器 76 项：72 通过，新增 4 项在 tooltip 断言失败。定位证明测试取到了 Tag 内层 label，title 在外层 Tag；仅修正新测试的定位器，没有修改产品绕过断言。
- 重跑 `npm run test:e2e -- review-release-state.spec.ts review-actions.spec.ts playlist-performance.spec.ts provider-capabilities.spec.ts admission.spec.ts`，Chromium/WebKit **76 项同轮全部通过**，覆盖双标记/本地标记共存、接受/跳过、手搜、事件刷新、草稿、两种主题/窄窗口及原确认取消/失效流程。
- `openspec validate add-streaming-playlist-sync --strict` 与 `git diff --check` 通过；规划基线保持不变。
- 本次不调用真实上游、不修改用户数据库；原生 Wails 手工验收、完整发行包及原剩余平台/在线验收未执行。生成的 Wails 绑定及 dist 不提交。

### 最终复验

- `go test -count=1 ./...` 全量通过，`go vet ./...` 再次通过。
- 最终 `npm test` 21 文件/111 项、`npm run build` 通过；仅有既存大 chunk 警告。
- 使用实际构建产物（`SONEXA_E2E_PREVIEW=1`），运行上述五文件及 `live-updates.spec.ts`、`synced-playlists.spec.ts`，新增普通搜索不展示复核标记的范围测试后，Chromium/WebKit **118 项同轮全部通过**。不是全前端测试目录验收。
- 查看 Chromium 暗色、WebKit 亮色的 960px 截图：来源列两种标记完整可读并换行，沿用候选表横向滚动。截图在 `frontend/test-results/review-release-state-*/review-release-state-{light,dark}.png`，不入 Git。
- 单独复跑 `TestLargePlaylistReadPerformance`（合成 1000 track / 320 review × 10 候选）：离线整理 42.38ms，队列读取 37.50ms，汇总 0.11ms，同步列表 2.74ms，接受候选并刷新 112.71ms。此数据用于检查批量查询未回退到逐候选全库归组，不代表真实网络或原生 UI 性能保证。
- 本轮未改变数据库 schema/代际，无需因本次修改清库；旧复核读取只更新标记，不自动接受。原 change 55/59 不变。
