# 4：执行计划

- [x] 1. 阅读截图、生产选择代码与历史样例，区分 Engine 分类和 library 实际准入。
- [x] 2. 获取并核验这 11 个发行的真实身份、整发行曲数、载体/国家，保存可离线重放的金样例及来源说明。
- [x] 3. 添加真实 provider → Engine → library → 提交/Runner 测试，输出候选及最终身份；覆盖空库、已有发行与反事实对照。
- [x] 4. 运行定向测试、race 与必要回归，记录确切选择原因，更新共享 spec-diff 中的测试证据。
- [x] 5. 检查 diff，以中文约定式提交并向用户报告结果及复现命令。

## 验证记录

- 公网仅 2 次请求：一次精确短语 recording 搜索取得原版与伴奏各 11 个发行（共 22 track），一次最小 release MBID 的详情核验。串行、相隔大于 1 秒、无重试或重定向，未查询用户配置/数据库。完整原始 JSON、来源参数和 SHA-256 已保存到 `testdata/golden/responses/musicbrainz/`。
- `go test ./internal/app -run '^TestSekaijuuNoAme' -v -count=1`：通过；详细输出候选身份、分数、总数、国家、载体、曲序及最终结果。三种原始输入 × 每种 22 组候选顺序变化全部一致。
- **空库结论**：11 个原版候选均同组/同 recording，各发行仅一个目标位置；总数均 4、国家均 JP、载体均 CD；没有已有发行优先条件，最后按 `provider + release_ref + match_ref` 升序决定，选中 release `07088ba6-2c40-356a-8d0c-ef72d8a21d93`、track `5e5f141f-cdaa-3346-9f9a-43b572006e9b`。真实 SubmitImport/Runner 写入 imported=1、review=0、skipped=0。该结果不是 API 首条，也不是 track MBID 最小者。
- **详情核验**：CD 第 1 碟第 2 轨，共 4 首；目录号 COZA-457，日期 2010-07-21。搜索原有总数足够，重放导入不请求摘要/详情；这次详情仅用于独立核验。
- **明确反事实**：预置 `e8ab0e85-213e-3a70-81b5-d6244a852945` 内另一首测试曲，真实导入改选此已有发行；仅改末尾发行总数为 5 或只让它为 JP（其余 XW）均改选它。载体显示偏好不改变结果。同发行多个位置或不同 recording 仍复核。
- `go test -race -count=1 ./internal/app ./internal/library ./internal/matching ./internal/acceptance`：全部通过。
- `go vet ./...`、`git diff --check`：通过。
- 本轮只改 Go 测试、金样例和记录，没有生产代码/API/前端变化；不需要生成绑定或重新执行浏览器测试。不修改主 spec，不改变自动选版策略。

## 复现

```sh
go test ./internal/app -run '^TestSekaijuuNoAme' -v -count=1
```

测试离线使用临时 App/数据库和本地 HTTP 重放。它确定相同数据与指定库状态下的选版原因，不把结果冒充用户过去那次操作的完整审计；未读取运行中的数据库，未进行原生窗口验收。
