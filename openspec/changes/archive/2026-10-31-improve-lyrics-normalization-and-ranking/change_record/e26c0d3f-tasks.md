# QQ 音乐「夕立」歌词获取失败：执行任务

起始提交：`e26c0d3fead99c8d20ac217ccfa7e99f80216000`。缘由及原始提示见 [e26c0d3f-why.md](e26c0d3f-why.md)。

## 1. 调查

- [x] 1.1 核对 QQ 搜索→获取→错误分类链路，使用有界真实响应定位截图失败原因。
- [x] 1.2 确定最小修复方案，记录证据；如需改变既有协议边界，先确认方案。

## 2. 实现与回归

- [x] 2.1 修复 QQ 上游适配，不在业务/前端新增来源差异处理。
- [x] 2.2 补充请求契约、三通道交付及错误/预算/取消回归测试。
- [x] 2.3 验证「夕立」真实响应可完整构建并回读 XLRC，记录实际结果与未覆盖范围。

## 3. 收尾

- [x] 3.1 运行相关测试及必要全量检查。
- [x] 3.2 更新共享 `spec-diff.md` 与本轮缘由、验证记录，不修改原规划或主 spec。
- [x] 3.3 检查 diff，按中文约定式提交规范提交本轮修改。

## 调查与验证记录

### 调查结论（当时尚未修改业务代码，随后已获得方案确认）

- 原查询 `夕立 CROW'SCLAW` 返回 20 条原生结果，只有同名 `003iQsFU4IGFlR` 符合目标（ID `567406875`、287 秒、CROW'SCLAW、专辑为空），其余为其他曲名；业务准入后只剩截图中的 1 条。
- 该 MID 的原请求两块均 `24001`，`songID=0`。添加数字 ID 或仅用数字 ID 仍为相同结果。补充候选曲名/艺术家/时长后 `code=0`，但原文/翻译/罗马音全部为空；补专辑及同端点参数对照未取得歌词。没有将这些探测加入生产重试/回退。
- QQ 将有歌词的同曲记录署名为 3L：`001Buqii4Rxrm5` / ID `108174042` / 287 秒 / `アコースティック・エピソード`。仅搜索曲名 `夕立`，它位于原生结果第 2 条；检索 `夕立 3L` 也可找到。
- 直接使用现有 MID 批量歌词参数成功取回两块（均 `code=0`）；`TestLocalQQProbe` 经现有解析器得到原文、翻译、罗马音均 available、逐字 available、XLRC 12888 字节、warnings=0。因此已否定「缺少数字 ID」及「解码失败」作为本次主因。
- 本次已发送 9 次 QQ `musicu.fcg` 诊断请求（3 次搜索、6 次歌词/参数对照），以及 1 次歌曲网页查询（仅返回 4 字节，未取得有效页面）；串行、无登录、无自动重试、未更换端点主机规避限制。实际正文位于忽略目录 `tmp/qq-yuudachi-e26c0d3f/`，不提交。
- `SONEXA_QQ_PROBE=.../songid-play.json go test ./internal/provider/qqmusic -run '^TestLocalQQProbe$' -v -count=1`：FAIL，重放上述业务错误，非实现测试退化。
- `SONEXA_QQ_PROBE=.../vocalist-play.json go test ./internal/provider/qqmusic -run '^TestLocalQQProbe$' -v -count=1`：PASS，三通道和逐字均成功。未验证原生窗口和采用回读，不能据此勾选实现完成。
- 用户已选择「双查询补充（推荐）」：同请求两组搜索，各最多 20 条，合并最多 40，业务评分后最多展示 20，预取 Top 3 不变。歌词请求补候选元信息，不新增数字 ID 查询，不改变 `24001` 及未知业务错误停止行为。
- 确认后检查已有 legacy 回退：上述无词 MID 返回 HTTP 200、`code/retcode/subcode=-1901` 且没有歌词字段；现有 generic error 会错误地停整源。QQ 已有参考协议将 `-1901` 作为无歌词，需要在 QQ 适配器内严格识别该一致空响应为 `ErrNoLyrics`，其他业务错误仍保留失败。新增 1 次旧接口诊断，当前合计 QQ API 10 次、网页 1 次。

### 修复后的真实生产能力验证

- 本地隔离程序 `tmp/_scratch/qq-yuudachi-e26c0d3f/main.go` 使用实际 `qqmusic.Provider` 和 `lyrics.Service`：共享 QQ 配额 30 次/分钟、单次验证 6 次发送上限、60 秒总时限、不重试、不写用户数据库；完整日志 `tmp/qq-yuudachi-e26c0d3f/fixed-live.log`。
- 保持用户原查询 `夕立 / CROW'SCLAW / アコースティック・エピソード / 287000 ms`：同一次双块搜索经原评分准入得到 3 条候选，仍仅取 Top 3。
- 第 1 条 `003iQsFU4IGFlR` / CROW'SCLAW：新接口成功但空、legacy `-1901`，最终 `kind=no_lyrics, StopSource=false`，共 2 次歌词请求，不阻断后续。
- 第 2 条 `001Buqii4Rxrm5` / 3L：原文、翻译、罗马音和逐字均 available；XLRC 12888 字节、warnings=0，序列化→解析→序列化与阅读行完全一致。匹配分 80、HighConfidence=false，署名差异仍需人工确认，不变成别名/身份等价。
- 第 3 条其他歌手同名记录：原文/翻译可用、罗马音解析失败，仍保持可选通道错误与警告；这不是本次目标，也没有隐藏此错误或继续扩大探测。
- 此轮共 **5/6 次实际发送**（1 搜索＋2 无词候选＋1 目标候选＋1 其他 Top 3），不追加第四条、不更换查询、不刷新重复验证。连同调查共 15 次 QQ API、1 次无效网页请求，分阶段串行低频执行。
- 本次未启动 Wails 原生窗口、未写入用户数据。真实歌词只保留于忽略目录；采用落库由合成 App 集成回归验证，不将离线测试声称为原生界面验收。

### 离线回归

- `go test ./...`：PASS。
- `go test -race ./internal/provider/qqmusic ./internal/lyrics ./internal/app`：PASS。
- `go vet ./...`：PASS。
- `openspec validate improve-lyrics-normalization-and-ranking --strict`：PASS（只做校验，不更新/归档原规划）。
- `git diff --check`：PASS；最终补充 `24001` 停止/空候选状态断言后的定向 race 回归同样 PASS。
- QQ 请求测试覆盖同请求双关键词、曲名去重/无曲名、每组最多 20/合并最多 40、业务最终 20/Top 3、MID 去重和原始署名、逐块失败、取消；元数据/封面仍用原单查询契约。
- 歌词请求测试验证候选自身 Base64 元信息及整数秒时长、不额外查询数字 ID；`24001` 和其他未知业务错误、401/403/429/TLS/超限/取消仍禁止后续回退。旧接口只有三状态均为 `-1901` 且两通道为空才为暂无歌词，缺码/矛盾码/附带正文不降级。
- `TestQQDifferentCreditRecallPrefetchAndAdoption`：复现无词同名先取、3L 后取；两候选正常完成、三通道/词时间可预览、未经批准不可采用、批准后保存同版本完整 XLRC 并无损回读。现有 App 三源管线 mock 更新为真实双块响应，没有放宽解析器接受缺块。

