# 6 — 执行计划与验证

## 任务

- [x] 6.1 `internal/matching`：新增 `SplitVersionDescriptor`（括号内的版本段 + 一直延续到结尾的未加括号版本词串），返回曲名主干与版本描述；主干为空或没有版本描述时报告不可拆分。补单元测试。
- [x] 6.2 `internal/provider/musicbrainz`：`buildLuceneQuery` 对可拆分的曲名改为「主干短语必须 + 锚点必须 + 完整短语/版本词可选加分」，不可拆分时保持原查询串。补查询串测试。
- [x] 6.3 用真实 MusicBrainz 响应固化回归用例：完整曲名经整条管线后，目标发行进入候选池且排第一，另一版（long Ver.）不混入。
- [x] 6.4 运行 Go 测试与 vet；更新共享 `spec-diff.md`；中文约定式提交。

## 验证记录

### 根因确认（真实 MusicBrainz，2026-11-13）

- 旧查询 `recording:"星の在り処 Harmonica short Ver." AND (release:… OR artist:…)`：返回 2 条录音（重制版 `13bc46a5`、Evolution 版 `8f51c545`），目标录音 `b919f281` 不在其中。与截图 29 的 6 行候选一致。
- 短曲名查询 `recording:"星の在り処" AND (…)`：返回 22 条，目标录音在其中。与截图 30 一致。
- `recording:"Hoshi No Arika Harmonica Short Version."`（同一录音在另一张发行上的曲目名）：0 条——确认 `recording:` 不索引曲目名。
- 目标录音名为 `星の在り処 (Harmonica Short version)`，其在目标专辑上的曲目名为 `星の在り処 Harmonica short Ver.`。
- 新查询 `+recording:"星の在り処 Harmonica" +(release:… OR artist:…) recording:"星の在り処 Harmonica short Ver." recording:("short" "Ver.")`：返回 6 条，逐字相同的两条仍排最前（100/100），目标录音第三（78）。
- 旁证：`recording:"That Girl \(DJLIU Remix\)"` 在真实上游命中 0 条，放宽形态下带 remix 的录音排在最前——放宽没有丢掉旧形态能查到的东西。

### 通过的检查

- `go test ./...`：全部包通过。
- `go vet ./internal/matching ./internal/provider/...`：通过。
- `gofmt -l internal`：本轮改动的文件无输出（`internal/acceptance/replay_test.go` 的格式差异在本轮之前已存在，未触碰）。
- 新增测试：`internal/matching/version_descriptor_test.go`（拆分的 18 个样例、主干是原曲名前段、不放宽 `CompareTitle`）；`internal/provider/musicbrainz/version_descriptor_test.go`（查询串形态、版本词上限、无版本描述时查询串不变、真实响应回放）。
- 回放用例 `TestFullTitleFindsTargetRecording`：夹具 `testdata/search-version-descriptor.json` 是新查询在真实上游的原始响应；经 `Engine.Run` 后只发 1 次查询，首位候选为目标专辑上的目标录音，曲名/署名/专辑三项精确且非仅供复核。

### 实现范围与边界

- 「哪一段是版本描述」由 `matching.SplitVersionDescriptor` 陈述；放宽只发生在 `internal/provider/musicbrainz` 的查询构造里。没有改 `provider.Query`、变体阶梯、评分权重、阶梯停止条件，业务代码里没有按上游分支。
- 曲名没有可分离的版本描述时查询串逐字不变，既有缓存继续命中；带版本描述的曲名查询串改变，缓存键随之不同，无需清缓存即可生效。
- 本地闸门不变：`CompareTitle` 仍按版本标记集合判定，`long Ver.` 等其他版本照旧被拒。
- 已知取舍：结尾恰好是版本词的普通曲名（如 `Love Live`）也会被拆成主干 `Love` + 版本描述 `Live`。结果集仍是旧形态的超集，完整短语的加分使逐字相同的录音排在最前（实测 `Love Live` 仍为 100 分首位），但必须项变宽后，取回的前 25 条里可能混入更多需要本地闸门筛掉的同主干录音。
- 未做：没有在打包后的应用里对着真实数据库人工复测复核队列第 29 条；没有检查其他上游（iTunes、网易云、QQ 音乐、酷狗）——它们是自由文本检索而非严格短语，不属于本次根因，未改动也未验证。没有运行前端测试（本轮无前端改动）。
