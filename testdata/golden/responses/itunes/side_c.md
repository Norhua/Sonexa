# Side-C 真实响应摘录

2026-09-24 查询截图 image14/image15 对应歌曲，未使用登录、Cookie、代理或音频端点。

- Search：`https://itunes.apple.com/search?term=ツバサ%204円%20side-C&entity=song&media=music&country=jp&limit=10`。实际返回 3 首，`search_side_c.json` 只保留对应的「ツバサ」和解析所需字段。
- Lookup：`https://itunes.apple.com/lookup?id=1475065332&entity=song&country=jp&limit=200`。`lookup_side_c.json` 保留专辑及完整 4 首清单的必要字段。
- 删除价格、试听 URL、商店跳转链接等无关字段；不保存音频/图片字节。封面 URL 是公开响应中的真实地址。
- 搜索标题为 `side-c - EP`，Lookup 专辑标题为 `Side-C`；身份均为 `jp:1475065332`。
- 响应有 artwork、署名、日期、风格、时长及曲目位置，没有演唱/作曲/作词关系。`country=JPN` 是商店地区，不作为发行国家写入。
