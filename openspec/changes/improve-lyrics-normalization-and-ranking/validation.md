# 本轮实施验证记录

日期：2026-10-21。适用变更：`improve-lyrics-normalization-and-ranking`。
本文记录本轮完整文档、预取/预算/批准和前端集成；不覆盖此前阶段记录或替代真实来源证据。

## 离线结果

| 检查 | 结果 |
| --- | --- |
| `/Users/amadeus/.go/bin/wails generate module` | 通过；按 README 准备 embed 目录，未手写生成模型；生成文件不提交 |
| `go test -count=1 -race ./internal/...` | 全部通过，包括依赖边界、App、公共模型、四来源和存储 |
| `npm --prefix frontend test` | 19 个文件、89 项通过 |
| `npm --prefix frontend run build` | 类型检查与构建通过；主包超过 500 kB 的体积警告保留，不作为失败或本次优化范围 |
| `SONEXA_E2E_PREVIEW=1 npm run test:e2e -- lyrics.spec.ts cover-search.spec.ts`（frontend 下） | Chromium/WebKit 共 28/28 通过，其中歌词 12/12 |
| `openspec validate improve-lyrics-normalization-and-ranking --strict` | 通过；不代表尚未执行的在线/原生任务完成 |

本轮执行范围无测试失败，不需要用基线失败豁免本次结果。未运行全量 Playwright；此前记录的编辑器徽标基线失败不在本次相关 E2E 范围内，不能据此宣称它已修复。

## 主要证据

- `internal/app/lyrics_pipeline_test.go`：通过生产能力装配、mock HTTPS、实际 App 绑定和隔离 Store，覆盖 QQ QRC、QQ legacy 回退、网易云 YRC-only、酷狗 KRC language、酷我两层识别中的整首交替样例。各源候选去重后只预取前三项，正常来源搜索加预取 4 次，酷狗及 QQ legacy 7 次；显式刷新与自动路径文档和分项一致。观察上游交付对象，确认业务未二次规范化，保存文本与预览同版本一致，其他曲目不变。
- `internal/app/kuwo_lyrics_test.go`、酷我 provider 合成测试：启发式诊断与 +15 一致，提取译文不重复计为原文；无命中、声明、歧义及非酷我相同输入由相应测试覆盖。
- `internal/app/lyrics_{budget,manual,pool,prefetch,preview,retention}_test.go` 与 provider 预算测试：冻结名单、并发/同源限制、跳转/重试/回退发送记账、假时钟期限、门禁复查、人工 8 次预算、取消排空、内存拒绝与释放、版本确认和修订冲突。关闭后的迟到成功文档不能恢复会话或修改新目标。
- 前端状态单测：初始响应/事件/一次快照竞态、过期会话/版本拒绝、监听释放、冻结重试查询、全部终态后的自动选择、人工优先和显式展示确认。
- 前端歌词 E2E：145/130/115 跨源排序、未评分 `—`、六种通道状态、未选中时徽标可见、启发式提示、失败与成功通道并存、重排保持焦点、手动获取、长行不溢出、键盘切换、HTML 样式文字不执行、确认前不可采用及详情回读。
- Go/TypeScript 使用 `testdata/lyrics/{ranking,text}.json`；显示投影保留正文空白/普通括号，不把详情文本回写采用文件。

## 浏览器截图（不是原生窗口）

Playwright 生成的截图位于被忽略的 `frontend/test-results/`：

- `lyrics-two-column-lyric-se-5c172-ure-and-applies-to-the-song-{chromium,webkit}/lyrics-search.png`
- `lyrics-local-lyrics-indepe-36975-d-resizable-indented-drawer-{chromium,webkit}/lyrics-drawer.png`

这些截图使用 Wails 浏览器替身，不证明原生输入、文件选择器或实际来源可用。

## 尚未执行

- 9.3：未显式启用真实网络。本轮无实际歌词上游请求，QQ/YRC/KRC/酷我最新在线效果均保持待验证；不得扩大现有 40 次总发送上限，也不因 Top 3 提高授权预算。
- 9.4：尚无本次 Wails 原生窗口截图及人工确认。此前上游/编辑器原生确认不覆盖本次歌词变更。

完成以上两项后，再运行 verify、最终 strict validate；主规格同步与归档另行确认。
