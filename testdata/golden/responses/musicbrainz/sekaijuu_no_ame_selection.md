# 「世界中の雨」11 发行选版回归

## 来源

以下为 2026-11-02 11:33–11:35 UTC 期间取得的真实 MusicBrainz 响应，原始 JSON 字节未改写：

1. `search_sekaijuu_no_ame.json`
   - `GET https://musicbrainz.org/ws/2/recording`
   - 参数：`fmt=json`、`limit=25`、`inc=artist-credits+releases+isrcs`
   - 查询：`recording:"世界中の雨" AND (release:"心の羽根" OR artist:"チームドラゴン from AKB48")`
   - 2 个 recording（原版/伴奏），各自有 11 个 release track，共 22 个候选。
   - SHA-256：`17f83e3f9191ca5d94f8094b64bfb29ae286ff22837ec7db453853f9dac0665b`
2. `release_kokoro_no_hane_07088ba6.json`
   - `GET https://musicbrainz.org/ws/2/release/07088ba6-2c40-356a-8d0c-ef72d8a21d93`
   - 参数：`fmt=json`、`inc=artist-credits+labels+recordings+release-groups+media`
   - 独立核验最终发行的完整 4 首清单、第 2 轨身份、日期与目录号。
   - SHA-256：`9af75842b33c99418fb1e48f1a6f0312276429c1e041671c2be64ffeb6604289`

本轮仅 **2 次公网请求**，串行且间隔大于 1 秒，禁用重试及重定向；未请求 11 次详情、未读取用户数据库或应用配置。User-Agent：`Sonexa/0.1 (https://github.com/norhua/sonexa; release-selection-regression)`。

MusicBrainz 核心元数据许可为 CC0：https://musicbrainz.org/doc/About/Data_License 。

## 真实候选事实

原版 recording：`fea2d6f4-0957-4090-baba-b013ae3dfbcd`。
同一 release group：`9b793f9f-12fd-45f6-9e2e-50ad4ac7389d`。

下表按 release MBID 升序展示。**所有行都是整发行 4 首、CD、JP，目标曲序为 2**；4 来自响应中的 release 级 `track-count`，不是从唯一命中的 track 数推算。

| Release MBID | 目标 track MBID |
| --- | --- |
| `07088ba6-2c40-356a-8d0c-ef72d8a21d93` | `5e5f141f-cdaa-3346-9f9a-43b572006e9b` |
| `09b43924-f822-375f-acd2-1871e69cd6a7` | `355e93ba-2c26-3642-8f76-82d8cf417495` |
| `0d03b09c-65d7-3423-8cb7-ae27499bd47b` | `a98c6566-cbdc-36f2-84f1-0a50b140844e` |
| `1728a503-dc08-3088-9f3b-9471c2fdc686` | `6e862e7e-d4a2-3c3e-bdc1-95fae9568f41` |
| `354c2d7f-875c-44a2-8ab2-847424ffd4de` | `5d444cdf-25d7-3177-81aa-07f39d46af6f` |
| `56dde897-7856-33e0-a96b-64cb21951c5f` | `50924f6f-6795-3932-8558-ed6c600d37b7` |
| `70e8ff92-7de2-3997-bebc-67db25b3e4bf` | `146fd64b-3fa5-338e-9853-51b7a1939466` |
| `8d29cdeb-a245-3fc4-990e-379ca69bb1c4` | `cd25916d-28be-3e75-b8fa-6ca6fd5d6dd7` |
| `bbd91f0b-bca2-3a11-a8d6-4c9e65643e89` | `332fbd25-caaa-3a88-aa93-4750e253eaf3` |
| `d6b47bf8-93e8-3995-9978-bbf3f038ffb7` | `fbcc8d05-0237-33c0-a8ff-136c7accde11` |
| `e8ab0e85-213e-3a70-81b5-d6244a852945` | `7b6ec98d-1ebf-377e-ab95-1a2a7e0cf9b1` |

## 执行与结论

```sh
go test ./internal/app -run '^TestSekaijuuNoAme' -v -count=1
```

默认完全离线，用本地 HTTP 服务器提供上述搜索响应，经过真实 MusicBrainz provider、Engine、library 选版，再通过真实 SubmitImport/Runner 和 SQLite 准入事务核对结果。三种输入（干净、译名/版本附注、旧截断）都保留 11 个自动候选；原版无备注输入均 100 分，后两者均 92 分。其余 11 个伴奏候选仅供人工确认。

空库中没有已有发行偏好可用，4 首总数与 JP 国家都并列，最后由 `provider + NUL + release_ref + NUL + match_ref` 的字典序决定。选中 `07088ba6-...` 的第 2 轨 `5e5f141f-...`，实际写入 `imported=1 / review=0 / skipped=0`。详情独立核对为 2010-07-21、目录号 **COZA-457**。

每种输入执行 22 组候选旋转/反转，选中身份不变。以下为**刻意构造的反事实对照，不是真实来源数据或用户历史状态**：

- 预置 `e8ab0e85-...` 发行的另一首测试歌曲，真实导入改选该发行，说明已有发行优先于字典序。
- 只将该末尾发行总数改为 5，改选它：总数优先于字典序。
- 其余改为 XW、仅末尾发行保留 JP，改选它：已知非世界国家优先于字典序。
- 修改显示载体偏好且让末尾发行成为 Digital Media，不改变 `07088ba6-...` 的结果：载体不是自动选版依据。
- 同发行出现两个目标位置、或候选的 recording 不同，仍复核。

这证明代码在上述完整候选与指定库状态下为什么作出选择；不将离线重放冒充用户某次历史操作的审计日志，也不说明该 CD 在内容质量上优于另外十个版本。本轮不改变选版规则。
