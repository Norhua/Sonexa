package qqmusic

import (
	"context"
	"testing"

	"github.com/norhua/sonexa/internal/playlists"
)

func TestPlaylistReorderAndDuplicateCountUsesOccurrenceIdentity(t *testing.T) {
	order := []int{1, 2, 1}
	p, _ := qqPlaylistServer(t, func(_, offset int) (int, any) {
		data := playlistFixture(len(order), offset)
		songs := []any{}
		for _, id := range order {
			songs = append(songs, map[string]any{"id": id})
		}
		data["songlist"] = songs
		return 200, playlistEnvelope(data)
	})
	ref := playlists.Ref{Provider: p.ID(), ID: "123456"}
	first, err := p.FetchPlaylist(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	order = []int{2, 1, 1, 3}
	reordered, err := p.FetchPlaylist(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.Entries[0].EntryKey != first.Entries[1].EntryKey || reordered.Entries[1].EntryKey != first.Entries[0].EntryKey || reordered.Entries[2].EntryKey != first.Entries[2].EntryKey {
		t.Fatal("reordering changed occurrence keys")
	}
	order = []int{1, 2}
	reduced, err := p.FetchPlaylist(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if reduced.Entries[0].EntryKey != first.Entries[0].EntryKey {
		t.Fatal("duplicate count reset survivor")
	}
}

func TestPlaylistLimitsAndMissingSongIdentity(t *testing.T) {
	for _, mode := range []string{"too-many", "song-identity"} {
		t.Run(mode, func(t *testing.T) {
			p, calls := qqPlaylistServer(t, func(_, offset int) (int, any) {
				data := playlistFixture(1, offset)
				if mode == "too-many" {
					data["total_song_num"] = playlists.MaxItems + 1
				} else {
					data["songlist"] = []any{map[string]any{"name": "missing identity"}}
				}
				return 200, playlistEnvelope(data)
			})
			if s, err := p.FetchPlaylist(context.Background(), playlists.Ref{Provider: p.ID(), ID: "123456"}); s != nil || err == nil || *calls != 1 {
				t.Fatalf("snapshot=%+v err=%v calls=%d", s, err, *calls)
			}
		})
	}
}
