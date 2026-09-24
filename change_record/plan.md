# 计划：QQ 音乐升级为标准元数据上游

> 日期：2026-09-24。依据：`docs/qqmusic-availability.md` 的调研结论及本次重新实测（search / GetAlbumDetail / GetAlbumSongList / get_song_detail / 歌词接口均匿名可用）。

## 目标

QQ 音乐不再只是封面搜索来源，而是与 MusicBrainz、iTunes 平级、实现完整 `provider.Provider` 接口的元数据上游：

- 可在上游搜索页、复核改搜、编辑重匹配中被选中（这些入口都从注册表动态渲染）；
- 候选可加入曲库，后台补全 / 手动刷新详情时写入：标题、署名艺术家、所有演唱（含不在署名中的虚拟歌姬等）、专辑、年份、音轨号、碟号、风格、专辑艺术家、作曲、作词、编曲、制作人、乐器演奏、厂牌、歌词；
- 以 QQ 为来源的歌集能自动取封面（album MID 可直接构造图片地址）。

非目标：导入阶段的自动匹配仍只用 MusicBrainz（现有设计）；封面搜索页的 QQ 来源保留原实现与独立配额；不做逐字歌词、翻译歌词。

## 设计

### 1. 传输层：`provider.Client` 支持 POST

`musicu.fcg` 只接受 POST JSON。给 `GetRequest` 增加 `Body []byte` 与 `Headers map[string]string`：`Body` 非空时发 POST（`Content-Type: application/json`），其余缓存、令牌桶、重试、出网门禁逻辑全部复用。缓存键由调用方把请求体纳入 `ParamsHash`。

### 2. 新包 `internal/provider/qqmusic`

| 项 | 取值 |
| --- | --- |
| ID / 显示名 | `qq` / `QQ 音乐` |
| 能力 | Composer、Lyricist、PerformerRoles、Label、PrimaryType、CoverArt、MultiSizeCover、Lyrics（新增能力位） |
| 速率 | 默认 30 次/分钟（调研建议间隔 ≥1.2s），可在设置中修改 |
| MatchRef | 歌曲 MID（如 `003RcjkV1XjQQy`） |
| ReleaseRef | 专辑 MID（如 `000BDJvT2aWQYZ`）；album ID=0 / MID 为空的搜索结果不给 ReleaseRef |

- **SearchTracks**：`music.search.SearchCgiService / DoSearchForQQMusicDesktop`。标题用 `title`（保留版本信息），署名为 `singer[].name` 以 ` / ` 连接，时长 `interval` 秒→毫秒，年份 `time_public`，缩略图用 300 档 QQ 图片地址。搜索结果的 `index_album` 可能是全专辑序号，因此候选只在第 1 碟（`index_cd==0`，此时全专辑序号与碟内序号一致）时给音轨号，碟号 = `index_cd+1`。
- **FetchDetail**：一次 POST 合并三个请求块（`album` GetAlbumDetail、`songs` GetAlbumSongList 每页 100、`song` get_song_detail），专辑超过 100 首时继续分页取清单。
  - 清单：数组顺序不是曲序，按 `(index_cd, index_album)` 排序；碟号 = `index_cd+1`；碟内序号为按碟分组后的排名（派生值）；多碟时把 `index_album` 原值记入 `TrackNumberText` 以便追溯。
  - 标题、署名（带结构化 `Credits` 区间，避免按标点再拆乐队名）、专辑名、专辑署名（`singer.singerList`）、发行日期 / 年份、类型（录音室专辑→Album、EP→EP、单曲→Single，其余不映射）、厂牌 `company.name`、风格（info「歌曲流派」，缺失时用专辑 `genre`）。
  - `recordNum` **不**映射为目录号（语义未核实）。
  - 参与者 `info[]` 按标题映射角色：演唱者→vocal、合声→vocal(attribute=合声)、作词→lyricist、作曲→composer、编曲→arranger、制作人→producer、吉他/贝斯/鼓/键盘/钢琴/弦乐等乐器→instrument(attribute=乐器名)；合声编写、录音师、混音工程师等无对应角色的条目不写入。专辑艺人→album_artist。**不**把 `singer[]` 推断为演唱者。
  - 歌词：优先取 get_song_detail 中「歌词」条目；为空时回退独立歌词接口 `fcg_query_lyric_new.fcg?format=json&nobase64=1`，校验 `retcode/code/subcode`。
  - 顶层 `code` 与每个块的 `code` 都必须为 0，否则报错；目标曲目不在清单中时报错。
- **SearchCovers**：由搜索候选按专辑 MID 去重，构造 300/500/800/1200 档位（Constructed）。

### 3. `provider.Detail` 与存储

- `Detail` 新增 `Lyrics *string`；能力新增 `CapLyrics`。
- `track` 表新增 `lyrics TEXT` 列（新迁移 0003；早期项目不考虑旧库兼容）。`WriteDetailColumns` 写入；`GetMetadata` 读出；详情抽屉只读展示（可折叠）。
- 匹配标识校验 `ValidateMatchRef` 增加 qq：`^[0-9A-Za-z]{10,20}$`。

### 4. 封面

- 抽出 `store.QQCoverImages(mid, title, artist)`，封面搜索 `sources.ParseQQ` 与自动取图共用同一图片身份和档位。
- `covers.Service` 新增 QQ 目录：`provider=qq` 且 ref 为合法 MID 时直接登记构造目录（无需请求元数据接口），自动下载；缩略图档优先 300、详情优先 800/1200 再退 500。
- `localFiles`、`coverLadder`、封面搜索的固定目标都补上 qq 分支。

### 5. 应用接线与设置

- `config.Settings` 新增 `QQMusic{RatePerMin}`，默认上游优先级 `musicbrainz, itunes, qq`。
- `rebuildProviders` 注册 QQ provider。
- 设置页「上游」分组增加 QQ 速率输入；详情抽屉的「无角色信息」提示与歌词展示；前端类型绑定同步。

### 6. 测试

- `qqmusic` 包：基于 `testdata/` 真实响应的离线解析测试（搜索、单碟详情、多碟排序/碟内序号、角色映射、歌词回退、错误码）；`httptest` 验证 POST 与合并请求。
- `provider.Client` POST 测试；`ValidateMatchRef` qq 用例；存储歌词写入；封面 QQ 目录自动取图用例。
- `go test ./...`、`go vet`、前端 `vue-tsc` / 单测。

### 7. 规格差异

执行中与现有 openspec 的差异、冲突写入 `change_record/spec-diff.md`，不直接修改 spec。

## 提交节奏

1. `docs(change_record)`: 计划
2. `feat(provider)`: Client POST + qqmusic provider + 测试
3. `feat(store)` / `feat(covers)`: 歌词列、QQ 自动封面
4. `feat(app)` + 前端：接线、设置、详情展示
5. `docs(change_record)`: spec-diff
