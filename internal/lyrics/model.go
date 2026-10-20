package lyrics

// All timestamps are integer milliseconds. EndMS is present only when the
// source explicitly supplied an end; adjacent word starts are not durations.
type Word struct {
	StartMS *int64 `json:"startMs,omitempty"`
	EndMS   *int64 `json:"endMs,omitempty"`
	Text    string `json:"text"`
}

type Line struct {
	StartMS *int64 `json:"startMs,omitempty"`
	EndMS   *int64 `json:"endMs,omitempty"`
	Parts   []Word `json:"parts,omitempty"`
	Text    string `json:"text"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Track is backend-only. Text and rows in Document are projections of the same
// track, never independently reconstructed by a caller adopting lyrics.
type Track struct {
	Lines       []Line
	Metadata    []string
	OffsetMS    int64
	Diagnostics []Diagnostic
}

type PreviewRow struct {
	TimeMS       *int64   `json:"timeMs,omitempty"`
	Text         string   `json:"text"`
	Channel      string   `json:"channel"`
	Translation  []string `json:"translation,omitempty"`
	Romanization []string `json:"romanization,omitempty"`
}

func ms(n int64) *int64 { return &n }
