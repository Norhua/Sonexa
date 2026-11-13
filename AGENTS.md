# AGENTS.md

## 技术栈
当前项目使用的技术栈为：Wails v2 + Go；Vue 3 + TypeScript + Tailwind CSS v4 + Volt + Lucide Icons + Pinia

## Git
对于 git 使用你要注意：
  - 在合适的阶段 git commit
  - commit 使用中文
  - commit 包含一句话简介和详细信息，且简介要遵守"约定式提交规范"
  - 约定式提交规范为"<type>[optional scope]: <描述>", <type>[optional scope]" 部分应当使用英文

## 架构约束
业务代码与上游差异分离：
1. 业务包（如 lyrics、covers）自行声明所需的接口，通过依赖注入使用，不在业务代码中处理不同上游造成的差异（禁止 switch source 式分发）
2. 上游差异的处理放在各上游包（internal/provider/xxx）中，每个上游按自身能力实现对应的业务接口，返回约定的数据或提供约定的功能

## 关于 change_record 文件夹
由于 openspec 自身或者其他原因，开发者有时不希望直接调用 openspec skills，但是在修改后又需要保持实现和 spec 文档一致。所以有时会在项目根目录，或者当前 change 目录下创建 change_record 来记录修改，并在合适的时机将记录合并回主 spec 文档，在 change_record 中至少有三类 md 文档：
1. spec-diff.md 只有一份：修改阶段可能通过多次 ai 会话完成，他们共同维护 spec-diff.md，spec-diff.md 记录代码实现与 spec 文档的差异。这个文档用于后续 archive change 时同步到主 spec 文档保持文档和代码实现的一致性
2. <一个从 1 开始递增的编号>-why.md: 每次修改前创建，记录：原始提示词，你的追问和提供的选项 (包括选项的解释) 和我的选择，修改目的
3. <一个从 1 开始递增的编号>-tasks.md: 和 openspec 的 tasks.md 作用相同，用来记录并同步执行计划
4. 以上是必要文档，当 AI 助手认为这些文档无法记录全部有效信息是，可以创建其他命名规则如"<一个从 1 开始递增的编号>-xxxx.md"的文档

## 注意事项
  - 当前处于项目开发的早期阶段，不考虑修改数据库表结构造成的兼容性问题。我会在测试前清空所有历史应用数据。
