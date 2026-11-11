# 同步歌单 API 与刷新事件

所有调用均通过 `@/api` 的 `App` 导出；来源来自后端能力列表，不在前端解析平台链接或启动 helper。

## 准备与提交

- `ListPlaylistSources`：返回已登记来源与运行环境可用性；禁网原因另读 `CheckOutbound`。
- `ListSyncedPlaylists`：绑定名称、规范身份、修订、获取/确认时间及已导入**出现项数**。
- `GetSyncedPlaylistEntries(id, query)`：仅当前绑定的 imported 出现项，包括上游缺席项；不截断为首页。
  `entryId` 是表格行 key，`trackId` 是本地实体；`upstreamPresent` 与 `inLibrary` 独立。
- `PreparePlaylistBinding(operationID, source, input, alias)`：返回 `preview` 或 `existingPlaylistId`，重复绑定无需解析错误字符串即可定位。
- `PreparePlaylistSync(operationID, playlistID)`：返回差异、忽略项及服务端计划 ID/hash/过期时间；不写库。
- `CommitPlaylistBinding` / `CommitPlaylistSync`：仅接收 ID/hash，后者另接收选中的忽略项 ID；不接受前端快照、目标或本地曲目替换。
  提交原子更新快照与导入需求，自动启动新批次；已提交计划重试不重复入队。
- `CancelPlaylistOperation`：接受调用方 operationID 或服务端 plan ID；关闭弹窗/切换上下文时取消。
- `DeleteSyncedPlaylist`：与提交串行，取消该绑定在途读取，清理其需求和引用；不是普通集合删除。
- `PrepareLibraryAddition(trackIDs)`：按 track 去重，返回 willAdd/alreadyInLibrary。
- `CommitLibraryAddition(id, hash)` / `CancelLibraryAddition(id)`：服务端保留原 ID 集合，提交重验当前归属和存在性。

预览、来源列表和查询返回的数组为空时仍是 `[]`；时间为 RFC3339 字符串，未曾入库的 `track.addedAt` 为空串。
候选 `localTrackId` 表示精确本地绑定，`inLibrary` 单独决定搜索动作；复核跳过依然由服务端重查来源 + track，不信任缓存本地 ID。
`releaseExists` / `releaseGroupExists` 分别表示候选同来源具体发行/原生组身份已存在于本地，包含同步专属数据。两者独立查询，不按标题或所属发行推测原生组，不把它们当作 track 命中或自动接受资格。复核来源列展示对应文字，普通搜索不增加这两个标记；读取队列和改搜返回实时值，不信任旧候选快照中的布尔值。

## 队列读模型

`GetReviewQueue` 保持完整条目/候选 DTO；后端按事实修订跳过无变化的离线整理，并批量解析输入/本地状态，不在前端伪造已接受结果。`GetTaskSummary` 只查询待处置数量，不传输候选快照。

`BatchStats.reviewEntered` 为本批次累计进入复核页（review/failed）的有效输入数；`processedOnce` 为累计已经处理过的有效输入数。两者持久化、重试与人工处置不清零，区别于随当前调度状态变化的 `processed`。历史列表百分数使用前两者；当前“复核中”继续使用 `stats.review`，“失败”使用 `stats.failed`。

## 事件及失效范围

- `library:changed`：既有通用本地实体失效通知，包含同步专属 track，不代表增加音乐库计数。
- `synced-playlists:changed`：`{ all, playlists: [{ id, revision }] }`。当前以 all=true 失效整个同步快照，覆盖删除绑定、共享字段及入库标记；修订只是提示，不直接覆盖状态。
- `import:queue-changed`：无载荷，重新读取批次、复核和汇总；不能拿一个批次的进度充当全局复核数。
- `import:progress`：保留既有批次进度/日志契约。

成功提交后立即通知，不等待可选网络补全；失败不发成功通知。复核读取触发离线重复整理时，只有实际成功投递才发事件，防止“读取→事件→读取”无限循环。
前端先订阅再读取，使用 refreshQueue 与上下文序号合并刷新；事件不是可直接应用的完整数据快照。

## 生命周期

App 保留一个歌单 service/fetcher；设置更新仅原子替换能力注册表，已有计划与抓取配额不丢失。
每次发送读取当前门禁，关闭出网额外取消在途读取；本地列表/已获取预览确认不依赖网络。
退出停止新绑定调用，取消并等待歌单 service、完整绑定调用和所有导入 Runner 代际，再关闭 Store。
平台 helper 的进程树/发行包验收仍独立见 tasks 4.2–4.4，不能用 App 替身测试代替。
