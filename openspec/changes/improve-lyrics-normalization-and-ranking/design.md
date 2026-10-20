# Design

## Context

动机与用户确认范围见 [proposal.md](proposal.md)。以下为只读源码审阅结论，不是新能力已交付或上游实时可用性的证明。

### Sonexa 当前实现

| 位置 | 已观察到的行为 | 本次设计依据 |
| --- | --- | --- |
| `frontend/src/views/LyricsSearchView.vue` | 遍历 `results[source].cards` 分组；`<pre>` 直接输出 `document.text` | 两个用户问题都能定位到明确渲染路径，不需要推倒搜索会话 |
| `frontend/src/stores/lyricsSearch.ts` | 已有汇总 `cards`，但高置信先于分数；自动预览等待各源完成 | 改为同一分数优先比较器供列表和自动预览筛选使用，保留人工选择优先 |
| `internal/lyrics/search.go` | 复用 matching 默认权重，已有版本过滤、近时长人工例外和 20 分时长扣减 | 不是“没有评分”；保留准入语义，补评分明细及稳定决胜 |
| `internal/app/bindings_lyrics.go` | 按来源独立会话，候选 ID 为随机 UUID，Ref 不暴露；采用使用会话内 `doc.Text` | 排名不能以 UUID 决胜；不能让前端展示文本成为写入源 |
| `internal/lyrics/{text,channels,service}.go` | 主要用正则检测格式、按标签合并字符串；无行词模型；无附加通道时返回原文 | 补结构化中间模型，区分无时间正文、时间轴、原始序列化文本和预览投影 |
| `internal/provider/qqmusic/{lyrics,qrc}.go` | 已有双块 GetPlayLyricInfo、变体 DES、QRC 转增强型 LRC、旧接口回退；XML 只读 `Lyric_1`；原文与附加通道格式识别不完全一致 | 保留成熟获取路径，完善容器边界及按实际解码内容分类，不改元数据获取 |
| `internal/provider/netease/lyrics.go` | 仅请求旧 WEAPI 歌词，逐字为 unsupported | 新增 YRC bundle 及旧路径回退 |
| `internal/provider/kugou/kugou.go` | 歌词候选只读 id/accesskey/duration，按时长取第一条，仅下载 fmt=lrc | 校验候选身份，补 KRC 解码和 language 通道，不把文件搜索成功视为歌词成功 |
| `internal/provider/kuwo/kuwo.go` | 旧 H5 逐行接口；非法时间降为纯文本；与酷狗一样仅返回 Payload.Text | 统一通道诊断，补 HTTPS 逐行获取路径与非同步警告 |
| `frontend/src/lib/lyrics.ts` / `DetailDrawer.vue` | 详情已能隐藏常见 LRC 和逐字标签 | 保持详情纯正文，统一前端标签语义，防止搜索/详情规则漂移 |

现有 `frontend/tests/e2e/lyrics.spec.ts` 明确断言预览 `<pre>` 含逐字标签，需改为默认可读、切换后原始文本保真；`internal/lyrics/search_test.go` 已覆盖「綿雪の夢 / ハトリアヤ / 狼と巫女とお菓子の家」及 Crest 人工确认例外。`internal/provider/qqmusic/lyrics_test.go` 已有密文构造、间隔、通道失败和普通回退夹具，应延伸而不是替换。

### Lyrimuse 源码对照

参考仓库 `tmp/lyrimuse` 审阅版本：`c4ed629c6f332352f8be3ef9799baf4c9413939e`。路径均相对此目录；它是 GPL-3.0 项目，本次独立实现，不复制其源码或整套测试数据。

| 参考位置 | 具体发现 | 采用或拒绝的理由 |
| --- | --- | --- |
| `lyrimuse-engine/searchcli.go`、`match.go:scoreLyricCandidateDetailed` | 搜索持续输出全局已评分列表；评分项可解释；无静态来源可靠度加分，但有播放器身份、逐字、行数、译文、共识及时间轴质量分 | 借鉴全局排序、解释项和无静态来源偏好；拒绝播放器/内容权重及先下载全部正文，与用户选择的按需获取不符 |
| `Sources/LyrimuseCore/Lyrics/LyricsPreviewText.swift`（位于 `lyrimuse/` 下） | 行时间与正文分开；预览投影不改采用原文，保留 offset 的存储语义 | 借鉴显示/存储分离；不照搬职员表过滤、语言猜测和播放引擎依赖 |
| `lyrimuse-engine/netease.go` 的 fetchBundle | v1 返回 LRC/翻译/罗马音/YRC；YRC 与 LRC 可能不是同一断行和时间轴 | 新增 v1 bundle，但不按下标强行对齐；拒绝跨桶绕过限流的回退策略 |
| `lyrimuse-engine/qq.go` 的 extractQRCLyricContent、qqApplyDecrypted、qqAuxiliaryPlainToLRC | 属性可能有未转义引号、多轨；正文可能 QRC/LRC/纯文本；附加通道各自独立 | 采用受限容器容错和实际格式识别；不采用额外数字 ID 查询、匿名会话获取及新网关链路 |
| `lyrimuse-engine/kugou.go`、`krcnegativeoffsets_test.go` | Base64 → 文件头 → XOR → zlib；词时间相对行首；language 按原始行序关联；负词偏移确实存在 | 采用格式语义与回退思路；拒绝 HTTP 下载、8 MiB 解压上限、静默吞错误和直接钳制未知时间 |
| `lyrimuse-engine/kuwo.go`、`kuwolrcx.go` | 新的逐行 openapi、mobi 及 LRCX；LRCX 译文可能挂在下一原文时刻 | 本次只取 HTTPS openapi 的逐行实现线索，不接 LRCX/mobi，不复制按行猜测翻译 |
| `lyrimuse-engine/lyricstimeline.go`、`yrcwhitespace.go` | 重挂时间轴需文本及时间依据，空白词会影响播放填色 | 不进行播放导向的重挂、插值或延长词时长；保存真实词间隔，无法可靠对齐就单独保留 |

`tmp/lrc-change/lyrics_search_issues.md` 中“没有评分、QQ 没有 QRC”的描述已被 Sonexa 当前代码超越，不再作为现状。`openspec/specs/upstream/lyrics-search` 当前要求来源分组、高置信优先并排除 YRC/KRC；本变更明确修改这些规则。`matching/confidence` 的默认权重和歌词局部规则边界不变；详情、文件存储、编辑草稿边界也不变，因此不制造对应无意义的 delta。

## Goals / Non-Goals

**Goals:**

- 保持 provider → lyrics 消费侧契约与共用传输架构；解密、业务码、来源占位留在各 provider，规范化和合并留在 lyrics。
- 每份文档只确定一个原文主时间轴；统一模型同时生成可读行和唯一采用文本，两者不能各自拼接。
- 用共享合成语料验证 Go 序列化、前端显示及存储往返，不依赖联网证明解析正确。
- 保留有界、取消、不可变文本和修订提交，不为新格式引入持久化通道模型。

**Non-Goals:**

- 不移植 Lyrimuse 的完整引擎、播放同步、跨源歌词拼接、源偏好、内容共识、别名网络扩查或批量候选预取。
- 不通过自动补齐歌词、翻译/音译生成、删职员表或长尾奏比例猜测来“改善”正文，不改变元数据匹配权重。
- 不新增本地二进制导入、YRC 文件导入、导出 UI 或历史数据迁移。

## Decisions

### 1. 分数优先，置信度与排序各司其职

`rankCandidates` 继续使用 matching 的默认分项和当前歌词准入/高置信规则。新增 `ScoreTerm { kind, points, match? }` 等结构化解释，固定 kind 对应前端文案，非评分确认原因仍放在 `MatchNotes`。总分采用同一次计算结果；若触发最低 0 分钳制，明细包含调整项，保证可核对。

后端生成稳定 `SortKey`（来源与原生歌曲身份的确定性编码，不含歌词下载 accesskey），供跨来源比较。随机 `ID` 继续仅用作当前会话取词/采用权限；不把排序键当可提交身份。统一比较顺序：

1. `Score` 降序；
2. 同分 `HighConfidence` 为 true 优先；
3. 来源 ID、来源内稳定身份按明确字典序升序。

后端来源内排序与前端全局比较使用相同规则和跨语言 fixture，不使用依运行环境变化的 locale 排序。仍按 Source+Ref 去重，重复身份选择评分最高项；同名跨来源不合并。上游只有 20 项，不增加结果分页或补查。

前端 `cards` 成为唯一候选展示顺序，`results` 只管理来源状态。自动预览在当前来源都结束后从该顺序选首个高置信项；即使它不是全列表第一名也可自动预览，87 分需确认项不能遮蔽 75 分高置信项的自动预览资格。失败不自动轮询或挨个试取。重排以候选 ID 保持人工选择和焦点。

单源重试使用已提交查询快照，不把编辑中的新条件与旧来源结果混合比较；条件改变后应发起整轮新搜索。沿用 generation/job/session 的迟到响应防护。

**替代方案**：只把模板改为 `store.cards` 会继续高置信先于分数且同分依赖随机/到达顺序，不满足确认后的排序规则。复制 Lyrimuse 的正文质量分需要预取，已由用户排除。

### 2. 结构化内存模型与唯一序列化文本

在 `internal/lyrics` 补充独立于上游语法的结构，建议形态：

```text
Word { startMs, endMs?, text }             // 有真实终点才填写 endMs
Line { startMs?, endMs?, parts[], text }   // text 为去控制标记后的完整正文
Track { lines[], metadata[], offsetMs, diagnostics[] }
Payload { original, translation, romanization, wordTiming, warnings }
Document { text, format, rows[], 原有通道状态与 warnings }
PreviewRow { timeMs?, text, channel, translation?, romanization? }
```

Provider 可提交已规范化 Track；保留 `Payload.Text` 作为现有单文本能力和本地 TXT/LRC 的入口，但进入 `DocumentFromPayload` 后统一校验，不让两条路径有不同格式判据。各上游原生 QRC/YRC/KRC 解析仍在 provider 子包，输出通用结构，不让公共层识别业务码、占位文案或平台密钥。

- 源时间用受范围检查的整数毫秒，单次数字加法前检查溢出；原生逐字时长必须非负，绝对时间采用现有 24 小时安全上限。
- KRC 相对词开始允许负数，只要换算后的绝对时刻有效，就保留词早于行首的事实；不能把 QQ 的“词必须位于该行区间”约束生搬给 KRC。绝对负时间或超限不钳制成假时间，报告逐字失败并走可用普通原文。
- 增强型 LRC 连续标签区分词结束和下一词开始；没有显式终点时不借下一词开始声称真实终点。空白片段保留正文和时间，不复制 Lyrimuse 为填色而延长前词的算法。
- 本地 `ParseText` 保持 BOM/换行处理与原始文本保真，不把新规范化强加为写盘重排；线上多通道与格式转换生成规范化合并文本。格式标签识别限定在对应语法位置，普通括号、`1 < 2` 和 HTML 样式文字不删除。
- 扩展多个行首时间戳后再按时间稳定排列；同时间、同可见文字且时间结构相同可去重，行级与逐字同文重复时保留含真实词时间的那项。不同词时间不能误当完全重复，重复副歌在不同时刻保留。
- `format` 基于最终可用内容；逐字状态基于选定原文的有效非空词，而不是看到一个 `<...>`、请求了 qrc 或只有罗马音带词时间。

仅 `Document.Text` 进入 `StageLyrics/SaveLyrics`；`rows`、各通道诊断、解析中间对象和上游密文不入库。前端不能把纯正文回传作为采用内容。

**替代方案**：直接在 Vue 中去掉标签能短期改善显示，却无法解决上游绝对/相对时间、通道归属和重复序列化；单纯把三种格式统一转成 YRC 字符串又引入前端专用方言，不如通用模型加既有增强型 LRC 保存格式。

### 3. 时间对齐保守且不丢内容

选择原文优先级为可用逐字原文 → 普通同步原文 → 纯文本，选一种，不重复输出 YRC 和 LRC 两套原文。普通轨作为回退及诊断依据留在本次解析中。

独立附加通道仅在校正各自 offset 后的精确行时间可唯一归属时附到原文行；同刻多个原文导致归属不明时单独显示附加行。未匹配的有时间通道保留自己的时间；无时间文字放入非同步区。暂不采用 700ms 最近邻、按行号对齐或 LCS 自动重挂，避免重复副歌及不同断句被误配。

KRC language 是明确的格式内关联例外：先保留原始所有计时行（含空行），验证 language 行数，再将声明的行索引转换成真实时间。之后才筛空和排序。数量不符但文字有效时按附加轨原序保留非同步文本并警告；编码/结构损坏保持 error。type=0 不是天然“罗马音”：至少要求非空且含拉丁文字、无汉字/假名/韩文的明确音译文本才标罗马音；其他可解读内容标为未分类附加文本并警告，不伪装成罗马音或丢弃。为此内部 Track/预览行允许 additional 通道，既有三通道状态仍明确表达没有可确认罗马音。

offset 处理作为公共规则：正 offset 表示提前，比较实际时间 `stamp - offset`；只在合并多轨时折算到同一轴，输出一个等价全局 offset，避免重复应用。无偏移/相同偏移保留等价含义；需要负实际时间时通过统一全局 offset 保持序列化标签非负，不截断句首。重复且矛盾或无法解析的 offset 作为格式诊断处理，不静默选择一个。时间列展示最终序列化行时间，与「原始文本」可核对；有非零偏移另提示。单文本导入不重写 offset。

**替代方案**：Lyrimuse 的最近邻和逐字时间轴重挂依赖其播放引擎与大量语料，当前不能以“时间近”证明翻译对应。保守方案的代价是部分译文单独显示，但保住文字与真实时间。

### 4. 默认阅读模式，原始模式只供核对

新增轻量可复用预览组件（例如 `LyricsPreview.vue`），搜索页使用 `Document.rows`，两列为行时间和正文；同刻可靠附加文本在正文下以次级样式显示。原始模式使用纯文字 `<pre>` 展示 `Document.Text`，不通过 HTML 拼接。默认模式每次打开为阅读，切换不请求后端。

`frontend/src/lib/lyrics.ts` 仍服务详情的纯文本展示，补充与后端一致的标签边界测试；不再靠“删除所有方括号/尖括号”扩展识别。将合成的 TXT/LRC/增强型 LRC fixture 作为 Go 与 TypeScript 共同语义基准。详情不需要知道 QRC/YRC/KRC，因为采用前已转换。

候选左栏和来源状态区分开，来源状态保持四项轻量摘要；列表卡片保留来源、匹配分、原因和待获取/真实格式。格式未知时不显示逐字优势。长行换行，长列表和预览内部滚动，键盘切换模式与焦点可访问，不引入播放控件。

### 5. 四来源获取与回退

每个逻辑请求共享 40 秒 context，保留 NoStore、4 MiB 响应、2 MiB 解码/转换/合并限额和生产配额。以下调用数为协议阶段上限，底层原有有限重试仍计入同一截止及平台配额；不追加无限候选循环。

| 来源 | 选中候选后的路径 | 限制及验证点 |
| --- | --- | --- |
| 网易云 | 首选 `GET https://music.163.com/api/song/lyric/v1?id=...&lv=-1&tv=-1&rv=-1&yv=-1`；可回退既有 WEAPI `song/lyric`，最多两个阶段 | 首选端点来自参考代码，实施时验证业务码、纯音乐标记、YRC、普通/附加轨和结构化署名；禁止把 GET 当作 WEAPI 加密请求，传输仍通过 provider.Client；不追加 EAPI/跨桶轮询 |
| QQ | 保持 MID 的一个 musicu 请求取逐字/普通两块，必要时一个 legacy 阶段 | 复用既有改造 DES，不换标准 3DES；QRC 提取先正常 XML，再对已知容器形态做唯一边界的受限容错；多轨确定性选择，不用横跨多个 Lyric 节点的贪婪表达式 |
| 酷狗 | hash 查候选 → 最优有效候选 fmt=krc → 必要时同候选 fmt=lrc，最多三个阶段 | 搜索解码补 song/singer，hash 路径不把上传者名强当歌手身份；仍检查可比较标题/版本、id/accesskey、时长；同分稳定选择。暂不增加去 hash 关键词补查 |
| 酷我 | 采用 `https://www.kuwo.cn/openapi/v1/www/lyric/getlyric?musicId=...` 的逐行路径；可回退已有 HTTPS H5 路径，最多两个阶段 | Referer 随主机，code 与 status 两种响应各自校验；新增许可主机仅为确认的 HTTPS 域名；不启用 mobi/LRCX 或 HTTP 路径 |

网易云 YRC 原生 `(start,duration,flag)` 在文字前，QQ QRC `(start,duration)` 在文字后，KRC `<relativeStart,duration,flag>` 在文字前；各自解析后输出统一模型，不能仅替换括号。网易云结构化署名仅按已识别 schema 还原文本，没有可靠时间则保留为非同步文字，不凭结构中未确认字段猜测时间。

QQ 各通道共享“解码后按实际格式分类”的纯函数；`crypt`、`qrc` 用来选择已知编码，不以反复尝试不同解码算法把损坏数据伪装成纯文本。QRC 容器无法确定边界时失败，正常纯文本不误判为 QRC。占位/提示识别只放对应来源，任何新提示过滤只在有明确来源语义和测试时新增，不删除真实署名或版权信息来假装歌词完整。

KRC 校验 `krc1` 后使用已知 XOR+zlib，严格 LimitReader(max+1) 检测超限；language 单独解 Base64/JSON，并受同样字节边界。原文有效而可选通道坏时只使该通道失败；整个响应/解压超限不得靠回退掩盖。

错误分类新增或沿用可由 `errors.Is/As` 判断的类别：no_lyrics、decode/format、unsupported_endpoint、auth、rate_limit、transport/TLS、cancel/timeout、too_large。只有明确可回退的缺失、格式异常或已确认端点不可用才启动下一阶段；限流/认证、TLS、取消、超限立即停止后续网络动作。QQ 同一响应已取得的普通块不算“继续请求”，可以保留，但必须附部分失败诊断。不得根据错误消息字符串包含某几个字决定是否绕过限制。

**替代方案**：完全照搬参考项目的备用域名、HTTP、失败即换桶等路径与 Sonexa 的安全规格冲突；保留所有旧端点不动则无法充分覆盖已识别的 YRC/KRC 和酷我逐行问题。

### 6. 验证分层和回归矩阵

- **公共模型/纯函数**：毫秒精度、BOM/CRLF、混合文本、多时间戳展开上限、重复副歌、空白/emoji/括号、偏移、同刻歧义、词开始/结束/间隔，解析→序列化→解析的文字和时间不变量。
- **QQ**：延伸现有 synthetic 密文生成器，补字面引号、动态轨名、多轨、附加通道 QRC、仅逐行/纯文本、损坏原文/附加通道及禁止回退分类。
- **网易云**：YRC-only、YRC 缺失/损坏、翻译错序、独立时间轴、结构化署名、占位及业务码、普通端点回退。
- **酷狗**：合法头/坏头、Base64/XOR/zlib 各失败及压缩炸弹、相对/负偏移、language 对齐含空行、行数不符、谐音轨不误报、候选标题冲突与时长选择、KRC→LRC 回退。
- **酷我**：两端点状态码形态、缺失和错误区别、时长单位、非法时间全部保留、逐字未接入及附加通道未提供。
- **集成/前端**：跨源交错分数与逆序返回、同分稳定、不预取、最高分低置信、单源重试查询快照、人工选择不丢失、迟到响应、预览切换、采用原始合并文本、保存冲突保持旧文件、Chromium/WebKit 可读性。

默认测试使用独立构造的短文本和 mock HTTP，不把参考仓库的整首歌词纳入测试库。真实网络按 README 现有显式开关和 `internal/app/lyrics_sources_test.go` 的有界入口扩展；选定来源/曲名样例，记录状态和字节摘要，遇限制停止，不输出 accesskey、请求正文或歌词全文。离线通过、真实来源成功和原生 UI 验收分别记录。

## Risks / Trade-offs

- **非公开端点变化** → 实施先补契约夹具，再运行显式有界 probe；验证失败作为来源风险或阻塞报告，不悄悄切换不安全路径、不宣称四来源已通过。
- **QRC 引号容错吞入下一轨** → 正常 XML 优先，容错仅处理可唯一界定的已知容器片段，多轨/截断/恶意边界有专门测试。
- **YRC 与 LRC 不同时间轴** → 不自动猜测重挂，未对齐附加内容保留原时刻；接受部分译文展示不紧邻原句的代价。
- **KRC 音译轨语种不明确** → 保守标作未分类附加文本，保留文字并警告，而非假称罗马音。
- **字符串与结构同时驻留增加内存** → 不持久化中间数据，生成 Document 后释放不用的源 Track；解析展开、序列化和 DTO 行数/文本累计设置有界检查，不突破已有会话上限和有效期。
- **前后端排序或标签规则漂移** → 共享 fixture 和完全相同排序契约；采用始终使用后端会话文档。
- **安全错误导致回退次数减少** → 这是有意约束，优先明确失败而非规避认证/限流；普通块已经可用时仍显示诊断并保留可用内容。
- **GPL 参考及依赖风险** → 不直接复制 Lyrimuse 实现/夹具；沿用 Sonexa 已存在的 qrcdes 及其许可证说明，新实现不增加 GPL 运行时依赖。

## Migration Plan

1. 无数据库迁移；不改写既有歌词文件或用户数据。按项目早期开发约定测试可使用清空后的数据，但不能自动清库。
2. 先落公共模型/排序测试，再实现各来源适配与契约测试，最后更新 Wails 绑定、前端视图及集成回归；旧 DTO 字段保留语义，新增 rows/scoreTerms/sortKey 随桌面端同步构建。
3. 实施完成后更新 README 和当前架构/缺口文档的已交付内容；规格同步/归档另按工作流完成，不在提案阶段改写主规格。
4. 如需回滚，回退应用代码及生成绑定即可；已采用的 TXT/LRC/增强型 LRC 文件仍属于原有可读取文本格式，不需要迁移回滚。

## Open Questions

- 最新上游是否仍返回所需 YRC/KRC、QQ 通道及酷我 openapi 字段，只有实际有界请求能确认；这是现有非公开接口的可用性风险，不改变格式、失败行为或安全要求。若必须引入新鉴权方式或额外协议链才能接入，停止实施并请求调整范围。
- 哪些真实歌曲适合作为稳定的逐字、翻译、回退样例需由 probe 结果确定；synthetic 测试不以该选择为前提，不用整首歌词作为日志证据。
