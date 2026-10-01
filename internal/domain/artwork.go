package domain

type CoverCatalogKey struct {
	Source     string `json:"source"`
	ObjectType string `json:"objectType"`
	SourceRef  string `json:"sourceRef"`
}

type CoverImage struct {
	ID            int64          `json:"id"`
	CatalogID     int64          `json:"catalogId"`
	SourceImageID string         `json:"sourceImageId"`
	Types         []string       `json:"types"`
	PrimaryType   string         `json:"primaryType"`
	Front         bool           `json:"front"`
	Back          bool           `json:"back"`
	Position      int            `json:"position"`
	OriginalURL   string         `json:"originalUrl"`
	Title         string         `json:"title"`
	Artist        string         `json:"artist"`
	Active        bool           `json:"active"`
	Variants      []CoverVariant `json:"variants"`
}

type CoverVariant struct {
	ID          int64  `json:"id"`
	ImageID     int64  `json:"imageId"`
	Spec        string `json:"spec"`
	URL         string `json:"url"`
	Constructed bool   `json:"constructed"`
	Width       *int   `json:"width"`
	Height      *int   `json:"height"`
	Format      string `json:"format"`
	State       string `json:"state"`
	LastError   string `json:"lastError"`
	Active      bool   `json:"active"`
}

type CoverCatalogInput struct {
	Key              CoverCatalogKey
	Images           []CoverImage
	ActualReleaseURL string
}
