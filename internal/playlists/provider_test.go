package playlists

import (
	"context"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

type playlistOnly struct{ id provider.ProviderID }

func (p *playlistOnly) ID() provider.ProviderID             { return p.id }
func (p *playlistOnly) DisplayName() string                 { return "仅歌单替身" }
func (p *playlistOnly) PlaylistAvailability() Availability  { return Availability{Available: true} }
func (p *playlistOnly) ParsePlaylist(s string) (Ref, error) { return Ref{Provider: p.id, ID: s}, nil }
func (p *playlistOnly) FetchPlaylist(_ context.Context, r Ref) (*Snapshot, error) {
	s := validSnapshot()
	s.Ref = r
	return s, nil
}

func validSnapshot() *Snapshot {
	return &Snapshot{Ref: Ref{Provider: "stub", ID: "list"}, Name: "歌单", FetchedAt: time.Now(),
		Entries:      []Entry{{EntryKey: "track:1", TrackRef: "track", Position: 1}, {EntryKey: "track:2", TrackRef: "track", Position: 3}},
		Excluded:     []Excluded{{Position: 2, Kind: "unsupported"}},
		Completeness: Completeness{Complete: true, RawSeen: 3, DeclaredTotal: 3, Pages: 1, TrackCount: 2, ExcludedCount: 1}}
}

func TestSnapshotContract(t *testing.T) {
	if err := validSnapshot().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Snapshot){
		"missing identity":      func(s *Snapshot) { s.Entries[0].TrackRef = "" },
		"duplicate key":         func(s *Snapshot) { s.Entries[1].EntryKey = s.Entries[0].EntryKey },
		"duplicate position":    func(s *Snapshot) { s.Excluded[0].Position = 1 },
		"reversed order":        func(s *Snapshot) { s.Entries[0], s.Entries[1] = s.Entries[1], s.Entries[0] },
		"incomplete":            func(s *Snapshot) { s.Completeness.Complete = false },
		"unknown total":         func(s *Snapshot) { s.Completeness.DeclaredTotal = -1 },
		"unexplained filtering": func(s *Snapshot) { s.Excluded = nil },
		"zero proof":            func(s *Snapshot) { s.Completeness.Pages = 0 },
		"source missing":        func(s *Snapshot) { s.Ref.Provider = "" },
		"negative duration":     func(s *Snapshot) { v := int64(-1); s.Entries[0].DurationMS = &v },
	} {
		t.Run(name, func(t *testing.T) {
			s := validSnapshot()
			change(s)
			if Code(s.Validate()) != Incomplete {
				t.Fatal("invalid snapshot accepted")
			}
		})
	}
}

func TestExplicitEmptyAndMissingBasicFields(t *testing.T) {
	s := validSnapshot()
	s.Entries, s.Excluded = nil, nil
	s.Completeness = Completeness{Complete: true, Pages: 1}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s = validSnapshot() // all basic fields intentionally unknown; repeats retained
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 2 || s.Entries[0].Title != nil {
		t.Fatal("altered missing fields or occurrences")
	}
	var missing *Snapshot
	if missing.Validate() == nil {
		t.Fatal("nil accepted")
	}
}

func TestRegistryOnlyPlaylistCapability(t *testing.T) {
	p := &playlistOnly{id: "stub"}
	r, err := NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := any(p).(provider.MetadataProvider); ok {
		t.Fatal("fixture unexpectedly implements metadata")
	}
	if got := r.List(); len(got) != 1 || !got[0].Available {
		t.Fatal(got)
	}
	got, err := r.Get("stub")
	if err != nil || got != p {
		t.Fatal(err)
	}
	ref, _ := got.ParsePlaylist("list")
	s, err := got.FetchPlaylist(context.Background(), ref)
	if err != nil || s.Validate() != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("unregistered"); err == nil {
		t.Fatal("unknown source accepted")
	}
	if _, err := NewRegistry(p, p); err == nil {
		t.Fatal("duplicate accepted")
	}
	var nilProvider *playlistOnly
	if _, err := NewRegistry(nilProvider); err == nil {
		t.Fatal("typed nil accepted")
	}
}
