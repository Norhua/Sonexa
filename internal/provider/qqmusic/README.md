# QQ 音乐上游

元数据、封面、歌词及公开歌单由同一 Provider 提供，复用请求客户端与 QQ 平台桶。

## 公开歌单读取

使用固定 `musicu.fcg` 的 `music.srfDissInfo.DissInfo / CgiGetDiss`，请求 `disstid / song_begin / song_num`，每页 100 项。沿用既有 musicu envelope 和请求块 code 校验。首次获取与刷新都使用 `NoStore`，不读写响应缓存；链接只解析规范 ID，从不直接请求用户 URL。

输入限制为数字 ID、`y.qq.com` 的明确歌单详情路径（ryqq playlist、taoge 和旧 playlist.html），以及 `i2.y.qq.com/n3/other/pages/details/playlist.html?id=...` 移动分享形态。只对这些精确主机/路径放行，不允许任意 QQ 子域名。拒绝错误主机、用户信息、端口、非歌单路径、非法数字与冲突身份参数；不复制 demo 的“寻找任意长数字”兜底。

严格响应字段为 `dirinfo.title / disstid（或 dirinfo.id）/ total_song_num / songlist`。CgiGetDiss 的 `total_song_num` 扣除的是**当前页** `filtered_song` 数，而非整单过滤数；因此先加回本页过滤数得到原始总数，再跨页比较，偏移按可读项 + 过滤项推进。检查每页原始覆盖（最多 100）、`songlist_size`/`hasmore`（若提供）、重复页、名称和身份。未知身份、缺列表、真正总数变化、不明过滤类型、`invalid_song` 等仍使整轮失败。

用户明确允许 ID 已知、类型为 13 的 `filtered_song` 作为 `unavailable_track` 报告。过滤项不返回原始位置，因此 Position=0 表示未知，绝不伪造位置；可读歌曲保持相对顺序。同步歌曲身份统一为新旧 `id/songid` 数字 ID，供可读/过滤项互认，不以 MID 猜测。首次绑定只导入可读项；刷新保护过滤身份的全部既有出现项（包括 pending/ignored/imported），不删除、不覆盖、不恢复忽略/缺席状态，直到可读或在后续完整快照中明确不再出现。数量不明仍拒绝，不补造歌曲详情。

明确零原始总数与空列表才是成功空歌单。基础字段兼容 `name/songname`、专辑字段，未知信息保持 nil，署名保持顺序，秒转毫秒。

不使用 qzone/v8 或任何异常触发的端点轮换；HTTP 鉴权/限流和非零业务 code 立即停止。临时 HTTP 错误最多两次额外尝试；所有发送共用原 QQ 桶、动态门禁和 500 次预算，总期限 15 分钟、最多 200 页/20,000 项，并限制累计响应字节。

成员键为歌曲 Ref + 出现序号，重排不改变出现身份；相同歌曲两个副本之间不能判断具体删除了哪个。缺字段不生成占位，不把 QQ 歌曲 Ref 当作已接受的元数据绑定。

**验证边界：** 用户提供的公开歌单 `9385819575` 已经生产 Provider 只读验证：11 页、1064 首可读、3 项过滤、1067 个原始项；未写入应用数据库。默认关闭的 `SONEXA_QQ_PLAYLIST_LIVE=<公开歌单ID>` + `go test -v ./internal/provider/qqmusic -run '^TestPlaylistLiveReadOnly$' -count=1` 可复验。接口诊断原始响应不提交；离线合成 fixture 复现各页局部扣数。此结果不代表真实增删/UI 全流程验收，记录见 change_record/1-tasks.md。
