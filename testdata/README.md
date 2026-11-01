# 测试数据约定

所有正式回归与离线金样例纳入版本控制，不因修复分支合并而删除。

## 目录

```text
testdata/golden/encoding/   编码与表头样例（GB18030 / Shift_JIS / Big5 / UTF-16 等）
testdata/golden/delimiter/  XLSX 单/多工作表样例
testdata/golden/responses/  精简的上游原始响应，供解析器离线重放
testdata/track-identity/    成对 CSV/TSV 异写、等值时长及未匹配原始行
```

文本标准化、括号/CV、评分等输入与期望直接维护在 `internal/matching/*_test.go` 的表驱动用例中；不另列尚不存在的 text/scoring 夹具目录。

`internal/app/paired_import_test.go` 经真实 SubmitImport→Runner→Store 提交 `original.csv` 和 `aliases.tsv`；固定候选事实离线返回，身份检查仍由 MusicBrainz 适配器提供。断言精确 track/release、新增 1、已存在跳过 1、无结果两行共享 1 问题及原文不变；第二次提交前删除首个终态输入，证明不依赖该历史行。搜索请求计数 7 来自当前查询变体，不包含真实网络。此前代码内 Runner fixture 不冒充成对文件的入口验收。

`you-identities.json` 固定设计提供的两个真实 track MBID、共同 Recording/ISRC 与名称。`you_identity_matrix_test.go` 的发行/组 ID、附加总数/时长等沿用确定性构造 fixture，不冒充完整真实发行响应；它测试真实身份可并存、显式目标不被拥有偏好替代、特典截断跳过、无专辑复核，以及旧字段/快照/本地图片选择/歌单不变。四个入口场景的固定查询次数为 1/2/2/2，全部离线。

## 测试分层

- `internal/app/*_test.go`：与应用包同包，调用生产绑定、详情应用及后台补全入口，使用隔离 SQLite 与本地 HTTP 桩件。
- `internal/acceptance/*_test.go`：跨领域验收，包括 CSV 的实际匹配分流。
- `internal/<领域>/*_test.go`：各包的原语、解析器、限流和队列回归。
- `frontend/tests/unit/`、`frontend/tests/e2e/`：前端逻辑与双浏览器真实组件/Wails 桩件回归。

Go 测试的工作目录为所属包目录；应用包读取共享夹具使用 `../../testdata/`，不改变进程工作目录或复制一份夹具。

## 约定

- 金样例用例名表明输入的具体行为，不将精简或构造响应冒充完整真实上游数据。
- `responses/` 包含原始响应的离线样本或有明确说明的精简版本，不依赖实时服务或开发者本地缓存。
- 用 `t.TempDir()` 隔离数据库与缓存；测试不访问用户真实数据，不启动原生应用。
- 修改解析或接线后保留并执行既有回归，不能仅因移动目录使测试不再被发现。
- 本地手动调试程序放被忽略且不被 Go 递归发现的 `tmp/_scratch/`，不是正式回归的一部分。
