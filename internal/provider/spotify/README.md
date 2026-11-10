# Spotify 只读歌单上游

Spotify 只实现 `playlists.Provider`，不进入元数据/封面/歌词注册表。`app/upstreams.go` 与 QQ 一起登记，QQ 复用原元数据实例。注册构造不读盘、不出网、不重配桶；来源可用性读取时才检查 helper，缺失不阻止其他能力。

## 读取与安全边界

- 官方 HTTPS playlist URL 或 22 位 ID 离线解析；分享参数不参与身份。Spotify 曲目 Ref 仅为同步身份，不写成元数据接受绑定。
- 所有请求由 Python 通过 framed stdio 交给 Go。Go 只允许当前歌单 embed、锁定 pathfinder 操作，以及已在本轮页面观察到的 track 详情；拒绝 Cookie、任意 header、任意主机/方法/路径、其他 track 与其他持久化查询哈希。
- 重定向最多 4 次，只允许同 HTTPS 主机、路径和等价查询；每次实际发送重新经过当前门禁、发送预算与长期 Spotify 桶。新桶为每秒 1 次、突发 1，重新装配或 helper 重启不重置。标准 Go HTTP transport 禁用环境代理；测试可注入明确 HTTP 替身。
- 绑定/刷新请求不使用缓存。临时错误最多 2 次额外尝试，401/403/429、GraphQL errors 直接失败，不换 token、端点或凭据。
- App 持有独立 `playlists.Fetcher`：总并发 2、每平台 1，退出先取消等待再关闭存储。请求总期 15 分钟、HTTP 30 秒、500 次实际发送、200 页/20,000 项；单响应 8 MiB、累计响应 256 MiB、单帧/快照 32 MiB、helper stdout 总量 64 MiB、stderr 留存 64 KiB。失败不返回可提交快照。

## 辅助程序

资源位置来自应用二进制，不从 PATH/cwd/用户输入查找：macOS bundle 的 `Contents/Resources/spotify-helper`、Wails 开发 `build/spotify-helper/dist/spotify-helper`、其他安装目录的 `resources/spotify-helper`。清单逐文件 SHA-256、版本与架构核验，拒绝逃逸路径和未列出的文件。完整性摘要不替代平台签名。

固定参数数组启动，不执行 shell、用户 Python、pip/uv 或运行时安装；过滤 Python、动态库注入、PyInstaller 和代理环境。stdio 验证版本、request ID、顺序和消息类型，帧头／正文短写直接失败。结果必须随后正常 EOF/退出，尾部输出、崩溃或退出阻塞不能变成功；退出等待最多 3 秒，Runner 自身也设置 15 分钟总期限。错误退出先回收整个 Job／进程组再等待，避免后代持有通信管道拖延退出；返回前等待取消监视 goroutine 结束。

Unix 独立进程组，取消/错误清理整个组；Windows 创建 suspended helper，先加入 kill-on-close Job Object 再恢复主线程，取消时关闭 Job。Windows 实现目前只完成交叉编译，未声称运行验收。Python 源码、冻结步骤、许可证及私有 API 风险见 [sidecar/README.md](sidecar/README.md)。

## 验证记录（2026-11-09）

分组 Go/race 定向测试覆盖消费契约、装配不污染其他能力、旧/新配额、HTTP 预算/限制、共享 fixture、错误帧、标识、损坏/错误架构/额外文件、空 PATH、异常退出、取消及 Unix 后代进程回收；14 项 Python 离线测试通过。前端架构边界 2 项通过。

```bash
# 本批后端完整包回归（结果见本次提交说明）
go test -count=1 -race ./internal/playlists ./internal/provider ./internal/provider/qqmusic ./internal/provider/spotify
# 已构建 helper 后，真实子进程 + Go HTTP 替身，不访问 Spotify
SONEXA_TEST_FROZEN_HELPER=1 go test -count=1 -race ./internal/provider/spotify -run '^TestFrozenHelperOfflineIntegration$' -v
# 只验证 Windows 源码可编译，不运行
GOOS=windows GOARCH=amd64 go test -c ./internal/provider/spotify -o tmp/_scratch/spotify-audit/spotify-windows.test.exe
```

真实冻结 helper 往返已验证明确空歌单和 205 项三页（含同 track 缺字段补取），全部网络调用经过 Go 替身。数据模型、同步计划、导入投递及 App/UI 已接入；并发／重启和浏览器结果见[第 11 组验收记录](../../../openspec/changes/add-streaming-playlist-sync/implementation-phase11-offline.md)。

后续已补 Windows 原生树／句柄测试（amd64/arm64 仅交叉编译），并通过 macOS arm64 干净检出、重新冻结与完整 `.app` 资源装配、自检及生产 Runner 定位往返，见[第 4 组构建验收](../../../openspec/changes/add-streaming-playlist-sync/implementation-phase4-packaging.md)。
显式构建入口为 sidecar 的 `package_app.py`；普通 `wails build` 不装配 helper。仍未做真实歌单、Windows Job Object 运行、发布签名／公证、实际安装或原生 UI 验收。
