## 1. 差异整理

- [x] 1.1 核对 D01–D08、两份计划及对应实现，生成覆盖 9 个能力的 delta，并以 `openspec validate sync-lyrics-and-drawer-specs --strict` 验证。
- [x] 1.2 在设计中明确歌词与元数据、立即保存与草稿的边界，保留协议限制及证据来源；检查 proposal/specs/design 范围一致。

## 2. 主规格同步

- [x] 2.1 将 status 提供的全部 delta 合并到主规格，保留未修改需求和既有场景；以逐项文本比对及 `openspec validate --specs --strict` 验证。
- [x] 2.2 更新索引、架构、已知问题和核对记录，逐项列出 D01–D08 去向；检查现行文档链接及旧冲突表述。

## 3. 验证与清理

- [x] 3.1 运行 Go 与前端离线测试、Go vet，记录本次结果；将原计划的网络、浏览器、Wails 结果单独标为历史证据，保留既有失败与原生验证边界。
- [x] 3.2 验证主规格与 delta 一致、`git diff --check` 通过后删除 `change_record`，确认目录不存在且修改仅涉及文档；提交中文约定式 Git commit。
