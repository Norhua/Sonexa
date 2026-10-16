# Design

## Context

动机与功能边界见 [proposal.md](proposal.md)。本设计基于实际代码核对，而不是直接照搬参考建议：

| 观察位置 | 当前事实 | 设计影响 |
| --- | --- | --- |
| `internal/provider/provider.go`、`registry.go` | 大接口有 12 个方法，主要服务元数据搜索、快照、身份与补全；`SearchCovers` 已无生产调用方 | 保留元数据契约，删除失效的强制封面方法，不删除仍在使用的元数据注册表 |
| `lyrics/service.go`、`search.go`、`text.go`、`qq.go` | 四来源请求、解析、歌词回退混入业务代码；评分、文本校验、通道合并也在同包 | 按协议与业务规则拆分，不把整个 lyrics 包机械移动到 provider |
| `covers/sources/web.go`、`providers.go` | 重复 QQ/163 搜索请求与解析，另有独立 HTTP、重试和配额实现；CAA、iTunes 依赖完整元数据接口 | 收敛协议和传输；封面搜索契约不要求完整元数据能力 |
| `app/bindings_cover_search.go`、`bindings_lyrics.go` | 固定列表、来源校验及分支装配重复；歌词每次请求临时构建 Service | 一处装配、按业务登记；绑定层保留会话、授权、事务及事件 |
| `provider/artwork.go`、`covers/strategies.go` | 自动封面已采用注入策略，目录、工作池、持久化逻辑已统一 | 复用现有策略，不另建一套自动取图系统 |
| `provider/musicbrainz/{artwork,covers,cover_catalog}.go` | CAA 协议目前放在 MusicBrainz 包，既有生产策略也有旧搜索链路 | CAA 协议移为独立包，MB 保留身份关联的薄委托 |
| `provider/client.go` | `SkipCache` 仅跳过读缓存，非空哈希仍写缓存；歌词靠不传哈希做到不落盘 | 必须显式区分常规缓存、刷新和完全不缓存 |
| `provider/limiter.go`、`app/app.go` | 一个 Limiters 持续复用，Configure 原位调整桶；元数据适配器按设置重建 | 延续桶与优先队列身份，不由能力构造函数重新配置桶 |
| `config/settings.go`、`SettingsView.vue` | QQ 默认 30/分钟，界面只有最小值 1，后端未校验范围 | 引入有界合计速率设置，并在任何保存副作用之前校验 |

当前架构和 G16/G17、主规格仍描述 `cover-qq` 独立配额。用户已确认合并并允许合理提速，本变更用增量规格显式替换该行为；其他已知问题不是本次重构的附带修复目标。

## Goals / Non-Goals

**Goals:**
- 用编译期小接口和真实装配测试证明单能力来源可以接入，不仅仅改目录名。
- 同一上游的同一端点只有一处请求构造、基础响应解析和错误校验；不同业务的输入策略和结果投影仍可不同。
- 保持公共绑定及会话边界稳定，重建设置不会留下旧独立配额、漏接依赖或绕过门禁。
- 保留既有协议测试、QRC 许可证、封面缓存/文件引用和歌词部分成功语义。

**Non-Goals:**
- 不做插件系统、反射式能力发现、万能动态注册框架，也不把字段能力位重新解释为运行时服务能力。
- 不把所有元数据方法进一步拆成大量接口；保留 `VersionLister` 等已有可选接口。
- 不重构整个 domain/store/matching，不改数据库模型，不迁移历史数据或清空数据。
- 不新增来源、歌词格式、播放器或设置页面通用表单引擎；不改变元数据导入范围或匹配策略。
- 不强求所有网络都使用同一个 `http.Client` 实例。API 使用共享 provider 传输设施；CAA 索引与图片仍走已有封面工作池和专用传输。

## Decisions

### 1. 原始三个问题的结论与接口归属

**采用消费端接口，但不为了形式把稳定的元数据契约搬来搬去。** `provider.Provider` 正名为 `provider.MetadataProvider`，保留 ID、显示名、字段能力、速率、配置检查、SearchTracks、Refs、SnapshotFromCandidate、Complete、LoadReleaseSummary、FetchDetail；删除 SearchCovers。`provider.Registry` 保留名称，但文档和类型清楚限定为元数据注册表。`library`、`importer`、`app` 及测试替身更新类型；`matching.Searcher` 等既有窄接口继续保留。

`provider` 父包是现有元数据契约与通用传输边界，不能反向导入歌词、封面服务或具体上游。歌词接口归 `lyrics`；封面搜索接口放在现有消费侧子包 `covers/sources`，将它变成纯契约、注册和通用相关性规则包，而不是具体上游集合。自动图片的 `ArtworkStrategy` 保持在 provider 父包作为跨元数据/封面的稳定契约，不为移动接口制造循环依赖。

通用 `Upstream` 只含 `ID() provider.ProviderID` 与 `DisplayName() string`。所有能力接口使用同一种 ID 返回类型，不能一边 `string` 一边命名类型使多能力对象无法同时满足接口。元数据接口另外保留配置及速率方法；限流初始化接受独立速率描述或仅含 ID/RateLimit 的小接口，不要求 CAA 之类的来源伪造 API 速率或元数据配置。

接口轮廓（不是本轮要编写的代码）：

```go
// internal/lyrics：候选是统一歌词模型，不是原始 JSON。
type Provider interface {
    provider.Upstream
    SearchLyrics(context.Context, Query) ([]Candidate, error)
    FetchLyrics(context.Context, Candidate) (*Payload, error)
}

// internal/covers/sources：Query 带普通搜索条件及可选绑定发行身份。
type Provider interface {
    provider.Upstream
    SearchCovers(context.Context, Query) ([]Result, error)
}
```

`lyrics.Payload` 是内部获取结果：可携带已解码文本或原文/翻译/罗马音通道、逐字状态、警告；`lyrics.Service` 统一校验并生成现有 `Document`。保留 `Query`、`Candidate`、`Document`、`Source` 等公共 DTO 在 lyrics 包，避免 Wails 模型命名空间变动。原生引用保留为不向前端序列化的 `Candidate.Ref`，业务层不解析歌曲 MID/hash 等。

封面 Query 使用自己的字段（Title、Artist、Album、Country、Limit、Force、可选 `domain.ReleaseIdentity`），不要求调用方提供原始 URL 或 `store.Release`。每个 Result 使用明确的两种互斥形式：
- 新目录输入 `domain.CoverCatalogInput`：应用通用登记后读取目录；
- 已由共享目录服务登记的 `domain.CoverCatalogKey`：只读取，不再次保存覆盖。

构造/校验保证恰好一种形式有效。这代替当前 `if source != "caa"` 的持久化分支，并保留 CAA 的 ActualReleaseURL、错误记忆、目录版本与文件关联。领域适配器不直接写 store；store 已有图片模型的 domain 类型可直接复用。

**不选的方案：** 保留原大接口并让单能力来源返回“不支持”，会使无意义方法和错误路径继续扩张；彻底删除元数据接口会重复现有成熟流程；把所有能力、配置、缓存做成统一超级接口只是换名后的老问题。

### 2. 注册与依赖方向

```text
app 的集中装配入口
  ├─ provider.Registry（仅 MetadataProvider）
  ├─ lyrics.Service（歌词能力集合）
  └─ covers/sources.Registry（封面搜索能力集合）
        ↑ 注入 provider/<upstream> 的具体实现

provider/<upstream> → lyrics、covers/sources 的契约/纯函数
lyrics、covers/sources → matching / provider 父包 / domain
covers 的目录与文件服务 → provider 的 ArtworkStrategy / domain / store
provider 父包 → domain（不反向依赖具体来源或业务服务）
```

集合使用类型明确的条目而不是 `any` + 类型断言遍历所有上游。集中装配显式登记实际支持的能力：

| 上游包 | 元数据 | 封面搜索 | 独立歌词 | 自动图片 |
| --- | --- | --- | --- | --- |
| musicbrainz | 是 | 不直接登记；为 CAA 提供发行召回 | 否 | 委托 CAA 策略，保留 MB→CAA 关联 |
| caa | 否 | 是，注入发行召回和共享目录读取 | 否 | CAA 目录及图片策略 |
| itunes | 是 | 是 | 否 | 原策略保留 |
| qqmusic | 是 | 是 | 是 | 原 MID 策略保留 |
| netease | 否 | 是 | 是 | 不增加自动取图 |
| kugou / kuwo | 否 | 否 | 是 | 否 |

每个业务集合负责稳定顺序、ID 去重、未知 ID 错误。新能力集合的重复 ID 在构造时失败；已有元数据 Registry 的覆盖语义不顺带修改。业务显示名称由类型化登记条目保留既有文案，例如封面“QQ”和歌词“QQ 音乐”；不因共享上游对象改变 UI 名称。地区需求放封面条目声明，元数据仍通过 RefDescription 提供。默认歌词与封面顺序维持现状，元数据顺序继续来自设置。

`ListLyricsSources`、`ListCoverSources`、请求参数校验和执行全部读取同一集合。新增仅歌词来源只需适配器和装配登记，既有业务服务与绑定逻辑不改；通过假的单能力来源测试这条扩展路径。未初始化时列表返回空集合，不保留另一套静态默认名单作为隐藏分支。

**不选的方案：** 在业务函数内继续按 source 构造适配器，会保留二次名单与扩展分支；把所有来源加入元数据 Registry 会污染 UI 和导入契约。

### 3. 上游协议唯一实现，业务策略不被抹平

QQ 包提取不经过元数据投影的私有歌曲搜索原语，集中声明 `DoSearchForQQMusicDesktop`、请求 envelope、头、状态校验及 `[]song` 解析。三个能力调用该原语后各自映射；不能让封面搜索直接借用 `SearchTracks` 的完整行为，否则无歌曲 MID 的有效专辑封面可能丢失，元数据查询词也会污染封面/歌词搜索。

| 用途 | 现有查询/取数策略必须保留 |
| --- | --- |
| QQ 元数据 | 曲名＋艺术家＋专辑；TitleOnly 仅曲名；默认 25，合法上限 50 |
| QQ / 163 封面 | 曲名（缺失用专辑）＋艺术家；召回 30 首后按相关性过滤、按专辑与图片去重，最多展示 20 张 |
| QQ / 163 歌词 | 曲名＋艺术家，二者为空才用专辑；最多 20 首，再由 lyrics 评分/去重 |

网易云在现有 `provider/netease/weapi.go` 基础上收敛 WEAPI 表单、搜索响应及歌曲/专辑字段，增加封面、歌词投影。163 专辑 ID 为 0 时的 `pic-...` 图片身份、域名验证和尺寸 URL 构造归该适配器。

歌词迁移边界：
- 上游包：URL/参数/Header、JSON/JSONP、Base64/实体编码、网易云加密、酷狗歌词候选及 accesskey、酷我上游时间转换、QQ 新旧接口和 QRC/DES/zlib/XML。
- lyrics：输入校验、40 秒总获取上下文、通用 2 MiB 文本校验、伴奏排除、匹配评分与说明、稳定排序/去重、通道合并及最后 Document。
- QQ 回退保留新接口已成功通道；可选通道错误不升级为整文档失败。通用通道构造/选择及文本校验函数留在 lyrics 供适配器调用，QRC 原始格式知识不留在 lyrics。
- 移动 `lyrics/qrc.go` 与 `qrcdes/` 至 QQ 包内，连同许可证和来源提交说明。纯文本增强型 LRC 处理仍属于 lyrics，不因格式来自 QQ 而专有化。
- QQ 元数据链路仍优先使用详情中的歌词，缺失再取旧端点，不自动改用手动逐字流程。两种策略复用同一旧端点传输和基础解析，分别投影原文指针或完整通道结果，保持元数据获取失败不影响其他字段的语义。

封面 `Relevant` 的实现仍在消费侧，iTunes/QQ/163 投影调用同一纯规则；CAA 不使用文字相关性规则。统一协议错误校验须覆盖缺失结构、业务错误与合法空数组，不能强行用元数据字段完整性过滤封面或歌词。

**不选的方案：** 三处都调用 SearchTracks 看似复用但耦合了业务策略；只共享 URL 常量却保留三份 JSON 解析不解决错误规则漂移。

### 4. CAA 独立化与封面既有机制复用

新增 `provider/caa` 承接 CAA JSON 类型、目录/缩略图解析、URL 降级、archive.org 直连及专用 ArtworkStrategy。`musicbrainz.Provider.ArtworkStrategy()` 仅委托 CAA 策略并声明元数据关联，不再保留第二份协议实现。CAA 包不反向导入 musicbrainz；其封面搜索对象接收窄的发行召回接口和目录读取回调。

CAA 搜索保持绑定发行优先、与 MB 召回发行去重、最多 10 个发行，并发读取目录；目录加载仍调用 `covers.Service.Catalog`，受同一个 Pool 的合并、门禁、缓存与取消管理。召回失败或部分目录失败时保留已取得结果及错误，不把部分成功变成全空。独立 CAA 目录能力本身不要求提供元数据，默认的文字搜索只是装配了 MB 召回依赖。

旧 `Provider.SearchCovers` 方法及只由它使用的 `CoverGroup` 类型、直连旧索引路径删除；`CoverImage` / `ArtworkLadder` 等若仍被既有自动降级链引用则保留或迁至对应协议包，按调用证据处理，不能整文件误删。更新 App 注入的缩略图解析器及测试引用，保留既有自动预览、采用、清除和文件保护机制。

**不选的方案：** 只移动 WebSource 而不处理 CAA，无法形成清楚的独立上游边界；重写整个封面解析器/存储则扩大无关风险。

### 5. 共享传输、明确缓存与限流策略

API 适配器共用应用提供的 `provider.Client` 及长期存活的 Limiters。QQ/163 不再使用 WebSource 的独立 HTTP/重试循环；由 Client 统一有限重试、优先级、请求门禁、响应大小限制。按请求附加 HTTPS 重定向允许主机与次数限制，检查时继续调用公共门禁；不可原位改共享 HTTP Client 的 CheckRedirect，避免并发请求相互污染。允许策略化客户端浅副本，但底层 Transport、Limiters 及缓存设施共用。

请求缓存改成明确模式（名字可按代码风格调整）：
- `UseCache`：可读、可写；命中不消费令牌。
- `RefreshCache`：不读、可写，对应既有元数据 Query/Ref 的 `SkipCache=true`。
- `NoStore`：不读也不写，独立歌词的搜索、正文、多步下载及回退必须使用。

QQ/163 手动封面搜索也是 NoStore，保留 WebSource 当前行为；CAA/iTunes 手动封面召回保留元数据刷新缓存行为，结果目录仍持久化。逐一更新 GetRequest 调用点及测试，避免依赖“哈希为空”隐式表达策略；公开 Query/Ref 的 SkipCache 含义保持不变。

配额 ID 与来源 ID 分开理解，业务不计算配额键：

| 请求 | 配额与设施 |
| --- | --- |
| MB API / iTunes API | 原设置共享桶，包括 CAA 的 MB 召回、iTunes 封面查询/Lookup |
| QQ API（含旧歌词端点） | `qq`，三个业务共享；删除 `cover-qq` |
| 网易云 API | `163`，封面＋歌词共享每秒 1 次；内部替换旧 `cover-163`，不改 UI/存储来源 ID |
| 酷狗 / 酷我 API | `kugou` / `kuwo`，各每秒 1 次，多步请求仍分别取令牌 |
| CAA 索引、全部图片 | 原封面池；不新增 API 令牌桶，不改变直连与无环境代理规则 |

桶只由装配/设置更新配置一次，能力适配器构造不能偷偷 Configure。总歌词获取的 40 秒包含配额等待、重试和回退，子请求不能重置总时限。

**不选的方案：** 每个能力各建 Client/Limiters 会放大配额；把图片下载也并入 QQ 桶会改变既有封面性能规则；把所有 SkipCache 都改为不写会破坏元数据刷新缓存。

### 6. QQ 合计速率与设置生命周期

默认 45/分钟（约 1.33 秒一次），比原元数据桶 30 提高，同时低于旧 QQ 封面单独 60/分钟；可调范围 1–60，burst=1。该选择参照现有封面 1/秒及 QQ 代码中约 1.2 秒间隔的历史说明，仅为保守工程默认，不是本轮在线测得的安全额度。出现限流仍退避，不自动提速或增加并行桶。

由 provider 父包的一份无业务依赖速率描述保存 QQ 默认值与边界，供独立适配器构造及 config 共用；config 提供启动规范化和保存验证函数，应用只注入已经校验的有效速率。QQ 包不新增对 config 的依赖，也不复制默认常量。有限正值越界启动时夹到边界，非正值用默认；新保存严格拒绝非法值，允许范围内小数。非有限值不能来自合法 JSON，但后端 Go 调用仍须防护；损坏 JSON 保留既有读取错误语义，不偷偷恢复整份配置。合法旧值如 30 不自动提到 45。

`App.SaveSettings` 在目录迁移、磁盘保存和 Configure 之前校验。新默认、UI 上限和合计配额说明一起更新；不新增配置字段或七来源通用设置面板。

装配入口同时构建元数据 Registry、歌词 Service、封面来源集合；构造失败不发布半套集合。设置保存后在锁下原子发布新集合，Limiters 对象不变并原位更新速率。新会话使用新集合，已存在会话持有所需适配器快照，不因重建丢失候选/文档；门禁始终通过当前设置检查，速率通过共享桶生效。已经取得排队权或做出的令牌预约不被强制回收，后续预约采用新速率。

歌词不再在每次调用时 `Service{Client: client}`；`lyricWork` 取得会话绑定服务而不是裸 Client。封面启动时同样封存所选能力对象，取消和 generation 规则不变。shutdown 仍先停止新工作、取消会话、等待工作退出再关闭 Store。封面缓存目录改动沿用 MoveRoot 路径，能力集合不能持有已废弃服务或路径。

**不选的方案：** 全部重建 Limiters 会放行额外令牌并遗失排队权；保存时静默修正用户输入会让表单与实际值不一致；为提高速度绕过 Retry-After 不可接受。

### 7. 验证策略与测试迁移

将协议测试移到各上游包，不删除覆盖；lyrics 留纯业务/格式测试，covers/sources 留相关性与能力集合测试。生产业务包去掉具体来源导入后，同包测试若再导入实现其接口的适配器会形成循环，必须拆为外部测试包或迁到适配包/`internal/app` 集成测试。迁移每种业务时，删除旧业务包对具体来源的导入、加入反向能力实现、更新调用方与测试必须形成同一编译闭环；不能在旧依赖尚存时先提交新增的反向导入。任务的测试随各子项补齐，在本组闭环完成后一起运行，组间提交保持可编译。

重点验证矩阵：
- **契约与接线**：单歌词/单封面假来源可使用真实列表、查询、预览、采用流程；未知/重复来源明确失败；元数据集合与导入范围不变。启动、保存设置、变更封面目录与退出使用生产装配路径。
- **协议复用**：QQ 三用途、163 两用途请求捕获；固定各自关键词、limit、Header、缓存模式；不同结果投影复用基础解析但不互相裁掉有效数据。QQ 错误块、空列表、专辑 ID=0、无歌曲 MID 的有效封面分别覆盖。
- **歌词**：现有 `search_test.go`、`qq_test.go`、`lyrics_test.go` 的评分、QRC、通道、回退、大小、重定向测试；App 会话归属、30 分钟失效、取消、修订冲突及原子保存。
- **封面**：CAA 10 发行上限、绑定目标、部分错误、目录不二次保存；iTunes 地区与异图保留；QQ/163 聚合和尺寸；原目录、池合并、图片采用与回滚回归。
- **传输/设置**：带记账缓存的 NoStore 零读零写；Refresh 写回；三类 QQ 实际 HTTP 到达时间共享同一桶；其他平台独立；新实例不重置；重试/回退再取令牌；保存期间排队/取消无 race。验证边界值、非法保存无副作用、旧配置保留及有效值一致。
- **前端/API**：保留 Wails 函数和 lyrics DTO 命名空间；重新生成绑定后跑 Vue 类型检查、Vitest 与定向 Playwright，补 QQ 设置控件及保存说明用例。

全量 Go 使用 `go test -count=1 -race ./internal/...`，静态检查使用 `go vet ./...`（根包依赖生成的 frontend/dist）；前端命令遵循根 README。真实来源测试继续环境变量显式开启，可迁到外部组合测试以保留原 `SONEXA_LYRICS_LIVE`、`SONEXA_LIVE_COVERS` 入口，并新增 QQ 元数据单碟/多碟/歌词回退样例。用有限请求样本记录 30/45/60 的到达间隔、429/业务错误、Retry-After，不做压力测试；遇到风控停止提速，不绕过限制。不能从少量成功推定官方额度或长期可用。

## Risks / Trade-offs

- **[接口倒置后循环依赖]** → 父包不导入业务或具体来源；CAA 通过窄接口注入 MB；同包实现测试分离；新增 AST/import 边界检查，禁止歌词与封面生产代码导入具体上游。
- **[统一歌曲协议误删有效封面/改变排序]** → 先解析上游私有模型，按业务分别投影，保留查询和 fixture 对照；相关性规则只保留一处。
- **[歌词缓存或通道语义退化]** → 明确 NoStore，记账缓存测试；业务仍统一合并，可选失败/回退警告不能被统一错误处理抹掉。
- **[CAA 结果二次保存破坏目录]** → Result 明确已持久化引用与新输入，通用验证两者互斥；保持共享目录加载及 ActualReleaseURL。
- **[QQ 提速触发非公开接口限制]** → burst=1、有界范围、允许低速率、正常重试退避；在线证据单独记录，G16 的可用性风险不因架构重构关闭。
- **[设置重建导致旧会话泄漏/绕过新门禁]** → 会话持有服务快照，门禁和限流共享动态设施，原子发布，shutdown 与 race 回归。
- **[移动 QRC 丢失授权说明]** → 连同 LICENSE 和来源记录移动，不引入新算法实现或依赖。
- **[文档将规划写成完成事实]** → 本轮只写 change 目录；主设计、known gaps、验证记录等到实现有证据后更新。

## Migration Plan

1. 为现有协议、业务投影、缓存与装配建立特征测试；先落小接口、能力集合和元数据改名，保持每阶段可编译。
2. 明确 Client 缓存/重定向策略与共享桶初始化规则；配额键切换和 QQ 新速率设置在全部能力接线完成后统一验收，不增加双配额过渡开关。
3. 收敛 QQ/163 歌曲搜索及旧歌词原语，以一个编译闭环迁移四来源歌词、QRC、业务服务与 App 调用；移除旧方向导入后再接入反向能力实现，验证真实绑定和存储流程。
4. 迁移 CAA 与四来源封面搜索，集中接线；将 QQ 封面切入共享桶，随后完整验证新的速率设置范围和保存行为。删除 WebSource、静态名单、无用旧 SearchCovers 及失效注释，不留第二套生产链路。
5. 完成生命周期、配额、UI 和全量回归，单独记录在线/原生验证边界；依据证据更新主架构与 G16/G17 对应现状，不关闭无关 known gaps。

不需要数据库或文件迁移；持久化来源 ID 与图片/歌词身份不变。回滚通过整体回退对应实现提交完成，不能只回退某个适配器导致接口错配；QQ 合法范围内值可被旧版本读取，必要时手动恢复 30/分钟。不以删除应用数据作为回滚手段。
