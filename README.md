# Sonexa

以音乐收藏为中心的桌面资料库，不依赖本地音频文件。

技术栈：Wails v2 + Go + Vue 3 + TypeScript + Tailwind CSS v4 + Volt + Lucide Icons + Pinia。

基本信息允许缺失；具体发行按本地组聚合，各版本保留自己的收藏、结构和封面，支持合并或拆分展示。编辑器采用两栏草稿、确认“使用”及原子保存；未切换发行上游时可原地修改共享专辑信息，新建或切换上游则逐曲确认迁入，空发行随最后一首收藏迁出或删除而清理。

当前开发模型需要在清空历史应用数据后初始化；旧模型会明确拒绝打开，程序不会自动删除或转换。本轮原生人工验收已由用户确认完成；浏览器回归仍不能代替后续版本的原生验收，证据范围见文档核对记录。

## 文档

- [功能规格与术语](openspec/README.md)
- [架构设计](openspec/design.md)
- [已知问题与未完成目标](openspec/known-gaps.md)
- [文档与实现核对](openspec/verification.md)

## 目录

```text
main.go               Wails 启动与前端资源嵌入
internal/
  app/                应用协调、前端绑定、后台补全与桌面配置
  store/              SQLite、当前值/快照、事务、查询与文件引用
  provider/           元数据契约、共享传输与各上游能力适配器
  domain/             三层身份、统一快照和跨层契约
  library/            归组、准入及编辑保存计划
  matching/           文本处理、评分、入库判定
  importer/           表格解析与持久化导入队列
  covers/             目录、工作池、缓存与本地服务；sources/ 为消费侧契约
  lyrics/             歌词能力契约、匹配/内容评分与业务编排
  lyricdoc/           公共行词模型、格式解析、通道合并和只读契约校验
  export/             CSV / JSON 导出
  config/             路径与设置
  acceptance/         跨模块验收测试
frontend/
  src/                Vue 页面、共享组件、Volt、状态管理
  tests/              Vitest 与 Playwright 测试
  wailsjs/            自动生成的绑定与运行时（不提交）
build/                平台资源和配置；bin/ 为打包产物
testdata/             离线回归测试数据
openspec/             规格、设计、已知问题和历史归档
prompt/               初始需求材料
```

Go 测试通常与实现同包；跨上游组合测试放在 `internal/app`，避免消费侧契约与适配器循环依赖。`internal/app/bindings_*.go` 是手写业务代码。前端统一 API 使用 `frontend/wailsjs/go/app/App`、`Models.app`，浏览器测试通过 `window.go.app.App` 提供测试替身。

## 上游能力与装配

元数据继续使用 `provider.MetadataProvider` 与 `provider.Registry`，负责身份、字段能力、
候选快照和精确补全；它不要求封面搜索方法。歌词和封面分别消费 `lyrics.Provider`、
`covers/sources.Provider`，仅歌词或仅封面来源不进入元数据选项，导入仍只用 MusicBrainz。
具体协议位于 `provider/<upstream>`；业务包不导入具体上游。

`internal/app/upstreams.go` 是启动和设置更新的集中装配入口，验证能力与图片安全策略后
原子发布。新会话使用新集合，旧会话保留能力及图片策略快照；出网门禁始终读取当前设置，
API 配额和封面工作池长期共用。封面目录通过 `MoveRoot` 迁移，不重建池或丢弃已有会话。
完整能力矩阵、依赖方向和自动图片规则见[架构设计](openspec/design.md#上游能力与装配)。

## 开发与验证

需要 Go、Node.js/npm、Wails v2 及目标平台构建依赖。

```sh
# 首次检出：安装前端依赖，再生成绑定、构建和打包
cd frontend
npm ci
cd ..
wails build

# Go 测试与静态检查
go test -count=1 -race ./internal/...
go vet ./...

# 原生开发
wails dev
```

首次使用或修改 Go 导出 API 后，须先运行 `wails build` 或 `wails generate module` 生成绑定。只生成绑定时，需先为根启动包准备 embed 目录：

```sh
mkdir -p frontend/dist
touch frontend/dist/.gitkeep
wails generate module
cd frontend
npm ci
npm test
npm run build
npx playwright install chromium webkit
SONEXA_E2E_PREVIEW=1 npm run test:e2e
```

`go test ./...` 和 `go build ./...` 包含根启动包，依赖前端 `dist/`；测试 `./internal/...` 不需要前端产物。浏览器测试不能替代原生窗口、输入法、文件对话框和大曲库验收。

## 歌词来源验证

歌词消费契约与评分位于 `internal/lyrics`；公共模型及标准格式工具位于
`internal/lyricdoc`，不依赖业务、上游或网络。四来源协议位于
`internal/provider/{netease,qqmusic,kugou,kuwo}`，由 `app/upstreams.go` 创建并注入，
会话保留服务快照。每个来源在返回前完成选轨、通道识别、合并、去重和序列化，
直接交付完整 `lyricdoc.Document`；业务只读校验、评分、预览和采用，不再组装 Payload。
本地 TXT/LRC 由 `app/bindings_lyrics_input.go` 独立解析，在线采用不复用本地解析入口。
适配器共用 `provider/lyricfetch` 映射统一错误类别，保留停止语义和错误链；
该包不属于通用传输父包，业务不调用它解释平台错误。
QQ QRC 转换和 MIT 许可的 DES 实现位于 `internal/provider/qqmusic/{qrc.go,qrcdes/}`，
许可证及来源提交记录随实现保留。QQ 解码后按实际 QRC/LRC/文本识别三通道，
容器支持动态轨名及唯一边界的字面引号容错；原文可用时保留同批普通块，
认证/限流、TLS、取消和超限不再触发旧接口请求。未知业务限制保守停止，
不根据本地化错误文案推断可回退。
网易云新增 HTTPS `/api/song/lyric/v1` GET bundle，独立识别 YRC 绝对词时间、
普通 LRC、翻译、罗马音；已识别署名结构只保留文字，无法同步时不猜时间。
普通 WEAPI 仅在缺失、格式失败或端点不可用时单次回退。YRC 离线解析已覆盖，
最新真实来源可用性待显式验证；没有翻译/音译生成兜底。
酷狗 hash 子候选先验证标题/版本和下载标识，再按时长稳定选择；优先 KRC，
Base64/文件头/XOR/有界 zlib 解码后保留相对词时间（包括合法负偏移）。
language 按原始计时行关联，数量不符保留非同步内容，中文谐音不假称罗马音。
原生行终点和空词片段起止保留在结构中；空片段不计为有效逐字覆盖。
仅允许同候选 KRC→LRC 回退，候选搜索加下载最多三个协议阶段，限制类错误不回退。
KRC 与附加轨已通过离线合成测试，真实来源可用性仍待显式验证。

酷我逐行首选 `https://www.kuwo.cn/openapi/v1/www/lyric/getlyric`，允许条件下仅回退一次
HTTPS H5；不接入 HTTP、mobi 或 LRCX。两端点在适配器中共用两层内嵌译文识别：
- 第一层：外文/纯汉字行各至少 8 行，比例 0.7–1.3，至少 80% 汉字行紧随外文，
  另需外文曲名/艺术家或至少半数外文行含假名/谚文。
- 第二层：中文紧跟外文、时间严格更晚且等于下一计时行；第一层未确认时至少 4 个强匹配。
  拟声不计强匹配，孤立尾行和演唱标签不凑门槛，同刻归属歧义不拆。
命中仅重分类已有文字，标记 `embedded/available` 和 `kuwo-embedded-v1` 推断依据，翻译加分一次。
未命中保持 unknown，不声称没有翻译；非法时间/矛盾 offset 禁用识别，保留全文原序为纯文本。
规则有误判与漏判风险，署名/版权声明不删除、不作为译文证据；离线测试不证明实际在线效果。
通道摘要仅记录格式、状态、规则版本、命中层及数量，不输出正文或逐行关联详情。

在线组合测试默认跳过；显式启用时只输出标题、通道状态和字节数，不转储歌词全文：

```sh
SONEXA_LYRICS_LIVE=1 go test -v ./internal/app -run '^TestLiveSources$' -count=1
# 可附加 SONEXA_LYRICS_SOURCE=qq 及 TITLE/ARTIST/ALBUM/DURATION_MS 查询变量
SONEXA_QQ_PROBE=/path/to/local-response.json go test -v ./internal/provider/qqmusic -run '^TestLocalQQProbe$' -count=1
```

迁移后的离线回归不代表当前真实来源已验通；在线与原生验证边界单独记录。
完整文档边界的阶段回归：

```sh
go test ./internal/acceptance ./internal/lyricdoc ./internal/lyrics ./internal/provider/... ./internal/app
```

后端已接入 Top 3 有界预取、摘要事件/快照、显式版本预览批准和保留内存限制，
预算与取消边界见[架构设计](openspec/design.md#上游能力与装配)。
新阅读/综合列表 UI 仍在后续任务中，不由后端测试宣称完成。

上述歌词入口复用生产能力装配与共享平台桶，从 QQ 30 请求/分钟开始，全部来源合计最多
40 次 API 实际发送，每来源最多试前三个候选。可选通道缺失照实记录；HTTP/业务限制或
传输失败立即停止该轮，不因部分歌词成功而继续探测。日志不输出通道正文或上游错误消息原文。

封面搜索消费 `internal/covers/sources` 的窄接口，由 CAA/iTunes/QQ/网易云实现。
CAA 协议位于 `internal/provider/caa`；MusicBrainz 仅委托图片策略。
原有 QQ/网易云封面 live 入口已迁到 App 组合测试，仍默认跳过：

```sh
SONEXA_LIVE_COVERS=1 go test -v ./internal/app -run '^TestLiveCoverSources$' -count=1
```

封面入口覆盖四来源的真实搜索、池内预览与显式采用，全部来源合计最多 12 次 API 请求、
24 次图片/目录门禁许可（含重定向及既有有限重试）；`SONEXA_COVER_SOURCE` 可限定单来源。
可用 `SONEXA_COVER_TITLE/ARTIST/ALBUM/COUNTRY` 指定非空样例字段，未设置沿用默认；不改相关性规则。
仅诊断 iTunes 时，`SONEXA_COVER_DIAGNOSTICS=1` 会额外执行一次同条件的元数据召回，
记录前五个候选及相关性计数；这次请求仍计入原有 12 次 API 硬预算，错误后不继续请求。
所有封面 live 结果输出 state、卡片数和是否有错误，不输出上游错误消息原文。
临时数据库与图片目录随测试清理，不接触真实收藏。图片仍用生产直连、安全策略和无环境代理传输。

## QQ 元数据与有界速率验证

以下入口默认跳过，只在显式环境变量为 `1` 时运行。少量样本不是压力测试，也不证明官方额度。
所有入口从 30 开始；速率样本只有前一档三次业务查询都成功才进入下一档，遇到 HTTP/业务错误、
429、鉴权或传输失败立即停止，不重试探测或绕过限制；日志记录主机/路径、发送间隔、HTTP 状态、
业务错误码、Retry-After 和字节/数量摘要，不转储请求体、响应体或歌词。

```sh
# 搜索、固定单碟/多碟详情与真实缺失歌词回退；总计最多 12 次 API 请求。
# 回退需提供一首“详情没有内联歌词”的真实样例，不会篡改响应或用 fixture 制造在线证据。
SONEXA_QQ_METADATA_LIVE=1 \
  SONEXA_QQ_FALLBACK_ALBUM=<album-mid> SONEXA_QQ_FALLBACK_TRACK=<song-mid> \
  go test -v ./internal/app -run '^TestLiveQQMetadata$' -count=1

# 仅在明确授权另一次样例查找时运行；最多两次搜索、每次最多三个候选详情。
# QQ 30 请求/分钟，全部实际发送（含分页/回退/重定向）硬上限 16，未找到即停止。
# 可预先设置 SONEXA_QQ_DISCOVERY_TITLES，以换行分隔最多两个标题；不增加单轮预算。
SONEXA_QQ_FALLBACK_DISCOVERY=1 go test -v ./internal/app -run '^TestLiveQQFallbackDiscovery$' -count=1

# 仅在另行明确授权扩大查找后附加 SONEXA_QQ_DISCOVERY_EXPANDED=1：
# 最多 10 个标题、各最多 3 个候选详情、40 次实际发送、3 分钟；速率仍为 30 请求/分钟。
# 找到即结束；无样例/预算耗尽则保留未验证，不自动重开。HTTP/业务限制或传输失败立即停止。

# 按 30 → 45 → 60 顺序，每档元数据/封面/歌词各查询一次（9 次，含跳转硬上限 12）。
# 必须先确认上一轮没有风控；一旦遇限制，不另开进程继续更高档。
SONEXA_QQ_RATE_LIVE=1 go test -v ./internal/app -run '^TestLiveQQRateSamples$' -count=1
```

元数据入口未提供回退样例或样例实际未触发回退时明确失败，不标成已验通；可保留已完成的
搜索/单碟/多碟摘要，待有真实样例再补证据。取消校验不发送额外请求。完整线上验收仍单独跟踪。

## 版本控制

提交手写源码、测试、测试数据、依赖锁文件、平台资源及项目工具配置。不提交依赖目录、构建产物、Wails 生成绑定、`package.json.md5`、测试报告、数据库、本机配置和 `.env`；规则见 `.gitignore`。

临时资料放 `tmp/`，临时 Go 程序放 `tmp/_scratch/`，避免被 Go 的 `./...` 扫描。Git 忽略规则不控制 Go 包发现。
