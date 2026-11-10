# 1 — 修改原因

## 原始提示词

> 关于 change 目录中的 change_record 目录：
> 因为 openspec update 没有很好的办法记录更新前做了什么，所以在 apply 后我们不再使用 openspec update，而是通过对应 change 中的 change_record 目录来记录后续修改，在 change_record 中有三类 md 文档，分别是：
> 1. spec-diff.md 只有一份：修改阶段可能通过多次 ai 会话完成，他们共同维护 spec-diff.md，spec-diff.md 记录代码实现与 spec 文档的差异。这个文档用于后续 archive change 时同步到主 spec 文档保持文档和代码实现的一致性
> 2. <一个从 1 开始递增的编号>-why.md: 每次修改前创建，记录：原始提示词，你的追问和提供的选项 (包括选项的解释) 和我的选择，修改目的
> 3. <一个从 1 开始递增的编号>-tasks.md: 和 openspec 的 tasks.md 作用相同，用来记录并同步执行计划
>
> 当前 add-streaming-playlist-sync 已经基本完成，但是存在以下问题：
> 1. qq 同步歌单基本不可用，对于"https://i2.y.qq.com/n3/other/pages/details/playlist.html?platform=11&appshare=android_qq&appversion=20090508&hosteuin=NK6zNe4FoKCF&id=9385819575&ADTAG=wxfshare"直接输入链接提示"请输入 QQ 官方歌单链接或数字 ID"。如果输入 ID"9385819575"，则会在读取歌单的过程中提示"读取期间 QQ 歌单发生变化"，同样失败
> 2. 对于 spotify，则提示 Spotify 运行环境缺失、损坏或版本不匹配。我是"~/.go/bin/wails build"后使用的，如果需要其他操作来获取 python 库，请你提供一个 makefile，或者一个 shell 脚本和 bat 脚本，可以使 windows，linux，macos 的用户一键编译并获取依赖

## 追问、选项及选择

### 追问 1：普通歌曲被上游明确过滤时的策略

追问：QQ 对这些有明确歌曲 ID、但不返回详情的过滤项，应采用哪种同步策略？

1. **允许明确报告的过滤项，保护已有条目（推荐）**：同步接口实际返回的歌曲，预览中明确显示被过滤歌曲的数量和原因。刷新时若过滤项对应已有同步歌曲，保留旧条目，不将其误判为上游删除；首次绑定的过滤项不进入导入队列。此行为记入 spec-diff。
2. **维持严格完整性，过滤即失败**：只要接口过滤了普通歌曲就拒绝本次同步，并改为明确提示过滤数量，而不是“歌单发生变化”。用户歌单仍无法完成同步，但完全保持现有完整性规则。

用户选择：**1 — 允许明确报告的过滤项，保护已有条目（推荐）**。

据此扩展本次计划：通用快照排除项可带稳定歌曲身份和未知位置，差异计算保护该身份的已有条目；QQ 使用所有普通/过滤歌曲均具有的数字歌曲 ID 作为同步身份（不影响元数据 MID 绑定）。

## 修改目的及边界

- 修复官方 QQ 移动分享链接解析，并依据真实分页响应修正误判；保留不完整快照不可提交、来源限制即停止、无任意 URL 请求等保护。
- 提供 macOS/Linux 的 shell 与 Windows 的 bat 入口，自动获取校验过的构建依赖、冻结 Spotify helper、构建 Wails 主应用并整体装配资源；用户运行应用仍不需要 Python 或在线安装。
- 按原生平台构建，不假装跨平台冻结可行；明确区分自动化支持范围与本机会话实际验收范围。
- 本次仅维护 change_record 后续记录，不运行 openspec update，不改写原 proposal/design/specs/tasks，不提前同步主规格或归档。
- 保留会话开始前 go.mod 的现存改动，不纳入本次提交；不清空应用数据。

## 有界只读排查结论

用户歌单 `9385819575`：前 3 个原始百项页面各返回 99 首普通歌曲及 1 个 `filtered_song`，页面 `total_song_num=1066`；最后页无过滤时返回 `total_song_num=1067`。一次请求全量返回 1064 首及 3 个过滤 ID，`total_song_num=1064`。因此总数是扣除**当前页**过滤项的结果，必须加回本页明确过滤数量后比较原始总数；偏移按原始计数而非可见歌曲数推进。过滤项没有原始位置，不能虚构位置。`dirinfo.title` 实际各页一致，最初对名称的怀疑未证实。

原始响应仅保存在忽略目录 `tmp/_scratch/qq-playlist-fix/`，不提交用户歌单内容或账户字段。保留脱敏合成回归样本。另一次 qzone 诊断仅返回无成员结构的响应，不采用该端点，不加入自动回退。

## 初步排查

- `ParsePlaylist` 仅允许 `y.qq.com`，没有 `i2.y.qq.com/n3/other/pages/details/playlist.html`。
- QQ 把每页 `dirinfo.title` 与首页强制比较；须对照真实后续页确认字段含义。
- 普通 `wails build` 不会装配 helper；现有脚本仅锁定 macOS arm64，其他平台的准备、许可证目标、验证和产物定位尚未贯通。
