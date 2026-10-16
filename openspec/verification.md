# 文档与实现核对

日期：2026-10-16。范围：`unify-upstream-and-redesign-track-editor` 及测试阶段 `change_record` 中的最终决定。核对当前实现至 `38217fd`，不把早期规划、旧测试决定或已修复遗漏写成当前规则。

## 核对结果

主规格仍为 26 份：原变更的 16 个能力已在 2026-10-02 首次同步，本次检查全部既有 delta，并更新 15 份主规格及相关架构、README、已知问题；其余未受影响规则保持不变。16 份 delta 的 59 个需求与主规格逐项一致，未删除原主规格需求。

| 范围 | 最终规则与实现证据 |
| --- | --- |
| 编辑（ED-001～ED-004） | 左侧使用、精确完整预览自动补取和过期重确认、语义身份；未换上游可原地修改共享专辑，新建/换上游仍逐曲迁入。`EditDialog`、`useEditDraft`、`internal/library/editor_*`、`internal/store/editor_save.go` |
| 发行与浏览（LIB-001、LIB-002） | 清理无收藏发行及空组，保留独占曲版本；合并/拆分 group 仅改变展示，保留全部收藏 ID 和灰色行真实发行。`internal/store/release.go`、`AlbumsView`、`albumRows`、`trackRows` |
| 补全（LIB-003） | 只计当前 pending 工作集，暂停/重试延续，下一批或重启重置；入口仅全部曲目页有 pending 时显示。`enrich_worker`、`TrackTable`、enrich store |
| CV（ART-001、ART-002） | 完整 CV 加独立声优双关联，可靠 CV 当前演唱兜底有 `credit_cv` 来源，明确集合及用户清空优先；详情显示完整 CV，不改快照和真实角色。`cv_credit`、`credit_projection`、`cv_display`、真实响应摘录 `vocal_you.json` |
| 外壳（UI-001、UI-002） | 全局提示在侧栏设置上方；详情发行信息固定顺序，音轨分母取当前碟曲数，碟号分母取发行总碟数。`AppSidebar`、`GlobalNotice`、`DetailDrawer` |
| 状态刷新修复 | 详情、复核、艺术家、搜索、导入队列、全局待复核计数与逐首导入/补全通知已补齐；保留当前草稿/条件，隔离迟到响应，候选本地重判不自动出网。`refreshQueue`、相关 views/stores、`internal/app/reactivity_test.go`、importer runner |
| 原有基础约束 | 三层身份/当前值/快照、统一准入、精确身份唯一、用户字段保护、文件准备和原子引用、版本对应与独立成员批准继续适用 |

## 本次实际执行

- `openspec validate --specs --strict`：26/26 通过。
- `openspec validate unify-upstream-and-redesign-track-editor --strict`：归档前通过。
- 全部 delta 需求与主规格逐项比对：16 文件、59 需求一致；主文档链接目标检查及 `git diff --check` 通过。
- `go test ./internal/store ./internal/library ./internal/app`、`go vet ./...`：通过。
- `cd frontend && npm test`：13 个文件、58 项通过。

本次仅修改文档、归档和清理记录，未改业务代码，未重跑浏览器、Wails 构建或真实上游网络。

## 已有测试与人工验收

测试阶段最后一轮记录（2026-10-15）：`go test -count=1 -race ./internal/...`、`go vet ./...`、前端构建及 58 项单元通过；Chromium/WebKit 开发与生产产物两轮各 236 项通过。此前 Wails darwin/arm64 构建及编辑/CV/详情专项回归通过。详细范围和各轮证据摘要保存在[归档实施记录](changes/archive/2026-10-16-unify-upstream-and-redesign-track-editor/implementation.md)。

用户于 2026-10-16 明确确认人工验收已经完成并要求勾选归档，因此任务 8.3 已完成。此前代理原生操作因 Computer Use 未授权停止的记录仅作历史保留；本次没有新增原生截图、逐项实测结果或代替用户执行验收。

浏览器使用 Wails 替身，Go 回归使用隔离数据库及离线样本；真实上游最新复测、大曲库性能和旧模型保留数据升级仍不由这些测试或人工确认推定完成。Vite 主包大于 500 kB 的既有提示仍保留。G09 局部错误/出网按钮、G11 专门署名边界确认等剩余问题继续列在 [known-gaps.md](known-gaps.md)。

## 归档与记录追溯

变更归档至 `changes/archive/2026-10-16-unify-upstream-and-redesign-track-editor/`，主规格为当前行为依据。`change_record` 的最终决定及关键验证边界已吸收，目录删除后原文仍可从 Git 查询，例如：

```sh
git show 38217fd:change_record/spec-diff.md
git show 38217fd:change_record/1c752aca-plan.md
```
