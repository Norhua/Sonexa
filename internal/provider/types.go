package provider

import (
	"github.com/norhua/sonexa/internal/domain"
	"time"
)

// Query 是一次上游查询的输入条件。三个文本字段可任意留空。
type Query struct {
	Title   string
	Artist  string
	Album   string
	Country string // iTunes 用；MB 忽略
	Limit   int
	// SkipCache 仅用于用户明确要求重新获取，不改变普通缓存命中行为。
	SkipCache bool
	// DurationMS 是导入输入的时长，用于 iTunes 的时长接近度补分；无时长时为 nil。
	DurationMS *int
	// ArtistAlternates 是艺术家的备选写法（括号里那种），作为并联锚点之一。
	// 平台习惯把另一种写法装在括号里，而上游用的往往正是括号里那个。
	ArtistAlternates []string
	// TitleOnly 为真时只以曲名检索，不带任何锚点——阶梯的兜底一级。
	TitleOnly bool
}

// Ref 定位一条可拉取详情的上游对象。
type Ref struct {
	SkipCache bool
	Provider  ProviderID
	// ID 是详情请求的主标识（MB 用 release MBID，iTunes 用 collectionId）。
	ID string
	// TrackRef 是曲目粒度标识（MB 用 track MBID，iTunes 用 {country}:{trackId}）。
	// 为空时只取发行/清单；非空但找不到目标时 FetchDetail 必须返回错误。
	TrackRef string
	Country  string
}

// Candidate 是 track 粒度的搜索候选：录音 × 发行 × 碟 × 位置的唯一交点。
//
// 所有字段用可空类型。缺失即缺失——用空串或零值伪装会让「署名精确匹配」
// 在两边都缺失时误判为匹配成功。
type Candidate struct {
	ArtworkCatalog     *domain.CoverCatalogInput
	Provider           ProviderID
	Credits            []CreditArtist
	AlbumCredits       []CreditArtist
	GroupCredits       []CreditArtist
	ReleaseIdentifiers []domain.Identifier

	// 匹配标识：MB 用 track MBID，iTunes 用 {country}:{trackId}。
	MatchRef string

	// 展示与评分字段
	Title             *string
	ArtistName        *string // 曲目级主要艺术家署名（扁平串）
	AlbumTitle        *string
	AlbumCredit       *string
	GroupTitle        *string
	GroupCredit       *string
	ReleaseTrackTotal *int
	MediumTrackTotal  *int
	Media             []domain.Medium
	Labels            []domain.Label
	Identifiers       []domain.Identifier
	PrimaryType       *string
	DurationMS        *int
	TrackNumber       *int
	TrackTotal        *int
	DiscNumber        *int
	Year              *int
	MediumFormat      *string
	Country           *string

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

	// 厂牌与目录号。iTunes 不提供，MB 在搜索响应中带回。
	//
	// 这两个字段的存在使 Capabilities 声明的 CapLabel / CapCatalogNumber
	// 真正有列可驱动：集合中全不支持则隐藏该列，部分支持则填占位短横线。
	Label         *string
	CatalogNumber *string

	// 去重与桥接
	RecordingRef *string // MB recording MBID，保留上游录音元数据；不用于跨 track 判定
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

// Detail 是一次详情拉取的完整结果。字段远多于 Candidate，全部可空。
type Detail struct {
	GroupTitle            *string
	GroupCredit           *string
	GroupCredits          []CreditArtist `json:"-"`
	AlbumCredits          []CreditArtist `json:"-"`
	ReleaseTrackTotal     *int
	MediumTrackTotal      *int
	Media                 []domain.Medium
	Labels                []domain.Label
	ReleaseIdentifiers    []domain.Identifier
	Identifiers           []domain.Identifier
	WorkRefs              []string
	TracklistCompleteness domain.Completeness
	Provider              ProviderID
	// MatchRef 只在精确定位曲目成功后赋值；仅取发行清单时为空。
	MatchRef string

	Title       *string
	AlbumTitle  *string
	TrackCredit *string // 曲目级署名原串
	AlbumCredit *string // 发行级署名原串
	// ArtworkURL 是上游明确提供的专辑图片地址，非参与者或人工采用信息。
	ArtworkURL *string `json:"-"`

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
	// Lyrics 是上游给出的歌词原文（通常为带时间标签的 LRC）。
	Lyrics *string

	// 上游标识
	RecordingRef    *string
	ReleaseRef      *string
	ReleaseGroupRef *string
	WorkRef         *string

	// Credits 保留上游结构化署名的边界，不把 joinphrase 当作艺术家名。
	Credits []CreditArtist `json:"-"` // 仅 Go 侧解析/落库使用，不进入前端详情模型。

	// 参与者：只含上游明确给出的角色关系，不由署名推断演唱。
	Artists []DetailArtist

	// 该 release 的完整曲目清单（详情响应本就包含，顺带写入）
	Tracklist []TracklistEntry

	// FetchedAt 不导出给前端：Detail 只在 Go 侧流转，绑定层导出的是
	// AlbumDetail.FetchedAt（RFC3339 串）。带 time.Time 会让 Wails 的
	// TS 模型生成器每次构建都打印 `Not found: time.Time`。
	FetchedAt time.Time `json:"-"`
}

// CreditArtist 是原串中的一个署名身份（字节区间）。
type CreditArtist struct {
	Identity      *domain.Identity
	Name          string
	CanonicalName string
	SortName      *string
	Type          *string
	MBID          *string
	Start         int
	End           int
}

// DetailArtist 是详情中的一位参与者。
type DetailArtist struct {
	Identity     *domain.Identity
	Attributes   []string
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
	Identifiers     []domain.Identifier
	DiscNumber      int
	TrackNumber     int
	TrackRef        *string
	RecordingRef    *string
	Title           string
	TrackCredit     *string
	DurationMS      *int
	TrackNumberText *string
}
