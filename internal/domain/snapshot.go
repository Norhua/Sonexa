// Package domain 定义来源无关的身份、上游事实和当前值契约。
// 本包不依赖 provider、store 或应用组装。
package domain

import (
	"encoding/json"
	"fmt"
)

type FieldState string

const (
	Unknown FieldState = "unknown"
	Empty   FieldState = "empty"
	Value   FieldState = "value"
)

// Field 的零值表示未提供。empty 必须有明确的来源证据，不能由缺字段推导。
type Field[T any] struct {
	State FieldState `json:"state"`
	Value T          `json:"value,omitempty"`
}

func Known[T any](value T) Field[T] { return Field[T]{State: Value, Value: value} }
func Cleared[T any]() Field[T]      { return Field[T]{State: Empty} }
func Missing[T any]() Field[T]      { return Field[T]{State: Unknown} }
func (f Field[T]) IsKnown() bool    { return f.State == Value || f.State == Empty }
func (f Field[T]) MarshalJSON() ([]byte, error) {
	state := f.State
	if state == "" {
		state = Unknown
	}
	if state != Unknown && state != Empty && state != Value {
		return nil, fmt.Errorf("未知字段状态 %q", state)
	}
	if state != Value {
		return json.Marshal(struct {
			State FieldState `json:"state"`
		}{state})
	}
	return json.Marshal(struct {
		State FieldState `json:"state"`
		Value T          `json:"value"`
	}{state, f.Value})
}
func (f *Field[T]) UnmarshalJSON(data []byte) error {
	var raw struct {
		State FieldState      `json:"state"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.State != Unknown && raw.State != Empty && raw.State != Value {
		return fmt.Errorf("未知字段状态 %q", raw.State)
	}
	var value T
	if raw.State == Value {
		if len(raw.Value) == 0 || string(raw.Value) == "null" {
			return fmt.Errorf("value 状态必须提供非 null 值")
		}
		if err := json.Unmarshal(raw.Value, &value); err != nil {
			return err
		}
	} else if len(raw.Value) != 0 {
		return fmt.Errorf("%s 状态不能携带值", raw.State)
	}
	*f = Field[T]{State: raw.State, Value: value}
	return nil
}

// MergeField 不以缺字段降级已有事实。
func MergeField[T any](old, incoming Field[T]) Field[T] {
	if incoming.IsKnown() {
		return incoming
	}
	return old
}

type ProviderID string
type Identity struct {
	Provider ProviderID `json:"provider"`
	Ref      string     `json:"ref"`
}
type GroupIdentity = Identity
type ReleaseIdentity = Identity
type TrackIdentity = Identity
type DetailTarget struct {
	Release   ReleaseIdentity `json:"release"`
	Track     *TrackIdentity  `json:"track,omitempty"`
	SkipCache bool            `json:"skipCache"`
}

func (t DetailTarget) Validate() error {
	if t.Release.Provider == "" || t.Release.Ref == "" {
		return fmt.Errorf("缺少精确发行身份")
	}
	if t.Track != nil && (t.Track.Ref == "" || t.Track.Provider != t.Release.Provider) {
		return fmt.Errorf("曲目与发行来源不一致或曲目标识为空")
	}
	return nil
}

type Completeness string

const (
	Unloaded Completeness = "unknown"
	Partial  Completeness = "partial"
	Complete Completeness = "complete"
)

type SnapshotMeta struct {
	Provider     ProviderID   `json:"provider"`
	FetchedAt    string       `json:"fetchedAt"`
	Completeness Completeness `json:"completeness"`
}
type Identifier struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
type Label struct {
	Identity      *Identity `json:"identity,omitempty"`
	Name          string    `json:"name"`
	CatalogNumber string    `json:"catalogNumber"`
}
type Artist struct {
	Identity *Identity `json:"identity,omitempty"`
	Name     string    `json:"name"`
	SortName string    `json:"sortName"`
	Type     string    `json:"type"`
}
type CreditMember struct {
	Artist       Artist `json:"artist"`
	CreditedName string `json:"creditedName"`
	Start        int    `json:"start"`
	End          int    `json:"end"`
}
type Credit struct {
	Text    string         `json:"text"`
	Members []CreditMember `json:"members"`
}
type Role struct {
	Artist       Artist   `json:"artist"`
	Role         string   `json:"role"`
	Attributes   []string `json:"attributes"`
	CreditedName string   `json:"creditedName"`
	Position     int      `json:"position"`
}
type Medium struct {
	Number     int           `json:"number"`
	TrackTotal Field[int]    `json:"trackTotal"`
	Format     Field[string] `json:"format"`
}
type Artwork struct {
	Identity Identity `json:"identity"`
	URL      string   `json:"url"`
	Type     string   `json:"type"`
}

type GroupSnapshot struct {
	Meta             SnapshotMeta    `json:"meta"`
	Identity         GroupIdentity   `json:"identity"`
	Title            Field[string]   `json:"title"`
	Credit           Field[Credit]   `json:"credit"`
	PrimaryType      Field[string]   `json:"primaryType"`
	SecondaryTypes   Field[[]string] `json:"secondaryTypes"`
	FirstReleaseDate Field[string]   `json:"firstReleaseDate"`
}
type ReleaseSnapshot struct {
	Meta                  SnapshotMeta            `json:"meta"`
	Identity              ReleaseIdentity         `json:"identity"`
	Group                 *GroupIdentity          `json:"group,omitempty"`
	Title                 Field[string]           `json:"title"`
	Credit                Field[Credit]           `json:"credit"`
	PrimaryType           Field[string]           `json:"primaryType"`
	SecondaryTypes        Field[[]string]         `json:"secondaryTypes"`
	DiscTotal             Field[int]              `json:"discTotal"`
	Media                 Field[[]Medium]         `json:"media"`
	ReleaseTrackTotal     Field[int]              `json:"releaseTrackTotal"`
	Date                  Field[string]           `json:"date"`
	Country               Field[string]           `json:"country"`
	Status                Field[string]           `json:"status"`
	Labels                Field[[]Label]          `json:"labels"`
	Identifiers           Field[[]Identifier]     `json:"identifiers"`
	Annotation            Field[string]           `json:"annotation"`
	Artwork               Field[[]Artwork]        `json:"artwork"`
	Tracklist             Field[[]TracklistEntry] `json:"tracklist"`
	TracklistCompleteness Completeness            `json:"tracklistCompleteness"`
}
type TrackSnapshot struct {
	Meta            SnapshotMeta        `json:"meta"`
	Identity        TrackIdentity       `json:"identity"`
	Release         *ReleaseIdentity    `json:"release,omitempty"`
	Title           Field[string]       `json:"title"`
	Credit          Field[Credit]       `json:"credit"`
	Year            Field[int]          `json:"year"`
	DiscNumber      Field[int]          `json:"discNumber"`
	TrackNumber     Field[int]          `json:"trackNumber"`
	TrackNumberText Field[string]       `json:"trackNumberText"`
	Genre           Field[string]       `json:"genre"`
	Lyrics          Field[string]       `json:"lyrics"`
	DurationMS      Field[int]          `json:"durationMS"`
	Recording       Field[Identity]     `json:"recording"`
	Works           Field[[]Identity]   `json:"works"`
	Identifiers     Field[[]Identifier] `json:"identifiers"`
	// 各角色分别跟随；缺少某个键等同 unknown，不代表空集合。
	Roles map[string]Field[[]Role] `json:"roles"`
}
type TracklistEntry struct {
	DiscNumber  int                  `json:"discNumber"`
	TrackNumber int                  `json:"trackNumber"`
	Track       Field[TrackIdentity] `json:"track"`
	Recording   Field[Identity]      `json:"recording"`
	Title       Field[string]        `json:"title"`
	Credit      Field[Credit]        `json:"credit"`
	DurationMS  Field[int]           `json:"durationMS"`
	Identifiers Field[[]Identifier]  `json:"identifiers"`
}
type Snapshot struct {
	Group   *GroupSnapshot   `json:"group,omitempty"`
	Release *ReleaseSnapshot `json:"release,omitempty"`
	Track   *TrackSnapshot   `json:"track,omitempty"`
}

// ReleaseSummary 复用快照子集的字段类型，不用清单长度代替声明总数。
type ReleaseSummary struct {
	Meta              SnapshotMeta    `json:"meta"`
	Identity          ReleaseIdentity `json:"identity"`
	Group             *GroupIdentity  `json:"group,omitempty"`
	Title             Field[string]   `json:"title"`
	Credit            Field[Credit]   `json:"credit"`
	ReleaseTrackTotal Field[int]      `json:"releaseTrackTotal"`
	Media             Field[[]Medium] `json:"media"`
	Country           Field[string]   `json:"country"`
}

func (r ReleaseSnapshot) Summary() ReleaseSummary {
	return ReleaseSummary{Meta: r.Meta, Identity: r.Identity, Group: r.Group, Title: r.Title, Credit: r.Credit, ReleaseTrackTotal: r.ReleaseTrackTotal, Media: r.Media, Country: r.Country}
}

type ValueMode string

const (
	Follow ValueMode = "follow"
	User   ValueMode = "user"
)

type Current[T any] struct {
	Field  Field[T]   `json:"field"`
	Mode   ValueMode  `json:"mode"`
	Source ProviderID `json:"source"`
}

func (c Current[T]) Sync(f Field[T], source ProviderID) Current[T] {
	if c.Mode == Follow && f.IsKnown() {
		c.Field, c.Source = f, source
	}
	return c
}
