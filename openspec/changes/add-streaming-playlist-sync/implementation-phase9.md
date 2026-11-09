# 第 9 组实施记录

完成 9.1–9.4，批量实现后分组测试。4.2–4.4 继续保留未完成。

- App 新增同步来源/绑定/预览/确认/取消/删除、列表与批量入库绑定；重复绑定返回既有 ID，前端不能回传快照或改目标。
- App 保留同步 service/fetcher，设置仅更新注册表；当前门禁、删除与退出取消接线。退出等待完整绑定调用和所有导入 Runner 代际，避免 Store 先关闭。
- 成功提交发布本地实体、同步列表和队列失效事件；离线复核整理仅在确有成功投递时通知，失败不伪报。
- 候选 localTrackId 与 inLibrary 分离，搜索/复核按不同语义映射动作；API/事件说明在 `frontend/src/api/README.md`。

实际通过：

```sh
go test -count=1 -race ./internal/app ./internal/playlists ./internal/store ./internal/importer ./internal/library ./internal/matching ./internal/acceptance
/Users/amadeus/.go/bin/wails generate module
cd frontend && npm run build
go vet ./...
git diff --check
```

Wails 不在默认 PATH，使用本机既有 `/Users/amadeus/.go/bin/wails`，没有安装新 CLI。
前端构建有既有大 chunk 警告，构建成功。生成绑定、dist 与依赖目录未跟踪。
本组没有运行 Chromium/WebKit、原生窗口或 live 来源验收；第 10 组界面和第 11 组综合验收仍待完成。
