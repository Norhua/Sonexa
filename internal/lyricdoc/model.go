// Package lyricdoc provides source-independent lyric documents and format tools.
// It has no business, transport, registry or persistence dependencies.
package lyricdoc

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
	Code      string     `json:"code"`
	Message   string     `json:"message"`
	Inference *Inference `json:"inference,omitempty"`
}

// Inference records a producer's bounded explanation, not a consumer rule.
// Source positions are zero-based; times retain the source's millisecond axis.
type Inference struct {
	RuleVersion  string            `json:"ruleVersion"`
	Layer        int               `json:"layer"`
	Count        int               `json:"count"`
	Associations []LineAssociation `json:"associations,omitempty"`
}

type LineAssociation struct {
	SourcePosition   int   `json:"sourcePosition"`
	OriginalPosition int   `json:"originalPosition"`
	SourceTimeMS     int64 `json:"sourceTimeMs"`
	AssociatedTimeMS int64 `json:"associatedTimeMs"`
}

// Track retains the selected normalized channel, including real line/word
// ends. It is not a second alternative original awaiting business selection.
type Track struct {
	Lines       []Line       `json:"lines"`
	Metadata    []string     `json:"metadata,omitempty"`
	OffsetMS    int64        `json:"offsetMs"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type PreviewRow struct {
	TimeMS       *int64   `json:"timeMs,omitempty"`
	Text         string   `json:"text"`
	Channel      string   `json:"channel"`
	Translation  []string `json:"translation,omitempty"`
	Romanization []string `json:"romanization,omitempty"`
}

// Channel is a completed channel in a Document; builders also accept it as
// input. Err is adapter-local error provenance, never a bridge DTO field.
type Channel struct {
	Track       *Track       `json:"track,omitempty"`
	Text        string       `json:"text,omitempty"`
	Status      string       `json:"status"`
	Origin      string       `json:"origin"`
	SyncStatus  string       `json:"syncStatus"`
	Message     string       `json:"message,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Err         error        `json:"-"`
}

// Document is immutable after construction. Version, preview approval and
// scores belong to the consuming session, not the provider's document.
type Document struct {
	Text         string       `json:"text"`
	Format       string       `json:"format"`
	Rows         []PreviewRow `json:"rows"`
	OffsetMS     int64        `json:"offsetMs"`
	Additional   []*Channel   `json:"additional,omitempty"`
	Original     *Channel     `json:"original,omitempty"`
	Translation  *Channel     `json:"translation,omitempty"`
	Romanization *Channel     `json:"romanization,omitempty"`
	WordTiming   string       `json:"wordTiming,omitempty"`
	Diagnostics  []Diagnostic `json:"diagnostics,omitempty"`
	Warnings     []string     `json:"warnings,omitempty"`

	// The builder seals all delivered projections after validating them. A
	// consumer can reject missing/changed content without running assembly,
	// parsing, channel classification or serialization a second time.
	integrity [32]byte
}

func ms(n int64) *int64 { return &n }
