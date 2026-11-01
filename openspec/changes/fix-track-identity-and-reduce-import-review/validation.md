# 验证记录（进行中）

变更：fix-track-identity-and-reduce-import-review。当前完成 **44/48**（1.1–9.2）。9.3–9.6 未完成，禁止用下列局部通过代替最终全量通过。

## 本轮入口矩阵实际结果

| 用例 | 新增收藏 | 已存在跳过 | 独立问题 / 来源行 | 错误自动操作 | 搜索调用 |
|---|---:|---:|---:|---:|---:|
| 成对 CSV/TSV，原名/译名/全角/等值时长 + 未匹配反例 | 1 | 1 | 1 / 2 | 0 | 7 |
| you：复核添加合集，再导入特典截断 | 2（含初始特典） | 1 | 0 / 0 | 0 | 1 |
| you：搜索添加合集，再导入特典截断 | 2（含初始特典） | 1 | 0 / 0 | 0 | 2 |
| you：显式合集导入，再导入特典截断 | 2（含初始特典） | 1 | 0 / 0 | 0 | 2 |
| you：无专辑输入，再导入特典截断 | 1（初始特典） | 1 | 1 / 1 | 0 | 2 |

- 测试：`TestSubmitImportPairedCSVTSVIdentityAndProblems`、`TestYouRealTrackIdentitiesAcrossReviewSearchAndImport`，定向普通/race 均通过。
- 成对文件经 SubmitImport→生产 Runner→Store，逐项断言精确 track/release、原文不变；第二次前删除首个终态输入，不依赖该历史行。
- you 使用设计提供的两个真实 track MBID 和共同 Recording/ISRC；发行/组及附加字段为确定性构造 fixture，不冒充完整真实来源响应。检查旧字段、来源快照、图片选择与歌单不变。
- 上表“搜索调用”为离线假 provider 方法调用，不是外网请求；纯比较测试不应伪造入库/问题计数。
- D10 其余证明位于 metadata_text/qualifiers/credit/evidence、problem_equivalence、candidate_pool、selection_evidence、local_existing、problem_recheck、single/multi_input、problem_lease 等既有测试。最终全量执行中已跑到这些包，但完整逐项映射与矩阵汇总尚未完成，9.3 未勾选。

## 已通过的阶段验证

- 8.1：Wails 生成、typecheck、117 前端单测、build、store/app/importer/library 回归、store/app race。
- 8.2–8.4：新增问题/租约/动作双浏览器测试 20 项、候选/准入/封面/实时事件等合计 82 项；旧复核动作 38 项全部通过。修复本地刷新失败被队列覆盖及租约重开事件竞态。
- 8.5：计数、截图、控件与实时更新双浏览器 30 项通过，保存跨批问题截图。日志回归恢复失败手动重试说明后，全量 Go 与五包 race 当时通过（最慢约 223 秒）。
- 最终前端 typecheck、**24 文件 / 120 单测**、build 通过。主 chunk 约 **1,065 kB**，仍有 >500 kB 警告。
- strict 规格与 diff 检查通过；9.6 的全代码一致性核对尚未完成。

## 最终全量运行失败（未掩盖）

### Go

执行 `go test -json ./...`：1796 个 test/subtest 通过，`internal/covers.TestCancelOutOfView` 失败：

```text
TempDir RemoveAll cleanup: unlinkat .../TestCancelOutOfView.../001/tmp: directory not empty
```

检查发现该测试启动 `go pool.Fetch(...)`，测试退出前释放 HTTP handler，但没有等待 visible Fetch 写盘结束。尚未修改/复验该封面测试；不声称已排除生产问题。

后续 `&& go test -json -race ...` **未执行**。第八组末的 race 通过不能代替加入 9.1/9.2 后的最终运行。日志在 `/tmp/sonexa-go-final.json`（临时诊断，不作为持久交付依赖）。

### Playwright

执行构建预览模式的全量 Playwright：**314/320 通过，6 失败**。

- 本次新增来源 Select 导致旧 `list-edges` 与 `provider-capabilities` 各双浏览器定位不唯一。已改按“上游/地区”可访问名称定位，相关两文件 **14/14** 重跑通过；尚未重跑完整 320 项，不报告全量已绿。
- `cover-grid` 双浏览器失败：原断言 `/variantId=32$/`（另有 11/12）不允许 URL 后带 `&searchId=...`，而当前 CoverPreview 为搜索会话携带该参数。本轮未改该封面模块/测试；须确认是否纳入修复。不能删掉 searchId 掩盖断言。

日志在 `/tmp/sonexa-e2e-final.log`；trace/截图在忽略目录 `frontend/test-results/`，后续重跑可能覆盖。

## 验证边界与后续

- 不运行原生 Wails 人工验收或真实网络；浏览器用 Wails 替身，Go 用临时 SQLite/httptest/固定事实。未清理、迁移或读取用户应用数据。
- 数据代际仍 `track-identity-v9`；未同步主规格、未归档。
- 下一步需用户确认是否修复两处封面测试基础设施/断言，再执行 D10 汇总、全量 Go、五包 race、完整浏览套件及一致性核对。9.3–9.6 保持未完成。
