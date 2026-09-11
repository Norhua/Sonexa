package provider

import "time"

// Query 是一次上游查询的输入条件。三个文本字段可任意留空。
type Query struct {
	Title   string
	Artist  string
	Album   string
	Country string // iTunes 用；MB 忽略
	Limit   int
	// DurationMS 是导入输入的时长，用于 iTunes 的时长接近度补分；无时长时为 nil。
	DurationMS *int
}

// Ref 定位一条可拉取详情的上游对象。
type Ref struct {
	Provider ProviderID
	// ID 是详情请求的主标识（MB 用 release MBID，iTunes 用 collectionId）。
	ID string
	// TrackRef 是曲目粒度标识（MB 用 track MBID，iTunes 用 {country}:{trackId}）。
	TrackRef string
	Country  string
}

// Candidate 是 track 粒度的搜索候选：录音 × 发行 × 碟 × 位置的唯一交点。
//
// 所有字段用可空类型。缺失即缺失——用空串或零值伪装会让「署名精确匹配」
// 在两边都缺失时误判为匹配成功。
type Candidate struct {
	Provider ProviderID

	// 匹配标识：MB 用 track MBID，iTunes 用 {country}:{trackId}。
	MatchRef string

	// 展示与评分字段
	Title        *string
	ArtistName   *string // 曲目级主要艺术家署名（扁平串）
	AlbumTitle   *string
	PrimaryType  *string
	DurationMS   *int
	TrackNumber  *int
	TrackTotal   *int
	DiscNumber   *int
	Year         *int
	MediumFormat *string
	Country      *string

	// 上游相关度分值（仅 MB 提供）
	UpstreamScore *int

	// ReleaseRef 是该候选所属发行的上游标识（MB 为 release MBID，
	// iTunes 为 {country}:{collectionId}）。封面降级链与清单写入都需要它，
	// 它与 ReleaseGroupRef 是两个不同层级，不能互相代替。
	ReleaseRef *string

	// release 优选阶梯所需
	ReleaseStatus    *string
	FirstReleaseDate *string
	HasCover         bool
	ReleaseGroupRef  *string // 同一 release group 的多个 release 归为一组

	// 去重与桥接
	RecordingRef *string // MB recording MBID，用于「已有其他版本」判定
	ISRC         *string

	// 缩略图地址（列表用）
	ThumbURL *string
}

// CoverImage 是封面档案中枚举出的一张图片。
type CoverImage struct {
	Type   string // Front | Back | Booklet | Medium | Tray | ...
	URL    string
	Width  *int
	Height *int
}

// CoverGroup 是一条候选返回的一组封面（可能含多个尺寸档）。
type CoverGroup struct {
	Provider ProviderID
	// SourceRef 是这组封面所属的上游对象标识（release MBID / collectionId）。
	SourceRef string
	Title     *string
	Artist    *string
	Images    []CoverImage
	// Constructed 为真表示尺寸靠构造地址得到（iTunes），选中时需先验证再采用。
	Constructed bool
	Confidence  float64
}

// Detail 是一次详情拉取的完整结果。字段远多于 Candidate，全部可空。
type Detail struct {
	Provider ProviderID
	MatchRef string

	Title       *string
	AlbumTitle  *string
	TrackCredit *string // 曲目级署名原串
	AlbumCredit *string // 发行级署名原串

	PrimaryType      *string
	SecondaryTypes   []string
	ReleaseStatus    *string
	MediumFormat     *string
	Country          *string
	Year             *int
	ReleaseDate      *string
	FirstReleaseDate *string

	TrackNumber     *int
	TrackNumberText *string
	TrackTotal      *int
	DiscNumber      *int
	DiscTotal       *int
	DurationMS      *int

	Genre         *string
	Label         *string
	CatalogNumber *string
	Comment       *string
	ISRC          *string

	// 上游标识
	RecordingRef    *string
	ReleaseRef      *string
	ReleaseGroupRef *string
	WorkRef         *string

	// 参与者：演唱、器乐、作曲、作词等
	Artists []DetailArtist

	// 该 release 的完整曲目清单（详情响应本就包含，顺带写入）
	Tracklist []TracklistEntry

	FetchedAt time.Time
}

// DetailArtist 是详情中的一位参与者。
type DetailArtist struct {
	Name         string
	SortName     *string
	Type         *string // person | group | character | ...
	MBID         *string
	Role         string // vocal | instrument | composer | lyricist | ...
	CreditedName *string
	Attribute    *string // role=instrument 时的乐器名
	Position     int
}

// TracklistEntry 是清单中的一个位置。
type TracklistEntry struct {
	DiscNumber      int
	TrackNumber     int
	TrackRef        *string
	RecordingRef    *string
	Title           string
	TrackCredit     *string
	DurationMS      *int
	TrackNumberText *string
}
