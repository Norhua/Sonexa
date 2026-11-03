# portamento：真实 MB 返回结果被本地旁证检查过滤

## 来源

2026-11-03 通过两次公开 MusicBrainz recording 搜索获取；请求相隔超过一秒，User-Agent 为 `Sonexa/0.1.0 ( https://github.com/norhua/sonexa )`。两个 JSON 文件保存原始响应字节，未删改、补造字段或重排结果。MB 核心元数据采用 CC0：https://musicbrainz.org/doc/About/Data_License 。

共用请求参数：

```text
GET https://musicbrainz.org/ws/2/recording
fmt=json
limit=25
inc=artist-credits+releases+isrcs
```

- `search_portamento_anchored.json`：`query=recording:"portamento" AND (release:"TVアニメ「灼眼のシャナ」ベストアルバム灼眼のシャナ\-BEST\-" OR artist:"川田麻美")`
  - count=0，recordings 为空。
  - SHA-256：`a5b7ece3f48cee4a0dde36106da58fb30c8c3fdcb88a0df26454af85a7978d81`
- `search_portamento.json`：`query=recording:"portamento"`
  - count=76，第一页返回 25 个 recording，展开为 36 个 track。
  - SHA-256：`de0b5e4ce0ddb8acdb1340f2cbe464cafc0f5bfca304f3184bda21f3e3bad33e`

## 目标与根因

输入：`portamento / 川田麻美 / TVアニメ「灼眼のシャナ」ベストアルバム灼眼のシャナ-BEST-`。

第一页第 7 个 recording 已是目标 `38a5891c-2aab-4bdc-b3b9-fa8879346b91`，署名 `川田まみ`，含 8 个发行位置；包括截图两个目标：

| release | track | 专辑 | 位置 |
| --- | --- | --- | --- |
| `fa61ac5d-447b-4698-817e-afd7faba11b2` | `a2079f0f-906c-4f64-b03c-9bdd36d5ebdb` | 灼眼のシャナ -BEST- | disc 2 / track 4 |
| `eb7cc685-a384-4eaa-96b6-1b4225c35bf5` | `72d6d601-73d7-4b5e-afd1-5d0c7655f2d2` | 灼眼のシャナ-BEST- | disc 2 / track 4 |

两条都为 CD、362000ms。其他 6 个位置包括 SAVIA、MAMI KAWADA BEST “F” 和罗马字专辑名；本轮不凭相同 recording 把它们一并放行。

失败不是分页、网络错误、track 解析缺失或 MB 没数据。锚定条件未命中后，现有仅曲名兜底已经返回目标；原始输入的两个辅助字段却都被本地判定为 MatchNone：艺术家存在汉字/假名异写，专辑完整名称位于不带空格的长宣传描述内。旧专辑比较只覆盖空格词元包含、前缀和高相似度，因此 Engine 清空候选池。

没有从读音猜出 `麻美=まみ`，MB 返回的 artist aliases 也没有 `川田麻美`，不构造该艺术家身份等价。新规则仅使用完整专辑文本的有界包含作为人工召回旁证。

## 回归

`internal/app/album_recall_test.go` 使用实际 provider/Engine/应用绑定重放上述原始请求和响应；HTTP handler 精确校验 query 和 limit，不向公网请求。

```sh
go test ./internal/app -run '^TestMusicBrainzAlbumRecallAcrossEntrypoints$' -count=1 -v
```

- 修改前：真实 provider 解析 36 tracks，目标曲名精确、艺术家/专辑都不匹配；Engine 执行两级后 `Scored:[]`，测试失败。
- 修改后：只留下表中两条人工候选、自动集合为空；其他专辑位置和无关同名歌曲仍被过滤。
- 导入、复核改搜、上游搜索、编辑重新匹配一致；原始输入、候选署名/标题/专辑与 MB track/release/位置保真，快照重读和本地刷新保持 ReviewOnly。
- 人工模拟旧的空候选持久化快照后，队列读取仍为空且不触发网络；点击重新搜索后用缓存的原始 HTTP 数据重新评分，恢复两条候选。人工接受仍通过既有准入事务绑定真实目标 track。
- 测试中两次本地 HTTP 重放跨所有入口缓存复用，不额外请求详情或艺术家 API。

公网仅取证两次；其余验证均离线。没有读取/修改用户应用数据，也没有原生窗口人工测试。
