package qqmusic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
)

func TestPlaylistInputs(t *testing.T) {
	p := &Provider{}
	for _, input := range []string{"123456", "000123456", "https://y.qq.com/n/ryqq/playlist/123456?si=share", "https://y.qq.com/n/m/detail/taoge/index.html?id=123456&ADTAG=share", "https://y.qq.com/n/yqq/playlist.html?disstid=123456", "https://y.qq.com/n2/m/share/details/taoge.html?disstid=123456&ADTAG=share", "https://y.qq.com/n/ryqq/taoge/123456"} {
		t.Run(input, func(t *testing.T) {
			ref, err := p.ParsePlaylist(input)
			if err != nil || ref.ID != "123456" || ref.Provider != provider.QQMusic {
				t.Fatalf("%+v %v", ref, err)
			}
		})
	}
	for _, input := range []string{"0", "-1", "1;echo hello", "https://evil.test/playlist/123456", "https://y.qq.com.evil.test/n/ryqq/playlist/123456", "https://y.qq.com/n/ryqq/song/123456", "https://y.qq.com/?id=123456", "https://user@y.qq.com/n/ryqq/playlist/123456", "https://y.qq.com:444/n/ryqq/playlist/123456", "https://y.qq.com/n/yqq/playlist.html?id=1&id=2", "https://y.qq.com/n/yqq/playlist.html?id=1&disstid=2", "9223372036854775808"} {
		t.Run(input, func(t *testing.T) {
			if _, err := p.ParsePlaylist(input); err == nil {
				t.Fatal("invalid URL accepted")
			}
		})
	}
}

func playlistFixture(total, offset int) map[string]any {
	songs := []any{}
	for n := offset; n < min(total, offset+100); n++ {
		songs = append(songs, map[string]any{"id": n + 1, "mid": fmt.Sprintf("%014d", n+1), "name": "曲名", "album": map[string]any{"name": "专辑"}, "singer": []any{map[string]any{"name": "署名一"}, map[string]any{"name": "署名二"}}, "interval": 123})
	}
	return map[string]any{"disstid": 123456, "dirinfo": map[string]any{"title": "歌单"}, "total_song_num": total, "songlist": songs}
}

func qqPlaylistServer(t *testing.T, reply func(int, int) (int, any)) (*Provider, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != musicuPath || r.Method != "POST" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		var request struct {
			Playlist struct {
				Module, Method string
				Param          struct {
					ID    int64 `json:"disstid"`
					Begin int   `json:"song_begin"`
					Num   int   `json:"song_num"`
				}
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Playlist.Module != "music.srfDissInfo.DissInfo" || request.Playlist.Method != "CgiGetDiss" || request.Playlist.Param.ID != 123456 || request.Playlist.Param.Num != 100 {
			t.Errorf("request=%+v", request)
		}
		status, body := reply(calls, request.Playlist.Param.Begin)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	old := MusicuBase
	MusicuBase = server.URL
	t.Cleanup(func() { MusicuBase = old })
	client := provider.NewClient(nil, panicPlaylistCache{}, "test")
	client.Retry.BaseDelay = 0
	client.Retry.MaxDelay = 0
	return New(client, Config{}, nil), &calls
}

type panicPlaylistCache struct{}

func (panicPlaylistCache) GetCache(string, string, string) (*provider.CacheEntry, error) {
	panic("playlist cache read")
}
func (panicPlaylistCache) PutCache(string, string, string, []byte, time.Duration) error {
	panic("playlist cache write")
}

func playlistEnvelope(data any) any {
	return map[string]any{"code": 0, "playlist": map[string]any{"code": 0, "data": data}}
}

func TestPlaylistCompletePagesAndEmpty(t *testing.T) {
	for _, total := range []int{0, 205} {
		t.Run(fmt.Sprint(total), func(t *testing.T) {
			p, calls := qqPlaylistServer(t, func(_, offset int) (int, any) { return 200, playlistEnvelope(playlistFixture(total, offset)) })
			s, err := p.FetchPlaylist(context.Background(), playlists.Ref{Provider: p.ID(), ID: "123456"})
			if err != nil {
				t.Fatal(err)
			}
			if len(s.Entries) != total || s.Completeness.RawSeen != total || *calls != max(1, (total+99)/100) {
				t.Fatalf("snapshot=%+v calls=%d", s, *calls)
			}
			if total > 0 && (*s.Entries[0].DurationMS != 123000 || len(s.Entries[0].Artists) != 2) {
				t.Fatal(s.Entries[0])
			}
		})
	}
}

func TestPlaylistFailureNeverReturnsSnapshot(t *testing.T) {
	for _, mode := range []string{"changed-total", "repeat-page", "early-empty", "missing-id", "missing-list", "missing-total", "wrong-list", "business", "rate-limit"} {
		t.Run(mode, func(t *testing.T) {
			p, calls := qqPlaylistServer(t, func(call, offset int) (int, any) {
				if mode == "rate-limit" {
					return 429, map[string]any{}
				}
				if mode == "business" {
					return 200, map[string]any{"code": 0, "playlist": map[string]any{"code": 24001}}
				}
				data := playlistFixture(205, offset)
				switch mode {
				case "changed-total":
					if call > 1 {
						data["total_song_num"] = 206
					}
				case "repeat-page":
					if call > 1 {
						data = playlistFixture(205, 0)
					}
				case "early-empty":
					if call > 1 {
						data["songlist"] = []any{}
					}
				case "missing-id":
					delete(data, "disstid")
				case "missing-list":
					delete(data, "songlist")
				case "missing-total":
					delete(data, "total_song_num")
				case "wrong-list":
					data["disstid"] = 456
				}
				return 200, playlistEnvelope(data)
			})
			s, err := p.FetchPlaylist(context.Background(), playlists.Ref{Provider: p.ID(), ID: "123456"})
			if err == nil || s != nil {
				t.Fatalf("accepted incomplete result %+v %v", s, err)
			}
			if *calls > 2 {
				t.Fatalf("continued after failure: %d", *calls)
			}
		})
	}
}

func TestPlaylistMissingFieldsAndRepeatedOccurrences(t *testing.T) {
	p, _ := qqPlaylistServer(t, func(_, offset int) (int, any) {
		data := playlistFixture(3, offset)
		data["songlist"] = []any{map[string]any{"id": 1}, map[string]any{"id": 2}, map[string]any{"id": 1}}
		return 200, playlistEnvelope(data)
	})
	s, err := p.FetchPlaylist(context.Background(), playlists.Ref{Provider: p.ID(), ID: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Entries[0].Title != nil || s.Entries[0].Artists != nil || s.Entries[0].DurationMS != nil || s.Entries[0].EntryKey == s.Entries[2].EntryKey {
		t.Fatal(s.Entries)
	}
}

func TestPlaylistGateAndCancel(t *testing.T) {
	p, calls := qqPlaylistServer(t, func(_, offset int) (int, any) { return 200, playlistEnvelope(playlistFixture(205, offset)) })
	ctx := provider.WithRequestGate(context.Background(), func() error {
		if *calls > 0 {
			return fmt.Errorf("disabled")
		}
		return nil
	})
	if s, err := p.FetchPlaylist(ctx, playlists.Ref{Provider: p.ID(), ID: "123456"}); err == nil || s != nil || *calls != 1 {
		t.Fatalf("%+v %v calls=%d", s, err, *calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.FetchPlaylist(ctx, playlists.Ref{Provider: p.ID(), ID: "123456"}); err == nil || *calls != 1 {
		t.Fatal("cancel ignored")
	}
}
