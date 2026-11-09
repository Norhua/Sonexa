package spotify

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
)

const ID provider.ProviderID = "spotify"

var playlistID = regexp.MustCompile(`^[A-Za-z0-9]{22}$`)
var playlistURL = regexp.MustCompile(`^/playlist/([A-Za-z0-9]{22})/?$`)

// Provider has only the playlist capability. The runner path cannot come from
// playlist input, environment Python settings, or a frontend DTO.
type Provider struct {
	client *provider.Client
	runner *Runner
}

func New(client *provider.Client) *Provider { return &Provider{client: client, runner: &Runner{}} }
func (p *Provider) ID() provider.ProviderID { return ID }
func (p *Provider) DisplayName() string     { return "Spotify" }
func (p *Provider) PlaylistAvailability() playlists.Availability {
	if p.runner == nil || p.client == nil {
		return playlists.Availability{Reason: "Spotify 运行环境未就绪"}
	}
	if _, err := p.runner.validate(); err != nil {
		return playlists.Availability{Reason: err.Error()}
	}
	return playlists.Availability{Available: true}
}
func (p *Provider) ParsePlaylist(input string) (playlists.Ref, error) {
	value := strings.TrimSpace(input)
	if !playlistID.MatchString(value) {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host != "open.spotify.com" || u.User != nil || u.Fragment != "" {
			return playlists.Ref{}, playlists.Failure(playlists.Input, "请输入 Spotify 官方歌单链接或 ID")
		}
		path := playlistURL.FindStringSubmatch(u.Path)
		if path == nil {
			return playlists.Ref{}, playlists.Failure(playlists.Input, "链接不是 Spotify 歌单")
		}
		value = path[1]
	}
	return playlists.Ref{Provider: ID, ID: value}, nil
}
func (p *Provider) FetchPlaylist(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
	if ref.Provider != ID || !playlistID.MatchString(ref.ID) {
		return nil, playlists.Failure(playlists.Input, "Spotify 歌单身份无效")
	}
	if p.runner == nil || p.client == nil {
		return nil, playlists.Failure(playlists.RuntimeUnavailable, "Spotify 运行环境未就绪")
	}
	ctx, cancel := context.WithTimeout(ctx, playlists.FetchTimeout)
	defer cancel()
	ctx = provider.WithSendBudget(ctx, provider.NewSendBudget(playlists.MaxSends))
	ctx = provider.WithRequestLimits(ctx, provider.RequestLimits{MaxAttempts: 3, ResponseBytes: playlists.MaxResponseBytes, TotalResponseBytes: playlists.MaxResponseTotal})
	bridge := &transportBridge{client: p.client, ref: ref.ID, tracks: map[string]bool{}}
	raw, err := p.runner.Run(ctx, ref.ID, bridge.exchange)
	if err != nil {
		return nil, err
	}
	var result struct {
		PlaylistRef  string                 `json:"playlistRef"`
		Name         string                 `json:"name"`
		FetchedAt    time.Time              `json:"fetchedAt"`
		Entries      []playlists.Entry      `json:"entries"`
		Excluded     []playlists.Excluded   `json:"excluded"`
		Completeness playlists.Completeness `json:"completeness"`
	}
	if json.Unmarshal(raw, &result) != nil || result.PlaylistRef != ref.ID {
		return nil, playlists.Failure(playlists.Incomplete, "Spotify 歌单响应身份无效")
	}
	s := &playlists.Snapshot{Ref: ref, Name: result.Name, FetchedAt: result.FetchedAt, Entries: result.Entries, Excluded: result.Excluded, Completeness: result.Completeness,
		Diagnostics: []string{"条目按歌曲身份和出现序号配对，不能识别相同副本的具体删除"}}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

var _ playlists.Provider = (*Provider)(nil)
