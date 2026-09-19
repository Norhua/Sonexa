# 綿雪の夢：有署名但没有角色关系

`release_snow_no_roles.json` 精简自 2026-09-18 的公开 MusicBrainz 发行详情响应：

- 发行：`54953b76-c791-4415-8092-1f5487cb8df9`
- 第 4 曲：`3d9c04e3-2f21-4e1f-a307-e40a96ff82cf`
- 录音：`7f4f192d-efb5-4d52-9005-74a435b1fd00`
- 请求参数与生产 `detailInc` 一致：`artist-credits+labels+recordings+release-groups+media+artist-rels+recording-level-rels+work-rels+work-level-rels+genres+isrcs+annotation`，`fmt=json`。
- [发行 API](https://musicbrainz.org/ws/2/release/54953b76-c791-4415-8092-1f5487cb8df9?fmt=json&inc=artist-credits%2Blabels%2Brecordings%2Brelease-groups%2Bmedia%2Bartist-rels%2Brecording-level-rels%2Bwork-rels%2Bwork-level-rels%2Bgenres%2Bisrcs%2Bannotation)
- [独立录音 API](https://musicbrainz.org/ws/2/recording/7f4f192d-efb5-4d52-9005-74a435b1fd00?fmt=json&inc=artist-credits%2Bartist-rels%2Bwork-rels%2Breleases) 同样返回 `relations: []`。

两处都提供「ハトリアヤ」的 `artist-credit`，但没有 vocal / work 角色关系。
这证明抽屉没有演唱、作曲、作词信息不是解析丢字段，不能为填满 UI 再推断 vocal。

精简仅保留目标曲目及必要发行字段，移除了另外 7 首与图片、空标签等无关字段；
`track-count: 8` 保持原值，因此该夹具是截断清单，不得标为 complete。
与 `internal/app/enrich_identity_test.go` 的**人工构造** `enrichmentRelease` 区分：后者故意包含
vocal / composer 关系来验证串曲保护，不能当作此真实录音具有这些关系的证据。
