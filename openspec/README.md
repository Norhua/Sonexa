# Sonexa 现行规格

## 文档权威与阅读顺序

- `specs/**/spec.md`：现行行为契约，已合并历史 v1 偏差记录第一至二十七节的确定裁决；同主题以后期条目为准。原记录已完成同步并从工作树删除，追溯方式见下方。
- [design.md](design.md)：当前技术栈、数据模型与实现约定，替代归档设计中的旧架构决策。
- [known-gaps.md](known-gaps.md)：仍未满足的要求、当前生产链路限制及建议回归。**不通过删除目标要求来掩盖实现缺陷。**
- [v1 文档核验](verification-v1.md)：本次归档/同步的覆盖、实测命令与验证边界。
- [历史变更](changes/archive/2026-09-19-add-sonexa-v1/README.md)：原始 proposal/design/tasks/specs 的封存快照，不再作为现行实现依据。

`add-sonexa-v1` 已在 2026-09-19 按用户确认归档。原任务为 182/182 勾选完成，但偏差记录明确记载仍有缺口；用户选择「同步后归档，保留已知缺口」。归档及严格格式校验不等于发布验收或所有链路已实现。

## 现行技术栈

Wails v2（Go + Vue 3 + TypeScript + Tailwind CSS v4 + Volt + Lucide Icons + 按需 Pinia）。Volt 使用本地组件源码与 unstyled PrimeVue；不再使用 shadcn-vue。技术版本与来源依据依赖锁文件、[Volt 组件说明](../frontend/src/volt/README.md)和[迁移记录](../frontend/docs/volt-migration.md)。

## 历史偏差的追溯

修复分支合并且规格吸收裁决后，不再维护 `change_record/` 或其 tmp 符号链接。现行约束看主 specs，未完成项看 known-gaps；如需核对下表的历史节号，可从合并提交读取原记录：

```sh
git show 65aafcc:change_record/v1-deviations.md
```

此命令读取 Git 历史，不依赖已删除的工作树文件。上方哈希已按清除自动生成文件后的历史更新。历史归档中的原始规划仍保留，不随文件整理重写。

## 能力索引与偏差映射

下表说明同步来源，不声称每条需求均有独立端到端验收。

| 能力 | 本次同步重点 | 偏差记录节 |
| --- | --- | --- |
| [platform/app-shell](specs/platform/app-shell/spec.md) | 导航、页头、文件列表选择、列偏好、标签子菜单、抽屉、模态 | 1、14、20、22–27 |
| [platform/data-store](specs/platform/data-store/spec.md) | 曲目/歌集实体、补全与复核成因、时间绑定、开发期 schema | 5、10、13、19 |
| [platform/settings](specs/platform/settings/spec.md) | 模态设置、主题、结构性高置信及参数接线边界 | 1.6、4.5、8、20、21.4 |
| [library/track-library](specs/library/track-library/spec.md) | 单表曲目、共享歌集、来源/锁、统一候选入库 | 18、19、24.4 |
| [library/artists](specs/library/artists/spec.md) | 署名/角色分离、合作名、CV 完整身份、无角色空态 | 19.2、24.3、25.2 |
| [library/browse-views](specs/library/browse-views/spec.md) | 名单成员、两级专辑分组、标签交集、固定标题 | 1.5、19、24、26 |
| [library/release-catalog](specs/library/release-catalog/spec.md) | 歌集实体、两态清单、代表版本与详情清单保存 | 19、22.1、23.1 |
| [library/collections](specs/library/collections/spec.md) | 保留歌单显式顺序、取消 UI 拖动重排 | 14、20.4、23.2 |
| [library/search](specs/library/search/spec.md) | 搜索页/就地/名单三种范围、四字段高级条件、标签交集 | 2、20.6、26.3、27.2 |
| [library/export](specs/library/export/spec.md) | 右键导出、当前列表范围、搜索结果快照 | 14.4、20.6、21.4 |
| [matching/text-processing](specs/matching/text-processing/spec.md) | 主干三级阶梯、无剥离扣分、两闸即停、CV 不拆实体 | 9、12、18、24.3 |
| [matching/confidence](specs/matching/confidence/spec.md) | 曲名版本、署名异写、旁证可比性、代表阶梯用途 | 9、10、12、18 |
| [matching/pipeline](specs/matching/pipeline/spec.md) | 唯一实体才入库、互斥统计、统一检索、后台调度、精确详情 | 10、13、18、23.1 |
| [matching/provenance](specs/matching/provenance/spec.md) | track 身份、灰色行入口、已有曲目可跳转、编辑缺口 | 18、20.6、21.3、23.1 |
| [upstream/provider-registry](specs/upstream/provider-registry/spec.md) | 单选注册表、两层重试、共享限流与交互优先 | 2、4.2、4.11、13.4 |
| [upstream/musicbrainz](specs/upstream/musicbrainz/spec.md) | OR 锚点、搜索 track 键、精确目标、实际角色、不补查 CV | 9、10.6、23.1、24.3 |
| [upstream/itunes](specs/upstream/itunes/spec.md) | 单选能力呈现、地区/track 精确定位、生命周期缺口 | 2、21.4、23.1 |
| [upstream/cover-art](specs/upstream/cover-art/spec.md) | 来源 URL、Front/索引回退、四态、共享歌集、lazy 修复 | 11、19.3、22.2、27 |
| [upstream/search](specs/upstream/search/spec.md) | 统一页单选、共同闸门、零同步详情入库 | 2、13、18、20.6 |
| [import/table-import](specs/import/table-import/spec.md) | 导入队列入口、中文列名、实际入队进度 | 3、4.3、4.9、10 |
| [review/review-queue](specs/review/review-queue/spec.md) | 零请求接受、恢复输入、移出队列、异步隔离、快照保真 | 4.7、13、15、18、24.4 |
| [editing/track-editing](specs/editing/track-editing/spec.md) | 歌集影响范围、隐藏来源徽标、保留保存/锁目标 | 19.5–19.6、21.3、24.4 |

## 历史边界

- 原始增量规格先完整同步，再移动到归档；之后仅修订主 specs，不倒改历史任务或伪造原始验收。
- 原四级变体、7 入库样例、跨发行组例外、CV 两实体、recording 跨版本拥有、歌单拖动、shadcn-vue 都不是当前契约。
- 现行十行匹配回归预期为 4 直接入库 / 3 多义复核 / 3 未匹配，不把历史 7/3 作为当前期望。
- 开发期 0002/0003 已并入 0001；后续正式版本仍需要迁移机制。本次没有迁移、清理或读取用户数据库。
- 后续变更应针对主 specs 创建增量，修复缺口时补生产入口回归并更新缺口状态，不仅修改任务勾选。
