package spotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
)

type transportBridge struct {
	client *provider.Client
	ref    string
	tracks map[string]bool
	pages  int
}

type httpMessage struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// validate permits only the current playlist and details whose IDs were seen
// in successful pages. Anonymous tokens can only go to the pathfinder host.
func (b *transportBridge) validate(message httpMessage) (*url.URL, error) {
	bad := func() (*url.URL, error) {
		return nil, playlists.Failure(playlists.Access, "Spotify 请求超出允许范围")
	}
	u, err := url.Parse(message.URL)
	if err != nil || message.Method != "GET" || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return bad()
	}
	for key, value := range message.Headers {
		if strings.ContainsAny(value, "\r\n") {
			return bad()
		}
		switch strings.ToLower(key) {
		case "authorization":
			if u.Host != "api-partner.spotify.com" || !strings.HasPrefix(value, "Bearer ") || len(value) > 16<<10 {
				return bad()
			}
		case "app-platform":
			if value != "WebPlayer" {
				return bad()
			}
		default:
			return bad()
		}
	}
	if u.Host == "open.spotify.com" {
		if u.Path != "/embed/playlist/"+b.ref || u.RawQuery != "" || len(message.Headers) != 0 {
			return bad()
		}
		return u, nil
	}
	if u.Host != "api-partner.spotify.com" || u.Path != "/pathfinder/v1/query" {
		return bad()
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 3 {
		return bad()
	}
	for _, key := range []string{"operationName", "variables", "extensions"} {
		if len(q[key]) != 1 {
			return bad()
		}
	}
	var variables map[string]json.RawMessage
	if json.Unmarshal([]byte(q.Get("variables")), &variables) != nil {
		return bad()
	}
	var uri string
	if json.Unmarshal(variables["uri"], &uri) != nil {
		return bad()
	}
	var expectedHash string
	switch q.Get("operationName") {
	case "fetchPlaylist":
		if uri != "spotify:playlist:"+b.ref || len(variables) != 4 {
			return bad()
		}
		var offset, limit int
		var watch bool
		if json.Unmarshal(variables["offset"], &offset) != nil || json.Unmarshal(variables["limit"], &limit) != nil || json.Unmarshal(variables["enableWatchFeedEntrypoint"], &watch) != nil || watch || offset < 0 || offset > playlists.MaxItems || limit != 100 {
			return bad()
		}
		b.pages++
		if b.pages > playlists.MaxPages {
			return nil, playlists.Failure(playlists.Resource, "Spotify 分页超过上限")
		}
		expectedHash = "a65e12194ed5fc443a1cdebed5fabe33ca5b07b987185d63c72483867ad13cb4"
	case "getTrack":
		if len(variables) != 1 || !strings.HasPrefix(uri, "spotify:track:") || !b.tracks[strings.TrimPrefix(uri, "spotify:track:")] {
			return bad()
		}
		expectedHash = "612585ae06ba435ad26369870deaae23b5c8800a256cd8a57e08eddc25a37294"
	default:
		return bad()
	}
	var extensions struct {
		PersistedQuery struct {
			Version int    `json:"version"`
			SHA     string `json:"sha256Hash"`
		} `json:"persistedQuery"`
	}
	if json.Unmarshal([]byte(q.Get("extensions")), &extensions) != nil || extensions.PersistedQuery.Version != 1 || extensions.PersistedQuery.SHA != expectedHash {
		return bad()
	}
	return u, nil
}

func (b *transportBridge) exchange(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var message httpMessage
	if json.Unmarshal(raw, &message) != nil {
		return nil, playlists.Failure(playlists.Access, "Spotify 请求结构无效")
	}
	u, err := b.validate(message)
	if err != nil {
		return nil, err
	}
	params := map[string]string{}
	for key, values := range u.Query() {
		params[key] = values[0]
	}
	info := &provider.ResponseInfo{}
	// Redirects may not change host, path or the requested operation/identity.
	// Every actual hop still crosses the generic send budget and current gate.
	client := *b.client
	httpClient := *b.client.HTTP
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if transport, ok := base.(*http.Transport); ok {
		transport = transport.Clone()
		transport.Proxy = nil // never inherit arbitrary user proxy environment
		httpClient.Transport = transport
		defer transport.CloseIdleConnections()
	}
	client.HTTP = &httpClient
	body, err := client.Get(ctx, provider.GetRequest{Provider: provider.SpotifyQuota, BaseURL: u.Scheme + "://" + u.Host, Endpoint: u.Path, Params: params,
		Headers: message.Headers, CacheMode: provider.NoStore, MaxResponseBytes: playlists.MaxResponseBytes,
		Redirects: &provider.RedirectPolicy{Hosts: []string{u.Host}, MaxRedirects: 4, Validate: func(next *url.URL) error {
			if next.Scheme != u.Scheme || next.Host != u.Host || next.Path != u.Path || next.Query().Encode() != u.Query().Encode() || next.Fragment != "" {
				return playlists.Failure(playlists.Access, "Spotify 跳转超出允许范围")
			}
			return nil
		}}, ResponseInfo: info})
	if err != nil {
		return nil, mapError(err)
	}
	if u.Host == "api-partner.spotify.com" {
		var page struct {
			Errors []json.RawMessage `json:"errors"`
			Data   struct {
				Playlist struct {
					Content struct {
						Items []struct {
							Item struct {
								Data struct {
									Type string `json:"__typename"`
									URI  string `json:"uri"`
								} `json:"data"`
							} `json:"itemV2"`
						} `json:"items"`
					} `json:"content"`
				} `json:"playlistV2"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &page) != nil || len(page.Errors) > 0 {
			return nil, playlists.Failure(playlists.Incomplete, "Spotify 查询未完整完成")
		}
		for _, item := range page.Data.Playlist.Content.Items {
			uri := item.Item.Data.URI
			if item.Item.Data.Type == "Track" && strings.HasPrefix(uri, "spotify:track:") {
				id := strings.TrimPrefix(uri, "spotify:track:")
				if playlistID.MatchString(id) {
					b.tracks[id] = true
				}
			}
		}
	}
	return map[string]any{"status": info.StatusCode, "headers": map[string]string{"content-type": info.ContentType}, "body": base64.StdEncoding.EncodeToString(body)}, nil
}

func mapError(err error) error {
	var fetch *playlists.FetchError
	if errors.As(err, &fetch) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return playlists.Failure(playlists.Cancelled, "Spotify 读取已取消或超时")
	}
	if errors.Is(err, provider.ErrResponseTooLarge) || errors.Is(err, provider.ErrSendBudget) {
		return playlists.Failure(playlists.Resource, "Spotify 读取超过资源上限")
	}
	var httpError *provider.HTTPError
	if errors.As(err, &httpError) {
		code := playlists.Temporary
		if httpError.StatusCode == 401 || httpError.StatusCode == 403 || httpError.StatusCode == 404 {
			code = playlists.Access
		}
		if httpError.StatusCode == 429 {
			code = playlists.Restricted
		}
		return &playlists.FetchError{Code: code, Message: "Spotify 请求失败", RetryAfter: httpError.RetryAfter}
	}
	return playlists.Failure(playlists.Temporary, "Spotify 请求未完成")
}
