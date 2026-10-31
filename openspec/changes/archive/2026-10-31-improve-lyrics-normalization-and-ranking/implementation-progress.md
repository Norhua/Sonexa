# 实施阶段记录

## 2026-10-20：公共模型、评分与来源适配

任务进度：20/41。已完成 1.1–1.5、2.1–2.5、3.1–3.3、4.1–4.2、5.1–5.4、6.1。

- 3.4 等待第 7 组预取调度后验证自动/人工共用路径；未勾选。
- 4.3 的统一文档、评分回归、文档与在线摘要已实现，候选卡片摘要仍待第 7/8 组；未勾选。
- 第 7/8 组会话、预算、预览批准和页面尚未实现；Wails 绑定尚未重新生成。
- 本次不是完整功能验收，尚不可将新 DTO 与旧页面视为交付完成。

### 已执行离线检查

- `go test -count=1 -race ./internal/lyrics ./internal/matching ./internal/provider/netease ./internal/provider/qqmusic ./internal/provider/kugou ./internal/provider/kuwo`：通过。
- `go test -count=1 -race ./internal/app -run 'Lyrics|QQ|Kugou|Kuwo|DocumentParsers'`：通过。
- `npm --prefix frontend test -- lyrics-ranking.test.ts`：1 文件 / 1 测试通过，Go/TS 共用排序语料。
- `openspec validate improve-lyrics-normalization-and-ranking --strict`：通过，仅表示规划结构有效。
- `git diff --check`：通过。

测试调整：多通道规范化词标签统一三位毫秒；旧 App legacy fixture 不再用业务错误触发回退，改为格式缺失；酷狗旧 LRC fixture 增加缺失 KRC 阶段。各限制错误另由计数测试覆盖。

未执行全量 internal race、前端全量构建/类型检查/E2E、真实网络和 Wails 原生窗口检查。不能用上述离线通过代替任务 9 的验收。

### 阻塞：任务 6.2 的酷我内嵌翻译来源依据

只读核对了现有本地资料，未新增网络请求、未输出歌词全文或凭据：

- `tmp/lyrimuse/lyrimuse-engine/kuwo.go` 的逐行模型仅有 `time` 和 `lineLyric`，注释说明另用 `adoptBakedTranslation` 启发式拆分；该方式被本变更明确排除，不可直接移植。
- `tmp/lrc-change/歌词上游使用方式调查.md` 同样只列上述两个字段。
- `tmp/lyrics-verification/kuwo-lyric-alt.json` 为 status=200，46 行；data 为 lrclist/simpl/songinfo，行字段仅有 lineLyric/time。
- 本地另两份 `kuwo-lyric*.json` 为 status=301，不能提供成功内嵌格式证据。

当前资料不足以交付“可靠内嵌内容 → available”的来源判据与正向夹具。不得编造类型字段、把同时间/交替行/语言差异当证据，也不得将该要求静默删除。等待用户选择：提供来源样例/文档；授权有限在线核对；或明确修订任务为保持 unknown 并记录限制后继续其他工作。
