# 实施检查点（未完成）

## 状态

- Change：`revise-album-cover-workflow`
- 本轮完成：9/43（1.1、1.2、2.1–2.4、7.1–7.3）。具体行为及验证条件以 tasks.md 为准。
- 本轮其他源码为进行中的实现，不能按文件存在或构建通过认定任务完成。
- 只使用隔离临时数据库测试，未启动应用迁移用户日常数据库，未执行主规格同步或归档。

## 已验证

- `~/.go/bin/wails generate module`：成功。`wails` 不在当前 PATH，机器已有该可执行文件；未安装替代工具、未手改生成绑定。
- `go test -count=1 -race ./internal/...`：通过；新增封面 sources 包尚无测试，不能据此认定四来源实现完成。
- `go vet ./...`：通过。
- `cd frontend && npm run build`：通过，有现存大 chunk 警告（主 JS 约 921 kB）。
- `cd frontend && npm test`：5 文件、34 测试通过。
- `git diff --check`：通过。
- 新增存储/封面测试覆盖：迁移回滚、旧引用保留、目录记忆、图片身份、采用 revision、失败保留旧图、JPEG/PNG/WebP 解码、字节/像素边界、符号链接越界、命名碰撞、候选预览隔离、旧图迁移重试、用途分级、目录合并及取消。

## 暂停原因：Chromium 回归失败

命令：

```sh
cd frontend
SONEXA_E2E_PREVIEW=1 npm run test:e2e -- --project=chromium tests/e2e/image11-refinements.spec.ts tests/e2e/table-refinements.spec.ts
```

结果：8 通过，3 失败。

1. `image11-refinements.spec.ts:56`：旧“larger drawer cover and inline icon”测试，封面到标题间距实际 48px，期望 <=20px；还含旧图标断言。需区分新无歌集说明带来的布局影响与旧测试上下文，不能直接放宽断言。
2. `image11-refinements.spec.ts:133`：封面失败状态没有显示预期 nil；测试替身仍只提供旧 CoverOutcomes，而新组件调用 CoverTargetOutcomes。需更新替身并验证真实空/错误分类，不能仅改预期。
3. `table-refinements.spec.ts:155`：等待已删除的“重新获取封面”按钮超时。需重写为右键→独立搜索页→返回/切换后的迟到结果保护测试。

本机未提交的 Playwright 证据目录：
- `frontend/test-results/image11-refinements-larger-80f70--search-errors-and-adoption-chromium/`
- `frontend/test-results/image11-refinements-cover--18591-ilures-keep-distinct-states-chromium/`
- `frontend/test-results/table-refinements-switchin-e0fc7-ores-its-late-search-result-chromium/`

每个目录包含 error-context.md 和 trace.zip。生成报告不提交。

## 已观察到、仍须完成的接线风险

- 新 CAA resolver、工作池和页面已有实现与部分测试，但没有完整覆盖全部 spec 场景；任务 3.x、4.x、6.x 未勾选。
- 单来源重试目前取消整个搜索会话，会使保留的其他来源卡片失去采用授权；应实现单来源独立重试会话/授权，而非丢掉其他来源结果。
- `LibraryImages.ReuseReleasePreviews` 的预览复用尚需区分“候选自动缩略图”与“手动搜索未采用预览”，避免手动浏览间接改变歌集自动图；1.3 未完成。
- 缓存容量/TTL、清缓存、新旧目录安全切换尚未接入新的 cover_file 全路径（2.5 未完成），不能声称改目录不会失联。
- 全部下载路径的出网 gate、来源域名/重定向、同图退档以及用户已采用优先还需实际绑定/并发回归；旧 SearchCovers/AdoptCover 尚未清理。
- 来源适配器需要错误/限流/Retry-After fixture、真实图片验证；搜索会话关闭与应用关闭的 goroutine 排空还需统一测试。
- 本地系统文件选择器需要可注入测试接缝及原生人工验收（6.1、8.4 未完成）。
- iTunes 的关于页旧说明与现行文档尚未清理（7.5、9.2 未完成）；7.4 只接通底层 SkipCache，仍缺完整绑定验收。
- 新事件模型通过 CoverSearchResults 快照绑定进入 Wails 生成范围；未使用手写生成文件。

## QQ / 163 只读公开链路预检

未使用账号、Cookie、代理服务或访问限制绕过。

- QQ `POST https://u.y.qq.com/cgi-bin/musicu.fcg`，`DoSearchForQQMusicDesktop`：两种公开参数形状对已知关键词返回 HTTP 200、空列表，不能视作可靠的无结果判断。
- QQ `GET https://c.y.qq.com/soso/fcgi-bin/client_search_cp?w=周杰伦&format=json&p=1&n=3&t=0`：HTTP 200，返回歌曲及 album MID，例如 `000MkMni19ClKG`（叶惠美）。
- QQ `https://y.qq.com/music/photo_new/T002R800x800M000000MkMni19ClKG.jpg`：HTTP 200，183730 字节，JPEG 魔数；尚未以正式适配器核验真实尺寸。
- 163 `POST https://music.163.com/api/search/get/web`，`s=周杰伦&type=10&limit=3&offset=0&total=true`：HTTP 200，返回 album ID、名称与 picUrl。
- 163 返回的 picUrl 加 `param=250y250`：HTTP 200，135876 字节，响应 Content-Type=image/jpg，但字节为 PNG；验证了不能信任 URL/响应头推断编码或尺寸，正式流程必须完整解码。

这些预检不等于任务 5.2/5.3 已完成，也不证明所有环境可用。
