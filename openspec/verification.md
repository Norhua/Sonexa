# 文档与实现核对

日期：2026-10-16。范围：提交 `a169712d`、`a656ca3` 的详情抽屉与歌词管理，以及 `change_record/spec-diff.md` 的 D01–D08、`5e5b1865-plan.md`、`a169712d-plan.md` 两份计划的最终执行结果。以 `a656ca3` 的现有代码为依据，不把早期方案或尚未实现格式写成当前规则。

## 核对结果

主规格从 26 份增至 27 份：修改 8 份既有规格，新增 `upstream/lyrics-search`。本次 delta 覆盖 9 个能力、22 个需求，未删除既有需求或场景；未涉及的需求保持不变。索引、架构、已知问题和本记录同步更新。

| 差异 | 主规格去向与最终规则 | 实现证据 |
| --- | --- | --- |
| D01 | `platform/app-shell`、`library/artists`：无参与角色时只显示空态，保留可靠 CV 投影，不显示署名兜底或来源说明；不删署名和艺术家关联 | `DetailDrawer.vue`、`bindings_library.go`、既有 CV 读模型 |
| D02 | `platform/app-shell`：默认 340px、鼠标/键盘调宽和偏好保存、视口范围约束，分组内容缩进 12px | `useDrawerWidth.ts`、`DetailDrawer.vue` |
| D03 | `platform/app-shell`：常显歌词状态图标，独立歌词页及返回，无歌词引导；详情隐藏时间标签但不改文件 | `DetailDrawer.vue`、`frontend/src/lib/lyrics.ts` |
| D04 | `editing/track-editing`、`upstream/lyrics-search`：菜单、本地导入、确认清除、双栏搜索与独立来源重试；成功预览后明确采用，只改当前歌词，区别于编辑器草稿 | `bindings_lyrics.go`、`LyricsSearchView.vue`、`lyricsSearch.ts`、`App.vue` |
| D05 | `upstream/provider-registry`、`platform/data-store`、`upstream/lyrics-search`：四个歌词专用来源、共享配额/出网/取消、40 秒/4 MiB/2 MiB 边界、无响应磁盘缓存、30 分钟会话失效 | `internal/lyrics/service.go`、`text.go`、`provider/client.go`、共享 WEAPI 模块 |
| D06 | `matching/confidence`、`matching/text-processing`、`upstream/lyrics-search`：默认评分复用、单次关键词、版本过滤、近时长人工确认例外；所有来源完成后才自动预览高置信项，不自动采用或修改元数据全局规则 | `internal/lyrics/search.go`、`search_test.go`、`lyricsSearch.ts` |
| D07 | `upstream/qqmusic`：仅手动歌词批量取普通与逐字块，显式翻译/罗马音，在线 QRC 有界转换与回退；元数据详情/补全链路不变 | `qq.go`、`qrc.go`、`qrcdes/`、`qq_test.go` |
| D08 | `upstream/lyrics-search`、`platform/data-store`：通道缺失/失败分离、无时间轴附加正文保留、按真实时间合并；最终预览文本采用，不落诊断和密文，不新增字段、FTS 或导出 | `text.go`、`qq.go`、`bindings_lyrics.go`、`store/lyrics_files.go`、`internal/app/lyrics_test.go` |

同时收窄“共享候选表格”和 QQ 组合词搜索的表述为元数据流程，避免误用于独立歌词；`library/artists` 中重复的旧署名兜底规则已一并移除。

## 本次实际执行

- `openspec validate --specs --strict`：27/27 通过；部分既有长需求有 INFO 提示，无验证失败。
- `openspec validate sync-lyrics-and-drawer-specs --strict`：通过。
- 主规格与 status 列出的全部 delta 逐项比较：9 文件、22 需求一致；既有需求和场景保留，未涉及需求不变，主规格无 delta 操作标题。
- 主文档链接目标检查及 `git diff --check`：通过。
- `go test ./...`：通过（命中 Go 测试缓存）。
- `go vet ./...`：通过。
- `npm --prefix frontend test`：14 文件、69 项通过。

本次只修改规格、相关文档和清理记录，不修改业务代码；未重跑浏览器、Wails 构建或真实网络接口，未操作真实用户数据或新增原生人工验收。

## 原实现计划的证据与边界

以下为被吸收记录中的历史结果，不是本次重新执行的结果：

- **第一轮（`5e5b1865` 起始，提交 `a169712d`）**：Go 测试、race、vet、前端 64 项单元、生产构建和 Wails 构建通过；Chromium/WebKit 全量 242 项通过。歌词文件选择器使用可注入 picker 与浏览器替身，未人工操作原生窗口。
- **第二轮（`a169712d` 起始，提交 `a656ca3`）**：Go 测试、race、vet、QRC 短时 fuzz、前端 69 项单元及构建、Wails 构建通过；歌词定向 Chromium/WebKit 共 6 项通过。全量 Playwright 为 241/242，失败为 `screenshot-review.spec.ts:107` 的编辑器“用户”徽标旧断言，基线定向复跑也有 2/3 复现。未修改或弱化该测试，基线只做 Vite 构建，不能据此声称旧源码完整类型检查通过。
- **MusicBrainz 角色调查**：目标「綿雪の夢 / ハトリアヤ / 狼と巫女とお菓子の家」的 release `54953b76-c791-4415-8092-1f5487cb8df9` 与 recording `7f4f192d-efb5-4d52-9005-74a435b1fd00` 实测 HTTP 200，目标关系均为 `relations: []`。现有请求包含录音/作品级角色扩展，没有解析遗漏证据，因此不从署名补造角色，也不改 MusicBrainz 语义；有精简回放回归 `TestSnowCreditSurvivesEnrichmentWithoutInventedParticipants`。
- **四来源网络核验**：四来源均有成功实测，也观察过 QQ `2001`、酷我 `301`；不承诺所有歌曲/任意时刻均可用。第二轮 QQ 目标取得 15370 字节增强歌词，含原文、翻译、罗马音；「晴天」原文成功而翻译/罗马音未提供，不能将缺失记为解析成功或接口失败。
- **安全与范围**：酷狗不使用证书主机名不匹配的旧域名，不关闭 TLS；QQ 仅内置有 MIT LICENSE 与来源提交的 DES 块变换，不引入无界解压。网易云 YRC、酷狗 KRC、本地二进制 QRC、播放器/实时高亮、独立歌词导出 UI 未实现。原始网络证据仅本地 `tmp/` 保存，仓库测试使用合成或精简内容，不提交完整第三方歌词。

此前用户确认的原生人工验收属于 `unify-upstream-and-redesign-track-editor`，不覆盖后续歌词功能。旧核对记录可用 `git show 5e5b1865:openspec/verification.md` 查看。剩余问题见 [known-gaps.md](known-gaps.md)，特别是 G16 元数据网络/封面配额与 G17 歌词来源及验证边界；Vite 大于 500 kB 的既有提示不视为已解决。

## 记录追溯

本次正式 delta、设计和任务位于 `openspec/changes/sync-lyrics-and-drawer-specs/`，主规格为当前行为依据。`change_record` 删除后，原差异及计划仍可从 Git 查询：

```sh
git show a656ca3:change_record/spec-diff.md
git show a656ca3:change_record/5e5b1865-plan.md
git show a656ca3:change_record/a169712d-plan.md
```
