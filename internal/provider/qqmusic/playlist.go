package qqmusic

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
)

var playlistPath = regexp.MustCompile(`^/(?:n/ryqq/playlist|n/m/detail/taoge|n/ryqq/taoge)/([0-9]+)/?$`)
var playlistDigits = regexp.MustCompile(`^[0-9]{1,19}$`)

func playlistID(value string) (string, error) {
	if !playlistDigits.MatchString(value) {
		return "", playlists.Failure(playlists.Input, "QQ 歌单 ID 无效")
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return "", playlists.Failure(playlists.Input, "QQ 歌单 ID 无效")
	}
	return strconv.FormatInt(n, 10), nil
}

func (p *Provider) PlaylistAvailability() playlists.Availability {
	if p.client == nil {
		return playlists.Availability{Reason: "QQ 请求服务未就绪"}
	}
	return playlists.Availability{Available: true}
}

func (p *Provider) ParsePlaylist(input string) (playlists.Ref, error) {
	value := strings.TrimSpace(input)
	id, err := playlistID(value)
	if err != nil {
		u, e := url.Parse(value)
		if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || (u.Host != "y.qq.com" && u.Host != "i2.y.qq.com") || u.Fragment != "" {
			return playlists.Ref{}, playlists.Failure(playlists.Input, "请输入 QQ 官方歌单链接或数字 ID")
		}
		if u.Host == "i2.y.qq.com" && u.Path != "/n3/other/pages/details/playlist.html" {
			return playlists.Ref{}, playlists.Failure(playlists.Input, "链接不是 QQ 歌单")
		}
		if path := playlistPath.FindStringSubmatch(u.Path); path != nil {
			id, err = playlistID(path[1])
		} else if u.Path == "/n/ryqq/playlist" || u.Path == "/n/m/detail/taoge/index.html" || u.Path == "/n/yqq/playlist.html" || u.Path == "/n2/m/share/details/taoge.html" || u.Path == "/n3/other/pages/details/playlist.html" {
			query, e := url.ParseQuery(u.RawQuery)
			if e != nil {
				return playlists.Ref{}, playlists.Failure(playlists.Input, "歌单分享参数无效")
			}
			id = ""
			for _, key := range []string{"id", "disstid", "dissid"} {
				if values, ok := query[key]; ok {
					if len(values) != 1 {
						return playlists.Ref{}, playlists.Failure(playlists.Input, "歌单身份参数重复")
					}
					candidate, e := playlistID(values[0])
					if e != nil {
						return playlists.Ref{}, e
					}
					if id != "" && id != candidate {
						return playlists.Ref{}, playlists.Failure(playlists.Input, "歌单身份参数冲突")
					}
					id = candidate
				}
			}
			_, err = playlistID(id)
		} else {
			err = playlists.Failure(playlists.Input, "链接不是 QQ 歌单")
		}
	}
	if err != nil {
		return playlists.Ref{}, err
	}
	return playlists.Ref{Provider: p.ID(), ID: id}, nil
}

func (p *Provider) FetchPlaylist(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
	id, err := playlistID(ref.ID)
	if err != nil || id != ref.ID || ref.Provider != p.ID() {
		return nil, playlists.Failure(playlists.Input, "QQ 歌单身份无效")
	}
	if p.client == nil {
		return nil, playlists.Failure(playlists.RuntimeUnavailable, "QQ 请求服务未就绪")
	}
	ctx, cancel := context.WithTimeout(ctx, playlists.FetchTimeout)
	defer cancel()
	ctx = provider.WithSendBudget(ctx, provider.NewSendBudget(playlists.MaxSends))
	ctx = provider.WithRequestLimits(ctx, provider.RequestLimits{MaxAttempts: 3, ResponseBytes: playlists.MaxResponseBytes, TotalResponseBytes: playlists.MaxResponseTotal})
	result, err := p.fetchPlaylist(ctx, ref)
	if err != nil {
		return nil, playlistError(err)
	}
	if err = result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *Provider) fetchPlaylist(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
	s := &playlists.Snapshot{Ref: ref, FetchedAt: time.Now().UTC(), Entries: []playlists.Entry{}, Excluded: []playlists.Excluded{},
		Diagnostics: []string{"条目按歌曲身份和出现序号配对，不能识别相同副本的具体删除"}}
	seenPages := map[[32]byte]bool{}
	occurrences := map[string]int{}
	total, rawSeen := -1, 0
	id, _ := strconv.ParseInt(ref.ID, 10, 64)
	for page := 1; page <= playlists.MaxPages; page++ {
		body, err := p.musicu(ctx, provider.NoStore, map[string]any{"playlist": map[string]any{
			"module": "music.srfDissInfo.DissInfo", "method": "CgiGetDiss",
			"param": map[string]any{"disstid": id, "song_begin": rawSeen, "song_num": 100},
		}})
		if err != nil {
			return nil, err
		}
		blocks, err := decodeBlocks(body, "playlist")
		if err != nil {
			return nil, err
		}
		var data struct {
			DirInfo struct {
				Title *string     `json:"title"`
				ID    json.Number `json:"id"`
			} `json:"dirinfo"`
			DissID      json.Number       `json:"disstid"`
			Total       *int              `json:"total_song_num"`
			Songs       []json.RawMessage `json:"songlist"`
			Size        *int              `json:"songlist_size"`
			HasMore     *int              `json:"hasmore"`
			Code        int               `json:"code"`
			Subcode     int               `json:"subcode"`
			Invalid     []json.RawMessage `json:"invalid_song"`
			FilterCount int               `json:"filter_song_num"`
			Filtered    []struct {
				ID   json.Number `json:"songid"`
				Type int         `json:"songtype"`
			} `json:"filtered_song"`
		}
		if json.Unmarshal(blocks["playlist"], &data) != nil || data.Total == nil || *data.Total < 0 || data.Songs == nil || data.DirInfo.Title == nil {
			return nil, playlists.Failure(playlists.Incomplete, "QQ 歌单分页缺少完整性字段")
		}
		if data.DissID == "" && data.DirInfo.ID == "" {
			return nil, playlists.Failure(playlists.Incomplete, "QQ 歌单分页缺少身份")
		}
		for _, echo := range []json.Number{data.DissID, data.DirInfo.ID} {
			if echo != "" && echo.String() != ref.ID {
				return nil, playlists.Failure(playlists.Incomplete, "QQ 返回了不同歌单")
			}
		}
		if data.Code != 0 || data.Subcode != 0 {
			return nil, playlists.Failure(playlists.Restricted, "QQ 歌单来源限制读取")
		}
		if len(data.Invalid) != 0 || data.FilterCount != 0 || data.Size != nil && *data.Size != len(data.Songs) {
			return nil, playlists.Failure(playlists.Incomplete, "QQ 歌单存在无法核对的歌曲")
		}
		// CgiGetDiss subtracts this page's filtered songs from its reported
		// total, not all filtered songs in the playlist. Restore the raw count
		// before comparing pages, and advance by raw slots rather than songs.
		if len(data.Filtered) > 100 || *data.Total > playlists.MaxItems-len(data.Filtered) {
			return nil, playlists.Failure(playlists.Resource, "QQ 歌单超过条目上限")
		}
		pageTotal := *data.Total + len(data.Filtered)
		if total < 0 {
			total = pageTotal
			s.Name = *data.DirInfo.Title
		} else if total != pageTotal || s.Name != *data.DirInfo.Title {
			return nil, playlists.Failure(playlists.Incomplete, "读取期间 QQ 歌单发生变化")
		}
		pageSize := len(data.Songs) + len(data.Filtered)
		if pageSize != min(100, total-rawSeen) || data.HasMore != nil && *data.HasMore != boolInt(rawSeen+pageSize < total) {
			return nil, playlists.Failure(playlists.Incomplete, "QQ 歌单分页覆盖不完整")
		}
		encoded, _ := json.Marshal([]any{data.Songs, data.Filtered})
		hash := sha256.Sum256(encoded)
		if pageSize > 0 && seenPages[hash] {
			return nil, playlists.Failure(playlists.Incomplete, "QQ 返回重复歌单页面")
		}
		seenPages[hash] = true
		for _, raw := range data.Songs {
			e, err := parsePlaylistSong(raw)
			if err != nil {
				return nil, err
			}
			occurrences[e.TrackRef]++
			e.EntryKey = playlists.OccurrenceKey(e.TrackRef, occurrences[e.TrackRef])
			e.Position = len(s.Entries) + 1
			s.Entries = append(s.Entries, e)
		}
		for _, filtered := range data.Filtered {
			id, err := playlistID(filtered.ID.String())
			if err != nil || filtered.Type != 13 {
				return nil, playlists.Failure(playlists.Incomplete, "QQ 歌单过滤项身份或类型无法确认")
			}
			s.Excluded = append(s.Excluded, playlists.Excluded{TrackRef: id, Kind: "unavailable_track", Reason: "QQ 未返回该歌曲详情；已有同步条目保持不变"})
		}
		rawSeen += pageSize
		if rawSeen == total {
			s.Completeness = playlists.Completeness{Complete: true, DeclaredTotal: total, RawSeen: rawSeen, Pages: page, TrackCount: len(s.Entries), ExcludedCount: len(s.Excluded)}
			return s, nil
		}
	}
	return nil, playlists.Failure(playlists.Resource, "QQ 歌单超过分页上限")
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func parsePlaylistSong(raw json.RawMessage) (playlists.Entry, error) {
	var song struct {
		ID       json.Number `json:"id"`
		SongID   json.Number `json:"songid"`
		Name     *string     `json:"name"`
		SongName *string     `json:"songname"`
		Album    struct {
			Name *string `json:"name"`
		} `json:"album"`
		AlbumName *string `json:"albumname"`
		Singer    []struct {
			Name *string `json:"name"`
		} `json:"singer"`
		Interval *int64 `json:"interval"`
	}
	if json.Unmarshal(raw, &song) != nil {
		return playlists.Entry{}, playlists.Failure(playlists.Incomplete, "QQ 歌单歌曲结构无效")
	}
	// Numeric song IDs exist in both readable and filtered records. MID is
	// still used by metadata providers; it cannot match a filtered song here.
	ref := song.ID
	if ref == "" {
		ref = song.SongID
	}
	id, err := playlistID(ref.String())
	if err != nil || song.ID != "" && song.SongID != "" && song.ID != song.SongID {
		return playlists.Entry{}, playlists.Failure(playlists.Incomplete, "QQ 歌单歌曲身份缺失")
	}
	e := playlists.Entry{TrackRef: id, Title: song.Name, Album: song.Album.Name}
	if e.Title == nil {
		e.Title = song.SongName
	}
	if e.Album == nil {
		e.Album = song.AlbumName
	}
	if song.Singer != nil {
		e.Artists = []string{}
		for _, artist := range song.Singer {
			if artist.Name == nil {
				e.Artists = nil
				break
			}
			e.Artists = append(e.Artists, *artist.Name)
		}
	}
	if song.Interval != nil && *song.Interval >= 0 && *song.Interval <= int64(^uint64(0)>>1)/1000 {
		ms := *song.Interval * 1000
		e.DurationMS = &ms
	}
	return e, nil
}

func playlistError(err error) error {
	var fetch *playlists.FetchError
	if errors.As(err, &fetch) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return playlists.Failure(playlists.Cancelled, "歌单读取已取消或超时")
	}
	if errors.Is(err, provider.ErrResponseTooLarge) || errors.Is(err, provider.ErrSendBudget) {
		return playlists.Failure(playlists.Resource, "歌单读取超过资源上限")
	}
	var business *provider.BusinessError
	if errors.As(err, &business) {
		return playlists.Failure(playlists.Restricted, fmt.Sprintf("QQ 歌单来源不可用（错误码 %d）", business.Code))
	}
	var httpErr *provider.HTTPError
	if errors.As(err, &httpErr) {
		code := playlists.Temporary
		if httpErr.StatusCode == 401 || httpErr.StatusCode == 403 || httpErr.StatusCode == 404 {
			code = playlists.Access
		}
		if httpErr.StatusCode == 429 {
			code = playlists.Restricted
		}
		return &playlists.FetchError{Code: code, Message: "QQ 歌单请求失败", RetryAfter: httpErr.RetryAfter}
	}
	return playlists.Failure(playlists.Incomplete, "无法取得完整 QQ 歌单")
}

var _ playlists.Provider = (*Provider)(nil)
