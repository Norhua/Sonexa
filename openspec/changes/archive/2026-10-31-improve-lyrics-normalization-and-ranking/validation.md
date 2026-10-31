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

本轮离线执行范围无测试失败，不需要用基线失败豁免本次结果。未运行全量 Playwright；此前记录的编辑器徽标基线失败不在本次相关 E2E 范围内，不能据此宣称它已修复。在线部分失败见下文。

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

## 真实网络：2026-10-21 授权单轮

用户明确同意继续有界网络验证及原生核对。本轮先将 `TestLiveSources` 从首份成功即返回迁移到实际 App 的 `SearchLyrics → StartLyricsPrefetch → LyricsSearchSnapshot/FetchLyrics`，保留总计 40 次实际发送、每源 60 秒/16 次、每候选 40 秒、QQ 30 次/分钟及限制即停；没有新增关键词、补取第 4 项或重开预算。同步补齐酷我 openapi 的业务码观察。修改后定向离线 race 回归通过：`LiveLyricsRound|LiveAudit|LiveEntries|LyricsFourSourceAppPipeline`。

只执行一次：

```sh
SONEXA_LYRICS_LIVE=1 SONEXA_LYRICS_SOURCE= \
SONEXA_LYRICS_TITLE='君の知らない物語' SONEXA_LYRICS_ARTIST='supercell' \
SONEXA_LYRICS_ALBUM= SONEXA_LYRICS_DURATION_MS=0 \
go test -v ./internal/app -run '^TestLiveSources$' -count=1 -timeout=5m
```

日志：本机临时文件 `/tmp/sonexa-lyrics-live-20261021.log`。仅包含身份元数据、状态、分项、大小和请求摘要，没有歌词全文、上游响应体或下载凭据。测试使用隔离 Store，未采用任何真实歌词到用户收藏。

### 来源结果

| 来源 | 准入/预取/可用文档 | 实际发送 | 结果与限制 |
| --- | --- | --- | --- |
| 网易云 | 5 / 3 / 3 | 4 | v1 bundle 成功交付 LRC，翻译与罗马音均 available；三项逐字均 unavailable，不能据此证明 YRC 可用 |
| QQ | 5 / 3 / 3 | 4 | 普通原文与翻译可用；三项 wordTiming 与罗马音均 error，保留普通文档，不把部分成功记为 QRC/罗马音通过 |
| 酷狗 | 8 / 3 / 3 | 7 | 三项 KRC 逐字可用，无 LRC 回退；language 罗马音/翻译按实际内容部分提供 |
| 酷我 | 1 / 1 / 0 | 3 | openapi 无可用歌词，单次 H5 回退后仍为 no_lyrics；partial，不是业务限流或解析错误，未取得启发式效果证据 |

累计 **18/40 次**，约 17.7 秒；所有已发送响应均 HTTP 200，QQ 请求块 code=0、网易云 code=200、酷我 code/status=200，未观察到 HTTP/业务限制。QQ 同源发送约 2 秒间隔，其他同源约 1 秒。因酷我无可用文档，整体 `TestLiveSources` **FAIL**；没有追加请求或改查询重跑。前三来源子测试 PASS 只表示取得可用文档，不代表所有可选格式成功。

### 内容与评分摘要

本轮未指定目标专辑及曲长，因此所有候选 M=75（曲名 55、艺术家 20）；未知目标曲长不扣时间越界分，也不验证长短版本是否适合某一真实收藏。不能把 101 秒合辑片段的高分当作完整曲目匹配保证。下列 `P` 均为 0。

| 来源/匹配序号 | 候选时长 ms | 格式 | 合并字节 | 保留字节 | M/S/W/T/R → 综合分 | 通道补充 |
| --- | ---: | --- | ---: | ---: | --- | --- |
| 网易云 1 | 339573 | LRC | 6845 | 53148 | 75/9/0/15/5 → 104 | 原文 partial；翻译/罗马音 synced；2 条警告 |
| 网易云 2 | 342169 | LRC | 6278 | 46202 | 75/9/0/15/5 → 104 | 原文 partial；翻译/罗马音 synced；2 条警告 |
| 网易云 3 | 341124 | LRC | 7371 | 52446 | 75/9/0/15/5 → 104 | 原文 partial；翻译/罗马音 synced；2 条警告 |
| QQ 1 | 101000 | LRC | 1853 | 17228 | 75/10/0/15/0 → 100 | 翻译 partial，罗马音/逐字失败；3 条警告 |
| QQ 2 | 341000 | LRC | 4625 | 40114 | 75/10/0/15/0 → 100 | 翻译 synced，罗马音/逐字失败；2 条警告 |
| QQ 3 | 101000 | LRC | 1809 | 18544 | 75/9/0/15/0 → 99 | 原文/翻译 partial，罗马音/逐字失败；3 条警告 |
| 酷狗 1 | 339000 | 增强型 LRC | 28194 | 153887 | 75/10/20/0/5 → 110 | 翻译 unavailable；罗马音 embedded/synced |
| 酷狗 2 | 101000 | 增强型 LRC | 6663 | 39776 | 75/10/20/15/5 → 125 | 翻译与罗马音均 embedded/synced |
| 酷狗 3 | 340000 | 增强型 LRC | 18358 | 99511 | 75/10/20/15/0 → 120 | 翻译 embedded/synced；罗马音 unavailable |
| 酷我 1 | 101000 | 无文档 | — | — | M=75，综合分为空 | 暂无歌词，无翻译识别依据 |

## 暂停与后续

- 9.3 保持未勾选：这一轮的有界 Top 3 请求及部分格式取得了真实证据，但 QQ QRC/罗马音失败原因未定位，网易云 YRC 和酷我逐行/启发式尚未验证。不能以离线合成通过覆盖实际失败，也不能自行再发请求查样例。
- 9.4 保持未勾选：网络测试失败后按工作流暂停，未启动原生窗口，未捕获本次原生截图或取得人工确认。此前上游/编辑器原生确认不覆盖本次歌词变更。

下一步需用户决定：先离线排查 QQ 解析，或另行明确授权指定样例的新网络验证；原生核对也须与网络预算分开，不自动借原生搜索扩大本轮请求。完成后再运行 verify、最终 strict validate；主规格同步与归档另行确认。
