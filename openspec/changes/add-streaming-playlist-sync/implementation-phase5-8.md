# 本次实施记录：任务 5–8

## 范围

按用户要求采用批量实现、分组测试。本次完成 5.1–5.5、6.1–6.5、7.1–7.6、8.1–8.8；
用户要求保留的 4.2–4.4 始终未勾选。第 8 组完成后按用户要求停止，不进入第 9 组。

- 数据模型：`streaming-playlists-v1`，明确入库归属与首次时间，同步出现项、导入目标、批次摘要与不可变输入。
- 同步：完整快照差异、有界服务端计划、原子提交、忽略恢复、上游缺席保留、取消及旧修订保护。
- 投递：准入计划封存目标，接受/无需匹配/自动或人工跳过/离线整理共用事务投递，取消共享任务保留有效需求。
- 音乐库：列表、FTS、艺术家/专辑、导出按入库范围；专用同步列表、显式入库、普通集合校验、引用感知移出与绑定删除。
- 文档：根 README、歌单模块 README、导入模块 README。

## 实际验证

以下分组均包含包内全部测试，而不是仅定向运行新增测试：

```sh
go test -count=1 ./internal/store ./internal/importer ./internal/library ./internal/app ./internal/acceptance ./internal/playlists
go test -count=1 -race ./internal/store ./internal/library ./internal/importer ./internal/app ./internal/playlists ./internal/acceptance ./internal/matching
go test -count=1 -race ./internal/store ./internal/library ./internal/importer ./internal/app ./internal/playlists ./internal/acceptance ./internal/export
go vet ./...
openspec validate add-streaming-playlist-sync --strict
git diff --check
```

上述命令最终全部通过。阶段测试曾发现并修复：

- 不同创建时间被元数据等价测试一起比较，改为独立验证时间格式。
- 架构检查误把计划 JSON 序列化视为上游解码；业务改用类型化深拷贝，检查只允许计划 Marshal，继续禁止解码/平台协议。
- 新结果含切片不能用 Go 结构体直接比较；历史确认夹具改用新的真实接受建立目标基线。
- 目标统计的 SQL 别名与 state 列重名造成错误分组，改为独立 effective_status。
- 原测试要求未收藏发行作为音乐库展示代表，按新规格更新，并增加隐藏结构不丢失断言。
- 歌词文件按曲目保存路径，文件保护夹具改为明确共享同一文件引用后验证。

## 未完成边界与续接点

- 未运行 `go test -count=1 -race ./internal/...` 全部内部包；本记录不能代替任务 11.3 的全量验收。
- 未生成本轮 Wails 绑定，未运行前端类型构建、Vitest、Chromium/WebKit 或原生窗口测试。
- 未做 QQ/Spotify live 抓取、Windows 实际进程回收、多平台安装/签名验收。
- 同步服务、批量入库和绑定删除具备后端实现，但完整 App 生命周期/绑定/事件、候选 DTO 与 UI 仍待第 9–10 组。
- 下一项从 9.1 开始；4.2–4.4、11.4–11.5 的平台/授权条件仍须独立取得证据，不能以本机离线通过勾选。
- 模型修改仅用于清空历史数据后的新库；不执行迁移或自动删库。
