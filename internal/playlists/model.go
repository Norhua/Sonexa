package playlists

import "time"

// Binding stores only canonical identity, not a user-supplied URL.
type Binding struct {
	ID             int64        `json:"id"`
	Ref            Ref          `json:"ref"`
	UpstreamName   string       `json:"upstreamName"`
	Alias          string       `json:"alias"`
	Revision       int64        `json:"revision"`
	LastSyncedAt   time.Time    `json:"-"`
	FetchedAt      time.Time    `json:"-"`
	SnapshotDigest string       `json:"-"`
	Completeness   Completeness `json:"-"`
	Excluded       []Excluded   `json:"excluded"`
}

func (b Binding) DisplayName() string {
	if b.Alias != "" {
		return b.Alias
	}
	return b.UpstreamName
}

type LocalState string

const (
	Pending  LocalState = "pending"
	Imported LocalState = "imported"
	Ignored  LocalState = "ignored"
)

// StoredEntry represents an occurrence, including an imported absent occurrence.
// Its snapshot is separate from both immutable import input and track_current.
type StoredEntry struct {
	Entry
	ID              int64      `json:"id"`
	PlaylistID      int64      `json:"playlistId"`
	UpstreamPresent bool       `json:"upstreamPresent"`
	LocalState      LocalState `json:"localState"`
	TrackID         *int64     `json:"trackId"`
	Revision        int64      `json:"revision"`
}
