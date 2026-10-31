# 歌词附加通道 10 毫秒精度对齐：任务

起始提交：`ff41f08c292c55a1846b3380b7d693b13463699c`。需求见 [ff41f08c-why.md](ff41f08c-why.md)，累计差异见 [spec-diff.md](spec-diff.md)。

- [x] 1. 在公共歌词构建阶段实现 offset 后的 ±10 ms 唯一、无交叉关联，保留精确匹配和歧义保护，不移动原文/词时间；同步通道结构、预览、序列化文本及有界诊断。
- [x] 2. 将上一轮精度差现状测试改为修复回归，覆盖 3/5/6 ms、正负 10 ms 边界、11 ms 不匹配、精确优先、同刻/多候选/竞争/顺序保护、空行/未知通道、offset、词时间和输入不可变、再次构建不漂移。
- [x] 3. 增加真实 QQ 适配器离线取词和 App 预取→预览批准→采用回读回归，验证来源交付时已完成对齐、业务不重建、保存全部通道且请求数不增加。
- [x] 4. 区分时间对齐与翻译识别诊断的卡片说明，扩展前端单测和双浏览器 E2E，保证预览同排、切换不再处理或改写时间。
- [x] 5. 执行相关 Go/race、前端单测/构建、双浏览器 E2E、架构边界及文档差异检查；更新累计规则和实际验证结论，提交中文约定式 commit。

## 验证记录

- `go test ./internal/lyricdoc`：通过，覆盖 ±10/11 ms、3/5/6 ms、冲突、空计时行顺序保护、未知/无时间通道、真实词/行结束、输入不可变、诊断篡改、正负 offset、重建/保存不漂移、4000 行及诊断超限。
- `go test -race ./internal/lyricdoc ./internal/lyrics ./internal/provider/qqmusic ./internal/app -run 'Precision|Alignment|LyricsFourSourceAppPipeline|ServiceAndScore' -count=1`：通过，QQ 直接交付及 App 预取/采用管线均使用对齐文本，无额外请求；业务评分不改写文档，也不因对齐诊断加分。
- `go test -count=1 -race ./internal/...`：全量通过，包含架构依赖检查；最终简化构建器返回值后再次全量执行通过。
- `npm --prefix frontend test`：19 文件、93/93 通过；`npm --prefix frontend run typecheck`、`npm --prefix frontend run build` 通过。保留已有 Vite 产物超过 500 kB 提示。
- `SONEXA_E2E_PREVIEW=1 npm --prefix frontend run test:e2e -- lyrics.spec.ts provider-capabilities.spec.ts`：最终 Chromium/WebKit 共 22/22 通过。新增场景验证 42.743/47.856 原文、译文同排，罗马音切换、原始文本和采用使用同一规范化时间。
- 过程中原有 WebKit 键盘场景在先鼠标选择当前值、立即重开并键盘选项时失败（焦点可能被 Select 延迟 blur 重置）；移除多余同值鼠标操作，将原有选项断言合并到键盘打开阶段，并显式断言展开/活动选项，未弱化键盘选择行为。该场景 WebKit 重复 3 次通过，之后整套 22 项通过。
- 已查看 WebKit 截图 `frontend/test-results/lyrics-normalized-precisio-57249-in-reading-and-adopted-text-webkit/lyrics-precision-aligned.png`：时间/原文/译文同排，卡片标明时间精度对齐，不误标翻译启发式识别；截图为合成数据。
- `openspec validate improve-lyrics-normalization-and-ranking --strict`、`git diff --check`、本目录相对链接检查：通过。

## 未验证边界

未执行真实上游网络验证或 Wails 原生窗口人工验收，未勾选原 change 的 9.3/9.4。截图对应的真实上游响应格式/截断方式仍未取证；离线测试证明新规则及交付/采用链行为，不代表接口当前可用。无历史文件迁移，旧歌词需重新获取并明确采用后才更新。
