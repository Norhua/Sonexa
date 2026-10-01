// Package musicbrainz 接入 MusicBrainz 作为主要上游。
//
// MB 提供本应用所需的绝大多数结构化字段，但严格限速（平均 1 req/s）
// 且要求规范的请求标识，因此必须对请求频次精打细算：
// 详情一次拉满、查询变体渐进式、曲目清单懒加载。
package musicbrainz

// searchResponse 是 /ws/2/recording 搜索端点的响应。
type searchResponse struct {
	Count      int         `json:"count"`
	Offset     int         `json:"offset"`
	Recordings []recording `json:"recordings"`
}

// recording 是一条录音。搜索响应中它携带 releases[]，
// 展开为 (recording × release × medium × track) 即为 track 粒度候选。
type recording struct {
	ID             string         `json:"id"`
	Score          int            `json:"score"`
	Title          string         `json:"title"`
	Length         *int           `json:"length"` // 毫秒
	Disambiguation string         `json:"disambiguation"`
	ArtistCredit   []artistCredit `json:"artist-credit"`
	Releases       []release      `json:"releases"`
	ISRCs          []string       `json:"isrcs"`
	// Relations 在详情响应中携带 recording-level-rels（演唱者、器乐演奏者）
	// 与 work-rels（经 Work 取得作曲、作词）。
	Relations []relation `json:"relations"`
}

// artistCredit 是署名的一个部件。JoinPhrase 保留了 feat. / & / × 这类连接词——
// 它们无法从关系表无损重建，因此原串必须原样保真。
type artistCredit struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
	Artist     artist `json:"artist"`
}

type artist struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	SortName       string     `json:"sort-name"`
	Type           string     `json:"type"` // Person | Group | Character | ...
	Disambiguation string     `json:"disambiguation"`
	Relations      []relation `json:"relations"`
}

// release 是一张发行。
type release struct {
	TrackCount         *int                `json:"track-count"`
	Barcode            string              `json:"barcode"`
	ID                 string              `json:"id"`
	Title              string              `json:"title"`
	Status             string              `json:"status"` // Official | Promotion | Bootleg | Pseudo-Release
	Date               string              `json:"date"`
	Country            string              `json:"country"`
	Disambiguation     string              `json:"disambiguation"`
	Quality            string              `json:"quality"`
	ArtistCredit       []artistCredit      `json:"artist-credit"`
	ReleaseGroup       *releaseGroup       `json:"release-group"`
	Media              []medium            `json:"media"`
	LabelInfo          []labelInfo         `json:"label-info"`
	CoverArtArchive    *coverArtArchive    `json:"cover-art-archive"`
	MediumCount        int                 `json:"medium-count"`
	Genres             []genre             `json:"genres"`
	Annotation         string              `json:"annotation"`
	TextRepresentation *textRepresentation `json:"text-representation"`
}

type textRepresentation struct {
	Language string `json:"language"`
	Script   string `json:"script"`
}

type coverArtArchive struct {
	Artwork bool `json:"artwork"`
	Front   bool `json:"front"`
	Back    bool `json:"back"`
	Count   int  `json:"count"`
}

type releaseGroup struct {
	ArtistCredit     []artistCredit `json:"artist-credit"`
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	PrimaryType      string         `json:"primary-type"`
	SecondaryTypes   []string       `json:"secondary-types"`
	FirstReleaseDate string         `json:"first-release-date"`
}

// medium 是一张碟（CD / Vinyl / Digital Media ...）。
type medium struct {
	Position   int    `json:"position"`
	Format     string `json:"format"`
	Title      string `json:"title"`
	TrackCount int    `json:"track-count"`

	// Tracks 是详情端点（/ws/2/release/{mbid}?inc=recordings+media）用的键名。
	Tracks []track `json:"tracks"`
	// SearchTracks 是搜索端点（/ws/2/recording?query=…）用的键名。
	//
	// 同一个 media 对象，两个端点的曲目数组键名不同：详情是 `tracks`，
	// 搜索是 `track`。只认其中一个的后果不是「少一个字段」——搜索结果里
	// 每条候选都会走发行级兜底，于是 match_ref 退化成合成串而不是 track MBID，
	// 载体格式、曲序、碟序一并丢失。载体格式正是分流规则第 3 条的过滤依据。
	SearchTracks []track `json:"track"`
}

// TrackList 返回该碟的曲目，抹平两个端点的键名差异。
func (m medium) TrackList() []track {
	if len(m.Tracks) > 0 {
		return m.Tracks
	}
	return m.SearchTracks
}

// track 是碟上的一个位置。
//
// track 的 id 是 (recording × release × medium × 位置) 的唯一交点，
// 正是 match_ref 所需的粒度——不可用 recording MBID 代替。
type track struct {
	ID           string         `json:"id"`
	Position     int            `json:"position"`
	Number       string         `json:"number"` // 黑胶可能是 "A1"
	Title        string         `json:"title"`
	Length       *int           `json:"length"`
	ArtistCredit []artistCredit `json:"artist-credit"`
	Recording    *recording     `json:"recording"`
}

type labelInfo struct {
	CatalogNumber string `json:"catalog-number"`
	Label         *label `json:"label"`
}

type label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type genre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// relation 是一条关系。作曲/作词挂在 Work 上，演唱/器乐挂在 Recording 上。
type relation struct {
	Type       string   `json:"type"`
	TypeID     string   `json:"type-id"`
	Direction  string   `json:"direction"`
	Attributes []string `json:"attributes"` // role=instrument 时为乐器名
	Artist     *artist  `json:"artist"`
	Work       *work    `json:"work"`
	TargetType string   `json:"target-type"`
}

// work 是作品。作曲与作词经此层级取得，而不是因为录音层级没有就判为缺失。
type work struct {
	ISWCs     []string   `json:"iswcs"`
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Relations []relation `json:"relations"`
}

// releaseDetail 是 /ws/2/release/{mbid} 详情端点的响应。
// 它就是 release 本身，只是 inc 参数带回了更多嵌套内容。
type releaseDetail = release

// artistDetail 是 /ws/2/artist/{mbid} 的响应，用于取角色↔声优关系。
type artistDetail = artist
