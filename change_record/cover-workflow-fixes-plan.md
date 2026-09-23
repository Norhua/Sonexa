# 封面流程修正：修改计划与实施记录

日期：2026-09-24
关联变更：`openspec/changes/revise-album-cover-workflow`（本次未修改任何 spec 文档）

## 0. Review 结论（修改前的问题定位）

| # | 问题 | 定位到的原因 |
|---|------|-------------|
| 1 | CAA 取图慢 | ① 封面池默认并发 8、硬上限 16，而 CAA/archive.org 不限制并发；② 图片传输克隆 `http.DefaultTransport` 后 `MaxIdleConnsPerHost` 仍为默认 2，并发取图时大部分请求每次重新 TLS 握手；③ 每张图都走 `coverartarchive.org → archive.org/download → ia*.archive.org` 两跳重定向，实测慢网络下每跳 2–5 秒；④ 封面搜索的 CAA 适配器**串行**读取最多 10 个发行目录；⑤ 单次请求总超时 30s，慢网络下取大原图容易超时。 |
| 2a | 163 总是返回空 | 请求本身可用，但把「歌曲名 + 艺术家 + 专辑名」拼成一个词去做**专辑搜索**（type=10），专辑名里不含歌曲名，于是 `albumCount:0`。 |
| 2b | QQ 总是报错 | `c.y.qq.com/soso/fcgi-bin/client_search_cp` 已下线，任何查询都返回 HTTP 500（与 known-gaps G14 一致）。 |
| 3 | 缺少清除封面 | 功能未实现。 |
| 4 | 目录名 | 现为 `albums/release-<id>/`。 |
| 5 | iTunes 返回不相关内容 | iTunes `entity=song` 对词项宽松召回（如仅凭 `AKB48` 召回整个团体的单曲），结果未经任何相关性过滤。 |

关于低/高质量图片规则的澄清：已对照 `internal/covers/resolver.go`，现有实现与澄清后的规则一致（缩略图 `<600px`、高清 `>800px`；CAA 目录持久化代表「已从上游获取过」；目录确认无图时回退本地最小/最大图，本地无图为 nil；高清只下 1200，缺失时下原图）。因此本次**未改动**解析顺序，只在其前面加了「已清除」判定（见 3）。

## 1. CAA 性能

- `internal/covers/pool.go`：`DefaultConcurrency` 8 → 32，`MaxConcurrency` 16 → 64；HTTP 客户端超时 30s → 60s。
- `internal/config/settings.go`：`cache.coverConcurrency` 默认 8 → 32。
- `frontend/src/views/SettingsView.vue`：并发输入上限 16 → 64，说明文字改为「CAA 不限制并发；网络较慢时可调高，上限 64」。
- `internal/covers/transport.go`：图片传输 `MaxIdleConnsPerHost = MaxConcurrency`、`MaxIdleConns = 2×MaxConcurrency`、`ForceAttemptHTTP2 = true`，复用连接，避免重复握手。
- `internal/covers/resolver.go`：新增 `caaImageLadder`。下载 CAA 图片时先直连它最终会 307 到的 `https://archive.org/download/mbid-<release>/mbid-<release>-<imageId>[_thumb250|500|1200].<ext>`，少一跳重定向；直连失败再退回规范的 CAA 地址。只改写 `/release/` 路径（archive 条目按发行 MBID 命名）。
  - **CAA 目录（index JSON）仍走 coverartarchive.org**：实测 archive.org 对不存在的条目返回 503 而非 404，直连会把「确认无图」误记为故障。
- `internal/covers/sources/providers.go`：`CAASource.Search` 改为先收集固定目标与 MusicBrainz 召回的发行（≤10），再**并发**读取目录，结果按原顺序输出。
- 未改：图片传输仍禁用环境代理（SSRF 私网地址检查依赖直连解析）。如果用户网络必须经代理访问 archive.org，需要另行设计。

## 2. QQ / 网易云搜索（按用户提供的线索实现并真实验证）

- QQ：`POST https://u.y.qq.com/cgi-bin/musicu.fcg`，`music.search.SearchCgiService` / `DoSearchForQQMusicDesktop`，`search_type=0`（歌曲）。解析 `req.data.body.song.list[]` 的 `name`、`singer[].name`、`album.mid/name`，按专辑 MID 去重。封面 `https://y.qq.com/music/photo_new/T002R{size}x{size}M000{mid}.jpg`，档位 300/500/800/1200（都是构造档位，1200 不一定存在，采用时遇 404 逐级回退）。
- 网易云：`POST https://music.163.com/weapi/cloudsearch/pc`，明文 `{"s","type":"1","limit","offset"}` 经标准 WEAPI 加密（AES-128-CBC 预设密钥 + 随机 16 字节第二密钥，RSA 逆序无填充生成 `encSecKey`），新文件 `internal/covers/sources/weapi.go`。解析 `result.songs[]` 的 `name`、`ar[].name`、`al.id/name/picUrl`。用户上传歌曲的 `al.id` 为 0，此时以图片 id（`pic-<picId>`）作为来源身份。去掉 picUrl 原有查询参数后作为原图，另构造 250/500 缩放档。
- 检索词：`歌曲名（缺失时用专辑名）+ 艺术家`；歌曲级结果多取 30 条，过滤并去重后截到请求上限。
- 重定向白名单 `c.y.qq.com` → `u.y.qq.com`。
- 均无需登录或 Cookie，也未使用代理或伪装。真实验证：`SONEXA_LIVE_COVERS=1 go test ./internal/covers/sources -run TestLiveCoverSources`，查询「世界中の雨 / チームドラゴン from AKB48 / 心の羽根」，两个来源都通过：QQ 取到专辑 `004BBPXb2Nxd4l`，请求 800 档实际为 500×500；163 取到 240×240 原图。

## 3. 清除封面

- 存储（`internal/store`）：
  - `release_cover_selection.image_id / variant_id` 改为可空（直接改建表语句，按 AGENTS.md 不考虑兼容）。image 为空表示「已清除」。
  - `CoverSelection.Cleared`。
  - `ClearCoverSelection(releaseID, expectedRevision)`：在同一事务中先写取锁、校验 revision，删除该歌集的全部 `cover_file` 记录，写入空采用并使 revision +1，同时置空旧版 `release.cover_path/cover_source_url/cover_width/cover_height`。
  - `ladder.go`：「有封面」判据忽略已清除的采用关系。
- 文件（`internal/covers/library_images.go`）：`ClearRelease` 在数据库提交后删除这些文件，并尝试删除空的歌集目录。删除失败只会留下无引用的孤儿文件，同时返回提示。临时预览区（`cache/sources/…`）不删。
- 解析（`resolver.go`）：已清除的歌集直接返回 `empty`（界面 nil），不复用预览、不自动查 CAA，直到用户重新采用。重新采用沿用 revision 校验。
- 绑定：`App.ClearCover(releaseID, revision)`，成功后发出 `cover:changed`，各处按新 revision 刷新。
- 前端：详情抽屉封面右键菜单新增顶层「清除封面」（与「更新歌曲图片」之间有分隔线）；无所属歌集时禁用；执行前弹出危险确认框。

## 4. 目录名

- `internal/covers/files.go`：歌集目录改为 `albums/release-<id>-<歌集名>`。歌集名中的路径和保留字符 ``/\:*?"<>|`` 及控制字符替换为 `_`，合并连续空白，截断到 80 个字符，去掉首尾的点和空格；清理后为空时退回 `albums/release-<id>`。
- 歌集改名后，新文件写入新目录；已登记的文件仍按数据库中的相对路径读取，不迁移。

## 5. 相关性过滤

- 新文件 `internal/covers/sources/relevance.go`，`Relevant(query, title, artist, album)`：
  - 比较前做 `matching.Normalize`（NFKC、小写、假名统一、去标点），并额外比较去括号后的版本（容忍「(劇場盤)」「(ドラゴンボール改ver.)」等后缀）；按包含关系匹配，长度 <2 的串只接受相等。
  - 专辑名匹配即保留；否则要求歌曲名匹配，并且（若给出）艺术家匹配。艺术家支持 `/、,&;` 分隔的多人署名。
  - 查询只有艺术家时按艺术家过滤；没有任何条件时不过滤。
- 适用于 iTunes、QQ、163。CAA 由 MusicBrainz 结构化搜索召回，不加过滤。

## 6. 测试

- 新增或更新：`sources/web_test.go`（新请求形状、WEAPI 表单、相关性过滤、夹具）、`sources/providers_test.go`（CAA 并发目录、iTunes 过滤）、`sources/live_test.go`、`covers/transport_test.go`（`caaImageLadder`）、`covers/files_test.go`（目录名）、`app/cover_clear_test.go`、`app/cover_search_test.go`、`config/paths_test.go`、e2e `cover-search.spec.ts`（清除封面确认与禁用）。
- 夹具 `testdata/golden/responses/covers/{qq,163}_search.json` 已替换为新接口的最小字段摘录。
- 结果：`go vet ./...`、`go test -race ./internal/...` 全部通过；前端 typecheck、37 项单元测试、build、Chromium+WebKit 104 项 e2e 全部通过；QQ/163 真实查询通过。
- 未做：在原生应用中人工确认 CAA 加载速度（只用 curl 测了单跳耗时）。
