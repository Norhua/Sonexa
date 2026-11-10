# 实现与规格差异（共享维护）

此文件由 apply 后所有修改会话共同维护。原 change 的 proposal/design/specs/tasks 保留规划基线；归档时须将 change delta 与本文件最终行为合并同步主规格，不把中间状态重复叠加。

## 修改 1：QQ 读取与完整构建

依据：[1-why.md](1-why.md) 中原始提示词及用户对过滤策略的明确选择。实现/验证见 [1-tasks.md](1-tasks.md)。

### `upstream/playlist-read` — 官方输入（实现修复/补充场景）

现有“支持 QQ 官方歌单详情链接”的要求不变，补充场景：

- WHEN 提交 `https://i2.y.qq.com/n3/other/pages/details/playlist.html?...&id=9385819575&...`
- THEN 离线解析为 `qq / 9385819575`，分享参数不影响去重；不请求用户 URL，不放行任意 QQ 子域名/路径，仍拒绝用户信息、端口、片段、重复或冲突 ID 参数。

### `upstream/playlist-read` — 完整性先于同步（用户批准的行为修改）

原要求仅容许可核对原始位置的非歌曲排除项，并规定歌曲缺失使整轮失败。修改为：

- 来源必须核对所有原始条目数量及分页覆盖；允许来源**明确报告稳定歌曲身份和已知类型**、但不提供歌曲详情的过滤项作为 `unavailable_track` 交付，预览明确显示数量、歌曲身份及原因；不得静默丢弃。
- 过滤项缺少原始位置时如实表示未知（协议 `Position=0`），不虚构原始位置；可读歌曲维持相对顺序。`TrackCount + ExcludedCount = RawSeen = DeclaredTotal` 仍须成立。
- 未知过滤类型、缺少稳定身份、无法解释的过滤计数、分页短缺/重复、真实总数/名称变化、业务限制、超限或取消仍整轮失败，旧快照不变。
- 首次绑定仅为可读歌曲建立导入需求，过滤项不补造元数据、不入队。

QQ 适配实现细节（不是业务中的平台分发）：

- 固定 CgiGetDiss / NoStore / 每页 100，不加入自动端点回退。
- `total_song_num` 为扣掉**当前页** `filtered_song` 的值；加回本页过滤数后核对原始总数，`song_begin` 依原始项数推进。
- 普通歌曲以 `id/songid` 数字 ID 为同步 TrackRef，过滤项用 `songid`；仅 type 13 的明确歌曲过滤项可报告。
- 元数据 MID 绑定、导入匹配与人工接受语义不变。旧开发数据的 MID 同步键不做迁移；按项目早期数据重置约定，测试前清空旧数据或重建绑定，脚本不自行清空。

新增验收场景：同一歌单各页分别过滤 1/0 首，原始总数稳定时不误报“歌单变化”；1067 项示例交付 1064 首 + 3 个明确过滤项，而非把 1064 当作无过滤的完整成员集。

### `library/synced-playlists` — 已过滤歌曲不生成删除（新增保护场景）

- WHEN 新快照包含稳定歌曲身份的明确过滤项
- THEN 通用 Diff 按 `TrackRef` 保护该身份所有既有出现项，不将它们分类为上游删除；保持最后已确认字段、位置、upstream_present、local_state、track 引用及活动导入需求。
- THEN 不把历史缺席项自动恢复、不恢复 ignored、不创建新导入；重复出现无法定位具体副本时保守保护已有所有副本。
- WHEN 后续该身份可读
- THEN 按原有出现键复用；WHEN 后续已确认完整快照既不含该歌曲也不含对应过滤项，THEN 正常应用原删除策略。

过滤项的稳定身份由各 provider 提供，业务不解释 QQ 码、数字 ID 规则或 `switch source`。

### `platform/app-shell` / `library/synced-playlists` — 预览提示（补充场景）

绑定及刷新差异预览显示排除数量、稳定歌曲身份和来源原因。原位置未知显示“原始位置未知”，不显示“位置 0”；多个无位置排除项均可展示。

### `platform/sidecar-runtime` — 显式完整构建入口（补充构建要求）

原有“随应用交付、无运行时安装、固定路径、版本/完整性校验”的要求不变，增加：

- 仓库提供 macOS/Linux 的 `build.sh` 和 Windows 的 `build.bat`，共享 `scripts/build.py`。在已安装 Go、Node/npm、Python 3.14+ 和原生系统工具链后，一次命令自动准备哈希锁定 CPython/wheel、冻结 helper、前端/Go 依赖、Wails 编译、许可/资源装配和冻结自检；自动检测 `~/.go/bin/wails`，缺 CLI 时按 go.mod 版本安装到私有构建目录。
- macOS/Windows/Linux glibc 各 amd64/arm64 六个原生目标使用固定归档锁；拒绝交叉冻结。`--offline` 仅使用已有校验缓存，缺失即失败。
- 完整输出为 `build/release/<os>-<arch>/sonexa.app` 或 `sonexa/`，平铺产物包含 `resources/spotify-helper/`；不得只分发主可执行文件。
- 普通 `wails build` 仍只构建主应用，缺 helper 时明确不可用，不回退系统解释器；文档必须明确推荐完整入口。
- 自检在无 PATH 工具、无效 Python 注入变量、中文/空格路径下启动冻结 helper 并校验资源未变。macOS 另有 OS sandbox 和 ad-hoc 签名检查；Windows/Linux 无 OS 沙箱保证，未做原生验收的平台不得宣称发行通过。
- 不自动安装系统 SDK，不生成/声称已验收 NSIS/AppImage/DMG 或发行签名、公证。

### 归档注意

本轮有 macOS arm64 完整构建/自检、QQ 只读大歌单及离线回归证据；没有 Spotify 真实在线歌单、真实已观察增删、Windows/Linux 原生执行或原生 UI 完整验收。不要因此勾选原 change 4.2/4.3/11.4/11.5 的全部验收，归档前仍需逐项处理这些边界。
