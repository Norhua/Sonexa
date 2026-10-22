# e7878a99 执行计划

- [x] 1. 核对截图、三上游与 XLRC 结构；记录原始提示、选项和用户选择。
- [x] 2. 修复网易 v1 字段/请求参数及随原文选择配套附加通道，覆盖普通回退。
- [x] 3. 在线构建增加有界、可解释的附加行近邻顺序关联；不重猜本地 XLRC，不丢未匹配文字及真实词时间。
- [x] 4. 三来源在线转换补全语言：保留明确语言、有限文字判断、翻译默认 zh、罗马音依据原文语言；不误改通道存在性/原文副本判定。
- [x] 5. 补回归与本地历史响应离线重放；核对阅读行、完整 XLRC、往返与采用契约；前端区分关联启发式说明。
- [x] 6. 运行 Go/前端相关验证，维护累计 spec-diff/格式说明并中文提交。

## 验证记录

- `go test ./...`、`go vet ./...`：通过。
- `go test -race ./internal/lyricdoc ./internal/lyrics ./internal/provider/netease ./internal/provider/qqmusic ./internal/provider/kugou ./internal/app ./internal/acceptance`：通过。
- `npm --prefix frontend test`：20 文件 / 106 项通过；`npm --prefix frontend run typecheck` 通过。
- `npm --prefix frontend run build`：通过，保留 Vite 大于 500 kB chunk 的体积提示，无构建错误。
- 构建后运行 `SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- tests/e2e/lyrics.spec.ts`：Chromium/WebKit 共 24 项通过，新增近邻关联/语言标签/无独立时间戳/原始文本采用场景。
- `SONEXA_NETEASE_PROBE="$PWD/tmp/upstream-research/ne-lyric-new.json" go test ./internal/provider/netease -run TestLocalNetEaseProbe -v`：离线通过。修字段后的普通构建：翻译 53 关联/0 独立，罗马音 0 关联/53 独立；在线构建两通道均为 53 关联/0 独立；阅读行往返一致。此为已有历史样本，不是截图 Tetra. 的当前在线响应。
- `SONEXA_QQ_PROBE="$PWD/tmp/lyrics-verification-v2/lyric-mid-batch.json" go test ./internal/provider/qqmusic -run TestLocalQQProbe -v`：离线通过，原文/翻译/罗马音均 available，XLRC 24530 bytes、0 warnings，不打印正文。
- 单测包括截图时间差合成回归、±2000/2001 ms、等距/同刻声部/多候选/精确优先、空计时行顺序、真实词与行结束、独立/未知/无时间内容、offset/不可变/seal/诊断上限/4000 行输入；语言覆盖 ja/ko/明确 zh/明确标签优先/中文内短日语引用/混合原文/und 保留/原文副本不升级；三来源及普通回退返回前完成转换。
- App 采用链验证在线文档预取不授予采用权，展示批准后原样保存，回读与 Rows 一致；架构边界新增禁止业务调用 `BuildOnline` 的负例。
- `openspec validate improve-lyrics-normalization-and-ranking --strict`、`git diff --check`：通过。

### 验证过程中修正

- 首轮 Go 回归发现 QQ 与 App 的旧 `[>und]` 固定断言，应随三来源已确认的新默认值改为 `[>zh]`；未修改本地/通用构建器的 und 断言。
- 新 E2E 首跑用了 `SONEXA_E2E_PREVIEW=1` 指向的旧 dist，显示旧「启发式识别」文案；重新构建后全部 24 项通过，不把旧产物通过当作本轮 UI 证据。

### 未执行的验证与边界

没有新增真实上游取词请求，没有 Wails 原生窗口人工验收；不提交本地历史整首歌词/响应。原 change 的 9.3/9.4 不在本轮勾选。近邻与文字识别均为有限启发式，仍可能误配/误判；无法接受的内容不会被删除。更新只影响重新获取并采用的歌词，不自动改写历史保存文件。
