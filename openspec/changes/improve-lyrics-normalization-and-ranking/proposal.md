# Proposal

## Why

Sonexa 的歌词搜索直接展示增强型 LRC 原始标签，逐字起止时间挤占正文；后端和状态层虽已评分，页面仍按来源分组，用户无法按匹配程度比较全部候选。现有通道模型与格式转换也不完整，需要参考 Lyrimuse 的实现补齐网易云 YRC、酷狗 KRC，并保证可读展示不损失保存内容。

## What Changes

- 搜索结果改为一个跨来源列表，严格按统一匹配分降序；高置信只用于标记、同分决胜和自动预览资格，不再凌驾于分数。来源仍有独立进度、错误、结果数与重试入口。
- 保留曲名/版本、艺术家、专辑、目标时长的现有准入与默认评分规则，增加结构化评分明细和稳定排序键；不按平台偏好加分，不预取所有候选，不因预览后发现逐字/翻译而重新打分。
- 搜索预览默认展示可读歌词行，行时间放在独立列，逐字标签不插入正文；提供「原始文本」切换以核对实际采用的合并文本。详情页继续只显示正文，隐藏标签不改动保存文件。
- 引入统一的行/词毫秒时间、原文/翻译/罗马音及诊断模型，统一解析、合并、去重和增强型 LRC 序列化；保留 Unicode、标点、空白与真实时间，不根据行号猜测对齐或生成逐字时间。
- 网易云新增 YRC 获取和解析，保留普通 LRC 与附加通道回退；酷狗优先获取 KRC，解析相对词时间及内嵌语言通道，失败时有界回退 LRC。QQ 完善 QRC 容器/通道格式识别与普通歌词回退；酷我完善逐行获取、状态报告和异常时间处理。
- 继续遵守现有 HTTPS、配额、响应/解压大小、40 秒总获取期限、内存会话、修订校验和明确采用边界。

用户已确认参考项目是 `tmp/lyrimuse/`，`tmp/lrc-change/` 的两份调查仅作历史线索；格式扩展包含网易云 YRC / 酷狗 KRC，不包含酷我 LRCX。此次只做规划，实施需另行发起 apply。

## Capabilities

### New Capabilities

无。新增格式仍属于现有独立歌词搜索能力，不拆出重复的歌词能力目录。

### Modified Capabilities

- `upstream/lyrics-search`：统一结果列表、分数优先排序及评分解释、可读预览、四来源通道与格式边界；新增 YRC/KRC 解析及可靠回退要求。
- `upstream/qqmusic`：完善在线 QRC 容器及各通道实际格式识别，在不改变元数据链路和匿名 MID 请求方式的前提下增强解析与回退。

## Impact

- 后端：`internal/lyrics/{service,provider,search,text,channels}.go`，`internal/provider/{netease,qqmusic,kugou,kuwo}/`；`internal/app/bindings_lyrics.go` 的候选/文档 DTO 与会话集成测试。
- 前端：`frontend/src/stores/lyricsSearch.ts`、`frontend/src/views/LyricsSearchView.vue`、`frontend/src/lib/lyrics.ts`，以及必要的可复用歌词预览组件；更新 Wails 生成绑定及 Go、Vitest、Playwright 回归。
- 文档：实施后按真实交付更新 `README.md`、`openspec/design.md`、`openspec/known-gaps.md` 的格式限制及验证入口；本提案不提前覆盖主规格或宣称在线接口已验证。
- 存储仍采用 UTF-8 不可变合并文本，不新增数据库通道表；本地仍仅导入 TXT/LRC，不导入二进制 QRC/KRC 或原始 YRC。
- 不增加歌词来源、播放器、高亮动画、自动翻译/音译、跨源正文混搭、自动采用、独立导出界面或元数据评分变更。
- Lyrimuse 为 GPL-3.0 参考项目：借鉴行为、协议与边界，按 Sonexa 架构独立实现，不直接移植其代码/夹具或引入运行时依赖。非公开接口的最新可用性必须在实施阶段以显式启用的有界验证确认。
