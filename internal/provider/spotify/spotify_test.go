package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
)

const testPlaylist = "0000000000000000000001"

func TestOfficialPlaylistInputs(t *testing.T) {
	p := New(nil)
	for _, input := range []string{testPlaylist, "https://open.spotify.com/playlist/" + testPlaylist + "?si=share"} {
		ref, err := p.ParsePlaylist(input)
		if err != nil || ref.ID != testPlaylist || ref.Provider != ID {
			t.Fatalf("%+v %v", ref, err)
		}
	}
	for _, input := range []string{"x;python", "https://open.spotify.com/track/" + testPlaylist, "http://open.spotify.com/playlist/" + testPlaylist, "https://evil.test/playlist/" + testPlaylist, "https://open.spotify.com:443/playlist/" + testPlaylist} {
		if _, err := p.ParsePlaylist(input); err == nil {
			t.Fatal("invalid input accepted", input)
		}
	}
}

func pathfinderURL(operation string, variables map[string]any) string {
	hash := "a65e12194ed5fc443a1cdebed5fabe33ca5b07b987185d63c72483867ad13cb4"
	if operation == "getTrack" {
		hash = "612585ae06ba435ad26369870deaae23b5c8800a256cd8a57e08eddc25a37294"
	}
	v, _ := json.Marshal(variables)
	extensions, _ := json.Marshal(map[string]any{"persistedQuery": map[string]any{"version": 1, "sha256Hash": hash}})
	return "https://api-partner.spotify.com/pathfinder/v1/query?" + url.Values{"operationName": {operation}, "variables": {string(v)}, "extensions": {string(extensions)}}.Encode()
}

func TestTransportTargetAndSameTrackPolicy(t *testing.T) {
	b := &transportBridge{ref: testPlaylist, tracks: map[string]bool{testPlaylist: true}}
	page := pathfinderURL("fetchPlaylist", map[string]any{"uri": "spotify:playlist:" + testPlaylist, "offset": 0, "limit": 100, "enableWatchFeedEntrypoint": false})
	for _, address := range []string{"https://open.spotify.com/embed/playlist/" + testPlaylist, page, pathfinderURL("getTrack", map[string]any{"uri": "spotify:track:" + testPlaylist})} {
		if _, err := b.validate(httpMessage{Method: "GET", URL: address}); err != nil {
			t.Fatal(err)
		}
	}
	for _, message := range []httpMessage{
		{Method: "POST", URL: page},
		{Method: "GET", URL: "https://evil.test/pathfinder/v1/query"},
		{Method: "GET", URL: "https://api-partner.spotify.com.evil.test/pathfinder/v1/query"},
		{Method: "GET", URL: "https://open.spotify.com/embed/playlist/" + testPlaylist, Headers: map[string]string{"Authorization": "Bearer token"}},
		{Method: "GET", URL: page, Headers: map[string]string{"Cookie": "secret"}},
		{Method: "GET", URL: pathfinderURL("getTrack", map[string]any{"uri": "spotify:track:9999999999999999999999"})},
	} {
		if _, err := b.validate(message); err == nil {
			t.Fatal("unsafe request allowed", message)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBridgeRestrictionStopsAndNoCache(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		calls := 0
		client := provider.NewClient(nil, nil, "test")
		client.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
		})
		b := &transportBridge{client: client, ref: testPlaylist, tracks: map[string]bool{}}
		raw, _ := json.Marshal(httpMessage{Method: "GET", URL: "https://open.spotify.com/embed/playlist/" + testPlaylist})
		ctx := provider.WithSendBudget(context.Background(), provider.NewSendBudget(500))
		if _, err := b.exchange(ctx, raw); err == nil || calls != 1 {
			t.Fatalf("status=%d err=%v calls=%d", status, err, calls)
		}
	}
}

func TestFramesAndEnvironment(t *testing.T) {
	var stream bytes.Buffer
	if err := writeFrame(&stream, map[string]any{"name": "中文 空格"}); err != nil {
		t.Fatal(err)
	}
	if _, err := readFrame(&stream); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{{0, 0}, {255, 255, 255, 255}, {0, 0, 0, 1, 'x'}} {
		if _, err := readFrame(bytes.NewReader(raw)); err == nil {
			t.Fatal("bad frame accepted")
		}
	}
	t.Setenv("PYTHONPATH", "malicious")
	t.Setenv("PYTHONHOME", "malicious")
	t.Setenv("DYLD_INSERT_LIBRARIES", "malicious")
	for _, entry := range safeEnvironment() {
		if strings.HasPrefix(entry, "PYTHON") || strings.HasPrefix(entry, "DYLD_") {
			t.Fatal(entry)
		}
	}
	w := &boundedStderr{}
	_, _ = w.Write(make([]byte, 128<<10))
	if len(w.data) != 64<<10 {
		t.Fatal(len(w.data))
	}
}

func TestMissingRuntimeNeverUsesPATH(t *testing.T) {
	r := &Runner{root: t.TempDir()}
	t.Setenv("PATH", t.TempDir())
	if _, err := r.Run(context.Background(), testPlaylist, nil); playlists.Code(err) != playlists.RuntimeUnavailable {
		t.Fatal(err)
	}
}

// This remains opt-in because a clean checkout has no generated frozen binary.
// It uses the real helper but every HTTP response is a local fixture transport.
func TestFrozenHelperOfflineIntegration(t *testing.T) {
	if os.Getenv("SONEXA_TEST_FROZEN_HELPER") != "1" {
		t.Skip("prepare frozen helper to run integration")
	}
	root, err := filepath.Abs("../../../build/spotify-helper/dist/spotify-helper")
	if err != nil {
		t.Fatal(err)
	}
	client := provider.NewClient(nil, nil, "offline-test")
	calls := 0
	large := false
	client.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body string
		if r.URL.Host == "open.spotify.com" {
			data := map[string]any{"props": map[string]any{"pageProps": map[string]any{"state": map[string]any{
				"data":     map[string]any{"entity": map[string]any{"uri": "spotify:playlist:" + testPlaylist}},
				"settings": map[string]any{"session": map[string]any{"accessToken": "fixture-token", "accessTokenExpirationTimestampMs": 9999999999999, "isAnonymous": true}},
			}}}}
			encoded, _ := json.Marshal(data)
			body = `<script id="__NEXT_DATA__" type="application/json">` + string(encoded) + `</script>`
		} else if large {
			var variables struct {
				Offset int    `json:"offset"`
				URI    string `json:"uri"`
			}
			_ = json.Unmarshal([]byte(r.URL.Query().Get("variables")), &variables)
			var data any
			if r.URL.Query().Get("operationName") == "getTrack" {
				data = map[string]any{"trackUnion": map[string]any{"uri": variables.URI}}
			} else {
				items := []any{}
				for i := variables.Offset; i < min(205, variables.Offset+100); i++ {
					entry := map[string]any{"__typename": "Track", "uri": fmt.Sprintf("spotify:track:%022d", i+1), "name": "曲名", "artists": map[string]any{"items": []any{}}, "albumOfTrack": map[string]any{"uri": "spotify:album:" + testPlaylist, "name": "专辑"}, "trackDuration": map[string]any{"totalMilliseconds": 123456}}
					if i == 0 {
						delete(entry, "trackDuration")
					}
					items = append(items, map[string]any{"itemV2": map[string]any{"data": entry}})
				}
				data = map[string]any{"playlistV2": map[string]any{"uri": "spotify:playlist:" + testPlaylist, "name": "离线歌单", "content": map[string]any{"totalCount": 205, "items": items}}}
			}
			encoded, _ := json.Marshal(map[string]any{"data": data})
			body = string(encoded)
		} else {
			body = `{"data":{"playlistV2":{"uri":"spotify:playlist:` + testPlaylist + `","name":"离线歌单","content":{"totalCount":0,"items":[]}}}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	p := New(client)
	p.runner = &Runner{root: root}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := p.FetchPlaylist(ctx, playlists.Ref{Provider: ID, ID: testPlaylist})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 0 || s.Name != "离线歌单" || calls != 2 {
		t.Fatalf("snapshot=%+v calls=%d", s, calls)
	}
	large = true
	calls = 0
	s, err = p.FetchPlaylist(ctx, playlists.Ref{Provider: ID, ID: testPlaylist})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 205 || calls != 5 || s.Entries[0].DurationMS != nil || *s.Entries[1].DurationMS != 123456 {
		t.Fatalf("large fixture mismatch entries=%d calls=%d", len(s.Entries), calls)
	}
}
