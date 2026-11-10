# AGENTS.md

## 技术栈
当前项目使用的技术栈为: Wails v2 + Go；Vue 3 + TypeScript + Tailwind CSS v4 + Volt + Lucide Icons + Pinia

## Git
对于git使用你要注意:
  - 在合适的阶段git commit
  - commit 使用中文
  - commit包含一句话简介和详细信息，且简介要遵守"约定式提交规范"
  - 约定式提交规范为"<type>[optional scope]: <描述>", <type>[optional scope]" 部分应当使用英文

## 架构约束
业务代码与上游差异分离:
1. 业务包（如 lyrics、covers）自行声明所需的接口，通过依赖注入使用，不在业务代码中处理不同上游造成的差异（禁止 switch source 式分发）
2. 上游差异的处理放在各上游包（internal/provider/xxx）中，每个上游按自身能力实现对应的业务接口，返回约定的数据或提供约定的功能

## OpenSpec 后续修改记录

已 apply 的 change 后续不再通过 openspec update 改写规划基线，改用该 change 的 `change_record/`：
- `spec-diff.md` 只有一份，跨会话共同维护实现与规格的最终差异；archive 时与 change specs 一起同步主规格。
- 每次修改前新建 `<编号>-why.md`，编号从 1 递增，记录原始提示词、真实追问、所有选项及解释、用户选择和修改目的。
- 配套 `<同编号>-tasks.md` 记录执行计划、实时完成状态及验证结果；不得把未执行的验证标为通过。

## 注意事项
  - 当前处于项目开发的早期阶段，不考虑修改数据库表结构造成的兼容性问题。我会在测试前清空所有历史应用数据。
