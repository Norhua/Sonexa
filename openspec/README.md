# Sonexa 规格

本目录描述当前应用的行为与设计。阅读顺序：

- `specs/`：按功能划分的行为规格，使用 OpenSpec 的 Requirement / Scenario 格式。
- [design.md](design.md)：技术栈、模块职责与数据模型。
- [known-gaps.md](known-gaps.md)：仍存在的问题、未完成目标及验收条件。这里的目标不代表当前已实现。
- [verification.md](verification.md)：最近一次文档与实现核对的范围和结果。
- `changes/archive/`：历史规划与验收记录，不作为当前实现依据。

## 术语

- **曲目（track）**：用户收藏的一首歌，不依赖本地音频文件。
- **歌集（release）**：一次具体发行，可为专辑、EP、单曲等；共享标题、发行信息与封面。
- **发行组（release group）**：本地管理的多个具体发行成员，可保存多来源组身份。所有具体发行都有本地组；无发行信息的曲目可不关联发行。
- **署名（credit）**：上游提供的艺术家名称文本；署名成员与演唱、作曲等参与角色分别记录。
- **候选**：上游搜索返回的曲目记录，尚不代表用户已收藏。
- **清单 / 灰色行**：上游歌集的曲目位置；未收藏的位置在专辑中以灰色行显示。
- **匹配 / 补全**：匹配确定上游曲目身份；补全按已有身份获取更多信息。

## 功能索引

| 范围 | 规格 |
| --- | --- |
| 平台 | [界面与交互](specs/platform/app-shell/spec.md) · [数据存储](specs/platform/data-store/spec.md) · [设置](specs/platform/settings/spec.md) |
| 资料库 | [曲目](specs/library/track-library/spec.md) · [艺术家](specs/library/artists/spec.md) · [浏览视图](specs/library/browse-views/spec.md) · [歌集清单](specs/library/release-catalog/spec.md) · [发行归组](specs/library/release-grouping/spec.md) · [统一准入](specs/library/admission/spec.md) |
| 组织与检索 | [歌单与标签](specs/library/collections/spec.md) · [本地搜索](specs/library/search/spec.md) · [导出](specs/library/export/spec.md) |
| 匹配 | [文本处理](specs/matching/text-processing/spec.md) · [评分](specs/matching/confidence/spec.md) · [处理流程](specs/matching/pipeline/spec.md) · [匹配来源](specs/matching/provenance/spec.md) |
| 上游 | [统一接口](specs/upstream/provider-registry/spec.md) · [MusicBrainz](specs/upstream/musicbrainz/spec.md) · [iTunes](specs/upstream/itunes/spec.md) · [QQ 音乐](specs/upstream/qqmusic/spec.md) · [封面](specs/upstream/cover-art/spec.md) · [封面搜索](specs/upstream/cover-search/spec.md) · [上游搜索](specs/upstream/search/spec.md) |
| 操作流程 | [表格导入](specs/import/table-import/spec.md) · [人工复核](specs/review/review-queue/spec.md) · [曲目编辑](specs/editing/track-editing/spec.md) |

## 维护约定

使用常见中文，代码名、协议名和界面文案保留原名。同一规则只在所属规格中完整定义，其他文档引用它。已解决的问题从现行说明中删除；仍存在的问题记录具体调用路径和验收条件，不用测试通过代替功能核验。历史决策通过 Git 和归档查询。
