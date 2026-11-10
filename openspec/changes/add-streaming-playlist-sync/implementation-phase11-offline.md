# 第 11 组离线验收与交付核对

> 后续已完成 4.4 的干净检出构建／自检，累计 55/59；当前构建与平台边界见[第 4 组记录](implementation-phase4-packaging.md)。以下保留本阶段实际结果。

## 范围与结论

本轮依用户授权继续排查上一轮 3 个浏览器失败，采用批量修改、分组测试，再做全量回归。
完成 11.2、11.3、11.6，累计 **54/59**；文档与规格核对见下表。没有请求真实 QQ/Spotify 歌单、安装完整应用或启动原生窗口。
4.2–4.4、11.4、11.5 仍保留未完成，不归档、不将 delta 同步到主规格。

环境：macOS 27.2 / arm64，Go 1.26.10，Node 24.21.0。本机进程证据只适用于该环境。

## 浏览器失败原因与修正

### 封面 URL 断言

`cover-grid.spec.ts` 原先要求 `/variantId=32$/`，但 `CoverPreview.vue` 的 URL 还携带当前来源搜索会话 `searchId`。
上一轮已在 UI 变更前的基线复现。本轮没有删除生产会话参数、改封面选择或宽泛放行 URL，
而是记录各来源的 `StartCoverSearch` 请求 ID，解析并严格核对 `/cover`、variantId 和相应 searchId。
最小规格、切换规格、大图、实际请求集合和显式采用断言均保留。

### 编辑器“用户”标签与加载时序

`wails-stub.js` 的 `editorFields()` 把 11 个字段标成 `mode=user`，与有效上游值不同时，
`EditCompareField.vue` 本来就显示“用户”。旧断言在懒加载编辑器尚无字段时可提前得到 0，
偶尔字段先到便失败，因此不是 WebKit 专属。
本轮先加字段就绪断言后运行两个文件：14 项中 10 通过、4 失败，Chromium/WebKit 都稳定复现
上述两种问题。该诊断不是最终结果。

修正后按真实契约验证：先等曲名加载，用户曲名显示标签；逐字段应用上游值后标签消失；
撤销后恢复原值与用户标签，保存恢复禁用。详情抽屉不显示字段来源的断言保持不变。
首次分组中新增 `filter(has:)` 错把编辑器祖先带入相对查找，导致 50/52；修正为相对字段定位后
三轮分组及全量全部通过。没有修改生产编辑器或将 11 个标签全部隐藏来迎合旧断言。

## 11.2 并发／重启证据

| 风险 | 实际入口与断言 |
| --- | --- |
| 同步预览期间导入完成 | `app/playlists_e2e_test.go` 经真实 App 接受／无需匹配／跳过，旧 pending 预览必须 stale；`store/import_delivery_test.go` 覆盖接受先提交与同步先提交两种顺序。 |
| 删除主输入所属歌单 | 新增 `store/playlist_restart_test.go` 通过真实 Service 删除绑定，保留另外两个歌单的有效目标、主调度壳、有效代表输入与已保存手搜草稿；真实 SQLite 关闭／重开后继续核对。 |
| 旧搜索迟到 | channel 控制旧结果在删除后才写回，必须返回 `ErrItemNotActive`；忽略与重启后再次提交旧 revision 同样拒绝。 |
| 忽略后选择恢复 | 三歌单共享任务移出后只恢复剩余歌单之一，新尝试 ID 不等于旧主条目，另一歌单仍 ignored；旧 discarded/merged 及批次分母不变。 |
| 提交回滚与重复确认 | `beforeCommit` 故障让恢复事务整体回滚且不保存 operation_result；两 goroutine 同时确认只创建同一批次，重启后重放得到原结果。 |
| 未确认／过期计划 | 重启丢弃未确认计划；可控时钟推进完整 TTL，删除预览过期不能改变原快照；成功计划则可在重启后幂等回读。 |
| 终态、孤立实体与 FK | 恢复后实际准入投递，重启不重排终态；未入库 track 必有同步引用，`PRAGMA foreign_key_check` 无异常。 |
| 在途 Runner 与退出 | `importer/streaming_cancel_test.go` 覆盖撤销主需求后取消旧请求、重排剩余需求及全撤销不留 track；App 生命周期测试覆盖禁网、删除、设置发布及先等待再关闭 Store。 |
| helper 中断与重新启动 | 扩展 `provider/spotify/process_unix_test.go`：主动取消／总期限超时均回收真实替身后代 PID，返回有界取消错误，随后同 Runner 启动新 helper 成功；既有错误帧、错误 ID、超量、崩溃及退出阻塞组同跑。Windows Job Object 仍未运行验收。 |

## 本轮实际命令与结果

```sh
# 批量修改后的后端组
go test -count=1 -race ./internal/store ./internal/playlists ./internal/importer ./internal/app ./internal/provider/spotify
# 全量 Go，不使用测试结果缓存，live 环境变量未启用
go test -count=1 -race ./internal/...
go vet ./...

cd frontend
npm test
npm run build
SONEXA_E2E_PREVIEW=1 npm run test:e2e -- cover-grid.spec.ts screenshot-review.spec.ts editor.spec.ts --repeat-each=3
SONEXA_E2E_PREVIEW=1 npm run test:e2e
cd ..

# 使用已有锁定环境和已有冻结产物，不下载、不重新冻结
V="$PWD/tmp/_scratch/spotify-audit/venv-a/bin/python"
"$V" -I -c 'import sys,unittest; sys.path.insert(0,"internal/provider/spotify/sidecar"); r=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.discover("internal/provider/spotify/sidecar/tests",top_level_dir="internal/provider/spotify/sidecar")); sys.exit(not r.wasSuccessful())'
"$V" internal/provider/spotify/sidecar/verify_frozen.py build/spotify-helper/dist/spotify-helper
SONEXA_TEST_FROZEN_HELPER=1 go test -count=1 -race ./internal/provider/spotify -run '^TestFrozenHelperOfflineIntegration$' -v
```

- 后端组全部通过；全量 25 个包中 24 个有测试并通过，`qqmusic/qrcdes` 无独立测试文件；vet 无输出、退出码 0。
- Vitest 21 文件、**111/111**；类型检查／Vite 构建通过，保留既有 >500 kB chunk 警告。
- 封面／编辑／截图三轮：**156/156**，Chromium 78、WebKit 78。
- 全量浏览器：**304/304**，Chromium 152、WebKit 152。包括同步、导入、复核、库浏览、编辑、导出及既有其他文件，不将定向通过写成全量通过。
- Python：**14/14**；真实冻结 helper：明确空歌单 2 次替身 HTTP、205 项分页及补取 5 次替身 HTTP；均无实际 Spotify 发送。
- 冻结自检：205 个原始位置，禁网、禁外部 Python、无 PATH 工具、中文／空格路径、资源未变；主 helper 与 libpython ad-hoc 校验通过。**不是发行签名、公证或完整应用验收**。
- 本机原始日志位于 Git 忽略的 `tmp/_scratch/playlist-offline-acceptance/`；本文件保留可提交的命令与结果摘要。

## 11.6：18 份 delta 与交付证据核对

逐份读取 proposal、design、tasks 和全部 delta，对照现有实现、测试及 README。
本轮仅更新过时的实现状态和证据说明，没有删减 delta 行为，也未声称以下未完成平台／在线任务已满足。

| Delta | 实现与回归证据 |
| --- | --- |
| `import/table-import` | 文件／同步来源及目标分别保存；`importer/streaming_test.go`、`app/import_destinations_test.go`、浏览器复核／同步组；根 README 的导入／入库流程。 |
| `library/admission` | 目标封存及写事务重验；`store/import_delivery_test.go`、`library` 准入组、App E2E 与新增重启投递。 |
| `library/browse-views` | 音乐库名单、计数、分区与可操作 ID 限定归属；`store/library_scope_test.go`、全量 album/list/view-options 浏览器回归。 |
| `library/collections` | 普通集合只接纳已入库曲目；`store/library_scope_test.go`、App 双歌单端到端拒绝／成功组织。 |
| `library/export` | 后端 selection/filtered/all 归属过滤；`store/library_scope_test.go`、App 导出组与浏览器表格回归。 |
| `library/release-catalog` | owned 位置与展示代表限定入库，隐藏发行仍保留；store/library 和 App 清单回归。 |
| `library/search` | FTS 在限量前应用作用域，当前同步可搜 absent 项；scope 大量外域命中测试与前端同步／搜索测试。 |
| `library/synced-playlists` | Service 差异／计划、store 事务／状态、App 绑定和前端 syncedPlaylists；真实 App E2E、新增重启／并发及全量浏览器。 |
| `library/track-library` | 默认不入库、显式归属、首次时间与引用感知移除；store model/scope、App E2E、added-time 和同步表格回归。 |
| `matching/pipeline` | 统一多目标投递、撤销／共享调度、历史分母；store delivery/identity、importer streaming/cancel 与新增重启验收。 |
| `matching/provenance` | 全 track 严格证据与候选 localTrackId/inLibrary 分离；owned_import/identity、App 候选和前端搜索／复核回归。 |
| `platform/app-shell` | 独立侧栏／导航、同步与 ghost 分离、列偏好及事件；unit 列／请求组、Chromium/WebKit 全量。 |
| `platform/data-store` | 模型、FTS、FK、不可变输入、时间及原子事务；store 全量 race 与重启，更新已知问题中的模型名／FTS 旧说明。 |
| `platform/settings` | QQ 共享配额与 Spotify 来源说明；Go 配额／设置／装配和前端 provider-capabilities/visual-controls；许可锁与清单版本一致。 |
| `platform/sidecar-runtime` | runner 帧／清单／门禁、Unix 替身及 macOS arm64 冻结自检；Windows 实际回收、各目标发行装配／安装仍缺证据，保持 4.2–4.4、11.5 未完成。 |
| `review/review-queue` | 全部有效目标、接受／跳过投递与忽略恢复；App destinations/E2E、store delivery/restart、review-actions/synced 浏览器组。 |
| `upstream/playlist-read` | QQ 完整分页 fixture、Spotify 锁定库／Go bridge 与真实 helper 离线往返；真实分页、限制响应及观察增删仍待授权 11.4。 |
| `upstream/provider-registry` | 单能力装配、QQ 实例复用、Spotify 不进元数据及旧桶复用；acceptance 边界、app assembly/lifecycle、provider 配额组。 |

构建／来源核对：SpotifyScraper 3.9.2、CPython 3.12.13（PBS 20260807）、PyInstaller 6.22.3 与锁、About 文案及 LICENSES.md 一致。
完整许可文本和 native 依赖已在现有 helper 清单中并经摘要自检；不以许可清单代替平台发布许可呈现。
`wails.json` 尚无 helper 装配钩子，`wails build` 不能独自证明完整 Spotify 发行包。
开发／build README 明示资源目录、现有 helper 准备入口及未完成边界；本轮未从干净检出重新构建，因此不勾 4.4。

最终文档检查命令：

```sh
openspec validate add-streaming-playlist-sync --strict
git diff --check
git ls-files frontend/wailsjs frontend/dist frontend/node_modules frontend/test-results build/bin build/spotify-helper
```

严格校验与 diff 检查通过；最后一条无输出，生成绑定、依赖、测试报告和冻结产物均未跟踪。
采用中文约定式分阶段提交，正文分别记录回归范围、实际结果和剩余风险。

## 剩余任务与续接条件

- **4.2**：Windows 进程树／句柄真实运行验收未完成；本机 Unix 测试不能代替。
- **4.3**：按目标冻结、Wails bundle／安装器装配及发行签名矩阵未完成；当前只有 macOS arm64 helper 证据。
- **4.4**：已有准备说明及本轮边界纠正，不等于按完整发行流程从干净检出构建／自检已完成。
- **11.4**：没有获准公开歌单、live 入口实施／执行与已观察增删证据，不发送线上请求。
- **11.5**：没有实际发布目标完整安装、原生窗口及升级／签名验收。

这些条件仍需用户另行确认，不自动安装工具、获取凭据、扩大在线探测或缩小任务标准。
