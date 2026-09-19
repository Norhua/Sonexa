package itunes

// searchResponse 是 /search 端点的响应。
type searchResponse struct {
	ResultCount int      `json:"resultCount"`
	Results     []result `json:"results"`
}

// lookupResponse 是 /lookup 端点的响应。
//
// results[0] 是 collection 自身，其后才是各曲目——两者必须区分处理。
type lookupResponse struct {
	ResultCount int      `json:"resultCount"`
	Results     []result `json:"results"`
}

// result 同时承载 collection 与 track 两种条目，靠 WrapperType 区分。
type result struct {
	WrapperType    string `json:"wrapperType"` // track | collection | artist
	Kind           string `json:"kind"`        // song | ...
	CollectionType string `json:"collectionType"`

	TrackID      int64 `json:"trackId"`
	CollectionID int64 `json:"collectionId"`
	ArtistID     int64 `json:"artistId"`

	TrackName      string `json:"trackName"`
	CollectionName string `json:"collectionName"`
	ArtistName     string `json:"artistName"`

	TrackNumber     int `json:"trackNumber"`
	TrackCount      int `json:"trackCount"`
	DiscNumber      int `json:"discNumber"`
	DiscCount       int `json:"discCount"`
	TrackTimeMillis int `json:"trackTimeMillis"`

	ReleaseDate      string `json:"releaseDate"`
	PrimaryGenreName string `json:"primaryGenreName"`
	Country          string `json:"country"`

	ArtworkURL100 string `json:"artworkUrl100"`
	ArtworkURL60  string `json:"artworkUrl60"`
}
