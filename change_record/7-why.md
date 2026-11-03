# 7：MusicBrainz 有记录但元数据候选为空

## 原始提示词

```text
关于change目录中的change_record目录:
因为openspec流程过于繁重，所以我们当前不直接生成change，而是将相关修改记录在./change_record/目录中，修改测试完成后我们会直接将其合并到主spec文档
，在change_record中有三类md文档，分别是:
1. spec-diff.md 只有一份: 修改阶段可能通过多次ai会话完成，他们共同维护spec-diff.md，spec-diff.md记录代码实现与spec文档的差异。这个文档用于后续
archive change时同步到主spec文档保持文档和代码实现的一致性
2. <一个从1开始递增的编号>-why.md: 每次修改前创建，记录：原始提示词，你的追问和提供的选项(包括选项的解释)和我的选择，修改目的
3. <一个从1开始递增的编号>-tasks.md: 和openspec 的tasks.md作用相同，用来记录并同步执行计划

当前遇到如图/Users/amadeus/Library/Application Support/com.raycast.macos/clipboard/file-b28284d88d38b19ca7f042029f8e1d7a.png歌曲在MB上无结果的问题，但是我直接去MB查询发现其实是有结果的/var/folders/xn/y6srybn90cv5cgpkv24s11_40000gn/T/pi-clipboard-368c74b9-95cc-4ef6-9b5f-911783052fff.png。这应该是一个问题的具体表现，而不是一种特殊情况，请你找出根本原因然后修复
```

## 追问、选项与选择

本轮未提出追问或选项。按用户要求直接调查并修复通用根因，使用根目录 `change_record/`，不创建 OpenSpec change，不修改主 spec。

## 修改目的与修改前调查

- 截图原始输入：`portamento / 川田麻美 / TVアニメ「灼眼のシャナ」ベストアルバム灼眼のシャナ-BEST-`；MB 网页对应署名是 `川田まみ`，发行名称是 `灼眼のシャナ -BEST-`（另有无空格写法）。输入也存在于 `tmp/music_extracted.csv`，不是只有截图可见。
- 现有查询有锚定与仅曲名兜底，但每级候选都用原始输入执行本地艺术家/专辑旁证检查。查询返回录音与最终候选可见不是同一件事。
- 待以真实响应复现：两个辅助字段的跨平台异写能否导致锚定未命中、仅曲名已返回目标却被本地检查清空。重点检查长专辑描述、非空格分词及上游署名旁证，不能据截图直接宣称 MB 没有记录。
- 修复应保持候选 track 身份与原始字段，不添加歌曲/艺术家/专辑名称白名单；人工召回不扩大自动身份、去重或自动选版权限。

## 方案与最终证据

### 根因已复现

两次真实公开 MB 请求，保存原始 JSON 至 `testdata/golden/responses/musicbrainz/search_portamento{,_anchored}.json`，请求与 SHA-256 见同目录 `search_portamento.md`。

1. 原始锚定查询 count=0；仅曲名查询 count=76、第一页 25 个 recording，实际 provider 展开 36 个 track。
2. 目标 recording `38a5891c-2aab-4bdc-b3b9-fa8879346b91` 已在第一页第 7 条，包含截图两个 CD 发行的真实第 2 碟第 4 轨。不是分页或响应解析丢失。
3. 原始艺术家 `川田麻美` 与上游 `川田まみ` 不匹配；MB 提供的 aliases 也未包含前者，不应凭推测将其当作严格身份等价。
4. 原始长专辑名包含完整 `灼眼のシャナ-BEST-`，但没有空格。既有 Tokens 按空格分词，无法识别该连续文本；它也不在长串开头、整体相似度不足，因此 CompareAlbum=MatchNone。
5. 两个辅助字段都判不匹配，`ScoreMetadataCandidate → IsPlausible → Engine.Run` 将已召回的正确 track 全部丢弃。修改前真实 App 回归确认 `Scored:[]`，两级均已执行。

### 实际修复

- 在共享元数据召回中新增 `metadataAlbumPhrase`：一方完整专辑文本在另一方长描述中连续出现；保留原始分隔边界，接受东亚文字无空格相邻，不接受拉丁单词内部截取。至少四个非泛称字母，数字不凑长度；排除 BEST、OST、ベストアルバム 等泛称。
- 只有曲名严格精确、没有截断时才可借该旁证召回；不能与模糊曲名、上游别名曲名或未注明版本召回叠加。专辑双方不截断且完整版本描述无冲突；只计 AlbumFuzzy，强制 ReviewOnly、HighConf=false。
- 不修改全局 Normalize/Tokens、CompareAlbum、ScoreCandidate、去重或自动选版；不将艺术家汉字/假名转换成严格相等，不修改 provider 查询、页数或限额。它是跨上游通用的文本召回规则，无 source 分发和歌曲名称白名单。
- 原来已可展示的严格候选提前返回，评分与自动资格不变。人工接受继续走精确 track 及发行成员准入。

### 结果与边界

真实响应只恢复两个原始发行 track：`a2079f0f-906c-4f64-b03c-9bdd36d5ebdb` 和 `72d6d601-73d7-4b5e-afd1-5d0c7655f2d2`。同录音其余不同专辑/罗马字名称位置不凭录音关系扩散召回；无关同名歌曲不展示；自动集合为空。

旧队列存的是先前已过滤的候选快照，不自动出网重搜。新版点击“重新搜索”即可重新评分已有原始 HTTP 缓存并恢复结果，无须清库。没有数据库/前端/公开绑定变更；完整验证见 `7-tasks.md`。
