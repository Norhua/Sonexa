package qqmusic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
)

const sharedPlaylistURL = "https://i2.y.qq.com/n3/other/pages/details/playlist.html?platform=11&appshare=android_qq&appversion=20090508&hosteuin=NK6zNe4FoKCF&id=9385819575&ADTAG=wxfshare"

func TestMobilePlaylistShare(t *testing.T) {
	p := &Provider{}
	for _, input := range []string{sharedPlaylistURL, "9385819575", "https://y.qq.com/n3/other/pages/details/playlist.html?id=9385819575"} {
		ref, err := p.ParsePlaylist(input)
		if err != nil || ref.ID != "9385819575" {
			t.Fatal(ref, err)
		}
	}
	for _, input := range []string{
		"https://i2.y.qq.com.evil.test/n3/other/pages/details/playlist.html?id=9385819575",
		"https://i2.y.qq.com:443/n3/other/pages/details/playlist.html?id=9385819575",
		"https://user@i2.y.qq.com/n3/other/pages/details/playlist.html?id=9385819575",
		"https://i2.y.qq.com/n3/other/pages/details/song.html?id=9385819575",
		"https://i2.y.qq.com/n3/other/pages/details/playlist.html?id=1&id=2",
		"https://i2.y.qq.com/n3/other/pages/details/playlist.html?id=1&disstid=2",
		"https://i2.y.qq.com/n3/other/pages/details/playlist.html?id=1#other",
	} {
		if _, err := p.ParsePlaylist(input); err == nil {
			t.Fatal("accepted", input)
		}
	}
}

// Sanitized shape of the reported 1067-slot playlist: slots 50/150/250
// omit type-13 songs. total_song_num subtracts ONLY the current page's
// filtered count; later pages restore the apparent total to 1067.
func filteredPlaylistFixture(offset int) map[string]any {
	d := playlistFixture(1067, offset)
	songs := d["songlist"].([]any)
	filtered := []any{}
	kept := []any{}
	for _, song := range songs {
		id := song.(map[string]any)["id"].(int)
		if id == 50 || id == 150 || id == 250 {
			filtered = append(filtered, map[string]any{"songid": id, "songtype": 13})
		} else {
			kept = append(kept, song)
		}
	}
	songs = kept
	d["songlist"], d["filtered_song"] = songs, filtered
	d["songlist_size"] = len(songs)
	d["total_song_num"] = 1067 - len(filtered)
	d["hasmore"] = boolInt(offset+playlistPageSize < 1067)
	return d
}

func TestPlaylistFilteredSongsUseRawOffsetsAndTotal(t *testing.T) {
	p, calls := qqPlaylistServer(t, func(call, offset int) (int, any) {
		if offset != (call-1)*playlistPageSize {
			t.Errorf("wrong raw offset %d on call %d", offset, call)
		}
		return 200, playlistEnvelope(filteredPlaylistFixture(offset))
	})
	s, err := p.FetchPlaylist(context.Background(), playlists.Ref{Provider: p.ID(), ID: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 2 || len(s.Entries) != 1064 || len(s.Excluded) != 3 || s.Completeness.RawSeen != 1067 {
		t.Fatal(s.Completeness, *calls)
	}
	for i, e := range s.Excluded {
		if e.Position != 0 || e.TrackRef != fmt.Sprint(i*100+50) || e.Reason == "" {
			t.Fatal(e)
		}
	}
	if s.Entries[49].TrackRef != "51" {
		t.Fatal("lost relative order")
	}
}

func TestPlaylistFilteredSongsDoNotHideIncompleteResponse(t *testing.T) {
	for _, mode := range []string{"changed-total", "changed-name", "unknown-filter", "missing-filter-id", "invalid-song", "unexplained-filter-count", "wrong-size", "wrong-more", "short-page", "business"} {
		t.Run(mode, func(t *testing.T) {
			p, calls := qqPlaylistServer(t, func(call, offset int) (int, any) {
				d := filteredPlaylistFixture(offset)
				if call == 2 {
					switch mode {
					case "changed-total":
						d["total_song_num"] = 1068
					case "changed-name":
						d["dirinfo"].(map[string]any)["title"] = "changed"
					case "unknown-filter":
						d["filtered_song"] = []any{map[string]any{"songid": 150, "songtype": 99}}
					case "missing-filter-id":
						d["filtered_song"] = []any{map[string]any{"songtype": 13}}
					case "invalid-song":
						d["invalid_song"] = []any{1}
					case "unexplained-filter-count":
						d["filter_song_num"] = 1
					case "wrong-size":
						d["songlist_size"] = 100
					case "wrong-more":
						d["hasmore"] = 1
					case "short-page":
						d["songlist"] = d["songlist"].([]any)[:60]
						d["songlist_size"] = 60
					case "business":
						d["subcode"] = 1
					}
				}
				return 200, playlistEnvelope(d)
			})
			if s, err := p.FetchPlaylist(context.Background(), playlists.Ref{Provider: p.ID(), ID: "123456"}); err == nil || s != nil || *calls != 2 {
				t.Fatal(s, err, *calls)
			}
		})
	}
}

func TestPlaylistNumericIdentityIsSharedWithFilteredSongs(t *testing.T) {
	for _, raw := range []string{`{"id":102182145,"mid":"oldMID"}`, `{"songid":"102182145"}`} {
		e, err := parsePlaylistSong(json.RawMessage(raw))
		if err != nil || e.TrackRef != "102182145" {
			t.Fatal(e, err)
		}
	}
	for _, raw := range []string{`{"mid":"00000000000001"}`, `{"id":1,"songid":2}`, `{"id":0}`} {
		if _, err := parsePlaylistSong(json.RawMessage(raw)); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestPlaylistLiveReadOnly(t *testing.T) {
	input := os.Getenv("SONEXA_QQ_PLAYLIST_LIVE")
	if input == "" {
		t.Skip("opt in with SONEXA_QQ_PLAYLIST_LIVE=public-playlist-ID; read only, no database")
	}
	limiters := provider.NewLimiters()
	limiters.Configure(provider.QQQuota, 1, 2*time.Second)
	client := provider.NewClient(limiters, nil, "Sonexa-playlist-live")
	client.Retry.MaxAttempts = 1
	audit := &playlistLiveTransport{base: http.DefaultTransport}
	client.HTTP.Transport = audit
	p := New(client, Config{}, nil)
	ref, err := p.ParsePlaylist(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sends := 0
	ctx = provider.WithRequestGate(ctx, func() error {
		sends++
		if sends > 100 {
			return fmt.Errorf("live request limit")
		}
		return nil
	})
	s, err := p.FetchPlaylist(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("read-only ref=%s tracks=%d excluded=%d raw=%d pages=%d sends=%d gateChecks=%d", ref.ID, len(s.Entries), len(s.Excluded), s.Completeness.RawSeen, s.Completeness.Pages, audit.sends, sends)
}

type playlistLiveTransport struct {
	base  http.RoundTripper
	sends int
}

func (a *playlistLiveTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if a.sends >= 20 {
		return nil, fmt.Errorf("live send limit")
	}
	a.sends++
	return a.base.RoundTrip(request)
}
