// Package playlists owns the read-only playlist capability and snapshot contract.
// Platform protocols and runtime management belong to provider/<upstream>.
package playlists

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/provider"
)

const (
	MaxItems         = 20_000
	MaxPages         = 200
	MaxSends         = 500
	MaxSnapshotBytes = 32 << 20
	MaxResponseBytes = 8 << 20
	MaxResponseTotal = 256 << 20
	FetchTimeout     = 15 * time.Minute
)

type Ref struct {
	Provider provider.ProviderID `json:"provider"`
	ID       string              `json:"id"`
}

func (r Ref) Validate() error {
	if !identity(string(r.Provider)) || !identity(r.ID) {
		return Failure(Input, "歌单缺少规范身份")
	}
	return nil
}

type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Provider requires no metadata, lyrics or cover capability. Availability must
// not make network requests; a missing runtime remains visible as unavailable.
type Provider interface {
	provider.Upstream
	PlaylistAvailability() Availability
	ParsePlaylist(string) (Ref, error)
	FetchPlaylist(context.Context, Ref) (*Snapshot, error)
}

// EntryKey identifies an occurrence, not an absolute position or a local track.
// Nil basic fields mean unknown. Empty known values remain empty.
type Entry struct {
	EntryKey   string   `json:"entryKey"`
	TrackRef   string   `json:"trackRef"`
	Position   int      `json:"position"`
	Title      *string  `json:"title"`
	Artists    []string `json:"artists"`
	Album      *string  `json:"album"`
	DurationMS *int64   `json:"durationMS"`
}

type Excluded struct {
	Position int    `json:"position"`
	Kind     string `json:"kind"`
}

type Completeness struct {
	Complete      bool `json:"complete"`
	RawSeen       int  `json:"rawSeen"`
	DeclaredTotal int  `json:"declaredTotal"`
	Pages         int  `json:"pages"`
	TrackCount    int  `json:"trackCount"`
	ExcludedCount int  `json:"excludedCount"`
}

type Snapshot struct {
	Ref          Ref          `json:"ref"`
	Name         string       `json:"name"`
	FetchedAt    time.Time    `json:"fetchedAt"`
	Entries      []Entry      `json:"entries"`
	Excluded     []Excluded   `json:"excluded"`
	Completeness Completeness `json:"completeness"`
	Diagnostics  []string     `json:"diagnostics,omitempty"`
}

// Validate checks the consumer contract, not native response interpretation.
// Only the adapter can prove page coverage and classify exclusions.
func (s *Snapshot) Validate() error {
	bad := func() error { return Failure(Incomplete, "歌单快照不完整或身份无效") }
	if s == nil || s.Ref.Validate() != nil || strings.TrimSpace(s.Name) == "" || !utf8.ValidString(s.Name) || s.FetchedAt.IsZero() {
		return bad()
	}
	c := s.Completeness
	if !c.Complete || c.DeclaredTotal < 0 || c.DeclaredTotal > MaxItems || c.RawSeen != c.DeclaredTotal || c.Pages < 1 || c.Pages > MaxPages ||
		c.TrackCount != len(s.Entries) || c.ExcludedCount != len(s.Excluded) || c.TrackCount+c.ExcludedCount != c.RawSeen {
		return bad()
	}
	keys, positions := map[string]bool{}, map[int]bool{}
	bytes := len(s.Name)
	previous := 0
	for _, e := range s.Entries {
		if !identity(e.EntryKey) || !identity(e.TrackRef) || keys[e.EntryKey] || e.Position <= previous || e.Position > c.RawSeen ||
			(e.DurationMS != nil && *e.DurationMS < 0) {
			return bad()
		}
		keys[e.EntryKey], positions[e.Position], previous = true, true, e.Position
		bytes += len(e.EntryKey) + len(e.TrackRef) + 128
		for _, value := range []*string{e.Title, e.Album} {
			if value != nil {
				if !utf8.ValidString(*value) {
					return bad()
				}
				bytes += len(*value)
			}
		}
		for _, artist := range e.Artists {
			if !utf8.ValidString(artist) {
				return bad()
			}
			bytes += len(artist) + 16
		}
	}
	previous = 0
	for _, e := range s.Excluded {
		if !identity(e.Kind) || e.Position <= previous || e.Position > c.RawSeen || positions[e.Position] {
			return bad()
		}
		positions[e.Position], previous = true, e.Position
		bytes += len(e.Kind) + 32
	}
	if len(positions) != c.RawSeen {
		return bad()
	}
	for _, diagnostic := range s.Diagnostics {
		bytes += len(diagnostic)
	}
	if bytes > MaxSnapshotBytes {
		return Failure(Resource, "歌单快照超过大小上限")
	}
	return nil
}

func identity(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= 512 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func OccurrenceKey(ref string, occurrence int) string { return fmt.Sprintf("%s:%d", ref, occurrence) }
