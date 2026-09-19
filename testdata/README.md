# 测试数据约定

所有正式回归与离线金样例纳入版本控制，不因修复分支合并而删除。

## 目录

```text
testdata/golden/encoding/   编码与表头样例（GB18030 / Shift_JIS / Big5 / UTF-16 等）
testdata/golden/delimiter/  XLSX 单/多工作表样例
testdata/golden/responses/  精简的上游原始响应，供解析器离线重放
```

文本标准化、括号/CV、评分等输入与期望直接维护在 `internal/matching/*_test.go` 的表驱动用例中；不另列尚不存在的 text/scoring 夹具目录。

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
