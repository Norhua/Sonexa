package qqmusic

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

// decodeBlocks 取出指定请求块的 data。
//
// HTTP 200 不代表成功：顶层 code 与每个块的 code 都必须为 0，
// 否则可能是风控或参数失效，不能把空 data 当作「无结果」。
func decodeBlocks(body []byte, names ...string) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 QQ 音乐响应失败: %w", err)
	}
	var code *int
	if err := json.Unmarshal(raw["code"], &code); err != nil || code == nil || *code != 0 {
		return nil, fmt.Errorf("QQ 音乐响应异常或来源不可用")
	}
	out := map[string]json.RawMessage{}
	for _, name := range names {
		var b block
		if err := json.Unmarshal(raw[name], &b); err != nil || b.Code == nil {
			return nil, fmt.Errorf("QQ 音乐响应缺少 %s", name)
		}
		if *b.Code != 0 {
			return nil, fmt.Errorf("QQ 音乐 %s 返回错误码 %d", name, *b.Code)
		}
		out[name] = b.Data
	}
	return out, nil
}

// ParseSearch 把搜索块解析为候选。导出以便离线重放测试。
func ParseSearch(body []byte) ([]provider.Candidate, error) {
	blocks, err := decodeBlocks(body, "req")
	if err != nil {
		return nil, err
	}
	var data searchData
	if err := json.Unmarshal(blocks["req"], &data); err != nil {
		return nil, fmt.Errorf("解析 QQ 音乐搜索响应失败: %w", err)
	}
	if data.Body == nil || data.Body.Song == nil {
		return nil, fmt.Errorf("QQ 音乐搜索响应缺少歌曲列表")
	}
	out := make([]provider.Candidate, 0, len(data.Body.Song.List))
	for _, s := range data.Body.Song.List {
		if !ValidMID(s.MID) {
			continue
		}
		out = append(out, candidateFrom(s))
	}
	return out, nil
}

func candidateFrom(s song) provider.Candidate {
	c := provider.Candidate{
		Provider:   provider.QQMusic,
		MatchRef:   s.MID,
		Title:      strPtr(songTitle(s)),
		ArtistName: strPtr(creditOf(s.Singer)),
		AlbumTitle: strPtr(albumName(s.Album)),
		DurationMS: durationOf(s.Interval),
		Year:       parseYear(firstNonEmpty(s.TimePublic, s.Album.TimePublic)),
	}
	c.Credits, c.ArtistName = creditsOf(s.Singer)
	// album ID=0 / MID 为空的搜索结果不是有效专辑，不能作为发行身份。
	if s.Album.ID != 0 && ValidMID(s.Album.MID) {
		ref := s.Album.MID
		c.ReleaseRef = &ref
		thumb := CoverURL(ref, 300)
		c.ThumbURL = &thumb
		c.HasCover = true
	}
	// index_album 在多碟专辑中是全专辑序号；只有第 1 碟时它才等于碟内序号。
	disc := s.IndexCD + 1
	c.DiscNumber = &disc
	if s.IndexCD == 0 && s.IndexAlbum > 0 {
		n := s.IndexAlbum
		c.TrackNumber = &n
	}
	return c
}

// detailParts 是一次详情拉取的各块原始数据。
type detailParts struct {
	album  albumData
	songs  []song
	total  int
	detail *songDetailData
}

// buildDetail 把专辑、清单与歌曲详情组合为 Detail。
func buildDetail(albumMID, trackRef string, parts detailParts) (*provider.Detail, error) {
	d := &provider.Detail{Provider: provider.QQMusic, FetchedAt: time.Now().UTC()}
	ref := albumMID
	d.ReleaseRef = &ref
	d.ArtworkURL = strPtr(CoverURL(albumMID, 500))

	if info := parts.album.BasicInfo; info != nil {
		if info.AlbumMID != "" && info.AlbumMID != albumMID {
			return nil, fmt.Errorf("QQ 音乐专辑响应未匹配目标专辑 %s", albumMID)
		}
		d.AlbumTitle = strPtr(info.AlbumName)
		d.ReleaseDate = strPtr(info.PublishDate)
		d.Year = parseYear(info.PublishDate)
		d.Genre = strPtr(info.Genre)
		d.PrimaryType = albumType(info.AlbumType)
	}
	if parts.album.Company != nil {
		// 可能是当前版权公司而非首版厂牌，作为 QQ 来源的值记录。
		d.Label = strPtr(parts.album.Company.Name)
		if d.Label != nil {
			d.Labels = []domain.Label{{Name: *d.Label}}
		}
	}
	if parts.album.Singer != nil {
		names := make([]string, 0, len(parts.album.Singer.SingerList))
		offset := 0
		for _, s := range parts.album.Singer.SingerList {
			if s.Name == "" {
				continue
			}
			names = append(names, s.Name)
			if len(names) > 1 {
				offset += len(creditSep)
			}
			member := provider.CreditArtist{Name: s.Name, Start: offset, End: offset + len(s.Name)}
			if s.MID != "" {
				member.Identity = &domain.Identity{Provider: provider.QQMusic, Ref: s.MID}
			}
			d.AlbumCredits = append(d.AlbumCredits, member)
			offset += len(s.Name)
		}
		d.AlbumCredit = strPtr(strings.Join(names, creditSep))
	}

	// 清单：数组次序不是曲序，按 (碟, 全专辑序号) 排序后派生碟内序号。
	songs := append([]song(nil), parts.songs...)
	sort.SliceStable(songs, func(i, j int) bool {
		if songs[i].IndexCD != songs[j].IndexCD {
			return songs[i].IndexCD < songs[j].IndexCD
		}
		return songs[i].IndexAlbum < songs[j].IndexAlbum
	})
	discs := map[int]int{}
	multiDisc := false
	for _, s := range songs {
		if s.IndexCD > 0 {
			multiDisc = true
		}
	}
	positions := map[string]provider.TracklistEntry{}
	for _, s := range songs {
		if !ValidMID(s.MID) {
			continue
		}
		discs[s.IndexCD]++
		mid := s.MID
		entry := provider.TracklistEntry{
			DiscNumber:  s.IndexCD + 1,
			TrackNumber: discs[s.IndexCD],
			TrackRef:    &mid,
			Title:       songTitle(s),
			TrackCredit: strPtr(creditOf(s.Singer)),
			DurationMS:  durationOf(s.Interval),
		}
		if multiDisc && s.IndexAlbum > 0 {
			// 保留上游全专辑序号，碟内序号是派生值。
			entry.TrackNumberText = strPtr(strconv.Itoa(s.IndexAlbum))
		}
		d.Tracklist = append(d.Tracklist, entry)
		positions[mid] = entry
	}
	if len(discs) > 0 {
		n := len(discs)
		d.DiscTotal = &n
	}
	total := parts.total
	if total > 0 {
		d.TrackTotal = &total
		d.ReleaseTrackTotal = &total
	}

	d.TracklistCompleteness = domain.Partial
	if total > 0 && len(d.Tracklist) == total {
		d.TracklistCompleteness = domain.Complete
		for disc := 0; disc < len(discs); disc++ {
			d.Media = append(d.Media, domain.Medium{Number: disc + 1, TrackTotal: domain.Known(discs[disc])})
		}
	}
	if trackRef == "" {
		return d, nil
	}
	entry, ok := positions[trackRef]
	if !ok {
		return nil, fmt.Errorf("专辑 %s 中未找到目标曲目 %s", albumMID, trackRef)
	}
	d.MatchRef = trackRef
	d.Title = strPtr(entry.Title)
	d.TrackCredit = entry.TrackCredit
	d.DurationMS = entry.DurationMS
	d.TrackNumberText = entry.TrackNumberText
	trackNo, discNo := entry.TrackNumber, entry.DiscNumber
	d.TrackNumber, d.DiscNumber = &trackNo, &discNo
	if d.TracklistCompleteness == domain.Complete {
		n := discs[discNo-1]
		d.MediumTrackTotal = &n
	}

	if parts.detail != nil {
		if ti := parts.detail.TrackInfo; ti != nil {
			if ti.MID != "" && ti.MID != trackRef {
				return nil, fmt.Errorf("QQ 音乐歌曲详情未匹配目标曲目 %s", trackRef)
			}
			if t := songTitle(*ti); t != "" {
				d.Title = &t
			}
			d.Credits, d.TrackCredit = creditsOf(ti.Singer)
		}
		applyInfo(d, parts.detail.Info)
	}
	return d, nil
}

// roleByTitle 把 info 条目的标题映射为参与角色；未列出的（合声编写、录音师、
// 混音工程师、简介等）没有对应角色，不写入。
var roleByTitle = map[string]string{
	"演唱者": "vocal",
	"合声":  "vocal",
	"作词":  "lyricist",
	"作曲":  "composer",
	"编曲":  "arranger",
	"制作人": "producer",
}

// instruments 是按乐器名给出的演奏条目，写为 instrument 角色并记录乐器。
var instruments = map[string]bool{
	"吉他": true, "电吉他": true, "木吉他": true, "贝斯": true, "鼓": true, "键盘": true,
	"钢琴": true, "弦乐": true, "小提琴": true, "中提琴": true, "大提琴": true, "萨克斯": true,
	"口琴": true, "长笛": true, "小号": true, "长号": true, "打击乐": true, "二胡": true,
	"琵琶": true, "古筝": true, "笛子": true, "手风琴": true, "尤克里里": true,
}

// applyInfo 从 info 数组中取角色、风格与歌词。不把 singer[] 推断为演唱者。
func applyInfo(d *provider.Detail, info []infoEntry) {
	position := len(d.Artists)
	for _, entry := range info {
		title := strings.TrimSpace(entry.Title)
		switch {
		case title == "歌曲流派":
			if len(entry.Content) > 0 {
				if g := strings.TrimSpace(entry.Content[0].Value); g != "" {
					d.Genre = &g
				}
			}
			continue
		case title == "歌词" || entry.Type == "lyric":
			if len(entry.Content) > 0 {
				d.Lyrics = strPtr(strings.TrimSpace(entry.Content[0].Value))
			}
			continue
		case title == "唱片公司":
			if d.Label == nil && len(entry.Content) > 0 {
				d.Label = strPtr(strings.TrimSpace(entry.Content[0].Value))
			}
			continue
		}
		role, attr := roleByTitle[title], ""
		if title == "合声" {
			attr = "合声"
		}
		if role == "" && instruments[title] {
			role, attr = "instrument", title
		}
		if role == "" {
			continue
		}
		for _, c := range entry.Content {
			name := strings.TrimSpace(c.Value)
			if name == "" {
				continue
			}
			a := provider.DetailArtist{Name: name, Role: role, Position: position}
			if c.MID != "" {
				a.Identity = &domain.Identity{Provider: provider.QQMusic, Ref: c.MID}
			}
			if attr != "" {
				v := attr
				a.Attribute = &v
				a.Attributes = []string{attr}
			}
			d.Artists = append(d.Artists, a)
			position++
		}
	}
}

// creditSep 是署名成员的连接串；Credits 区间给出边界，不会按它反拆乐队名。
const creditSep = " / "

func creditOf(singers []singer) string {
	names := make([]string, 0, len(singers))
	for _, s := range singers {
		if s.Name != "" {
			names = append(names, s.Name)
		}
	}
	return strings.Join(names, creditSep)
}

// creditsOf 构造署名原串及其结构化区间。
func creditsOf(singers []singer) ([]provider.CreditArtist, *string) {
	var credits []provider.CreditArtist
	var b strings.Builder
	for _, s := range singers {
		if s.Name == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(creditSep)
		}
		start := b.Len()
		b.WriteString(s.Name)
		member := provider.CreditArtist{Name: s.Name, Start: start, End: b.Len()}
		if s.MID != "" {
			member.Identity = &domain.Identity{Provider: provider.QQMusic, Ref: s.MID}
		}
		credits = append(credits, member)
	}
	return credits, strPtr(b.String())
}

// albumType 只映射明确的三类；其余（合辑、现场等）不猜。
func albumType(t string) *string {
	switch strings.TrimSpace(t) {
	case "录音室专辑":
		return strPtr("Album")
	case "EP":
		return strPtr("EP")
	case "单曲":
		return strPtr("Single")
	}
	return nil
}

// songTitle 优先用 title：它常带译名或 Live 等版本信息，不能丢。
func songTitle(s song) string {
	return firstNonEmpty(s.Title, s.Name)
}

func albumName(a albumRef) string {
	return firstNonEmpty(a.Title, a.Name)
}

// ParseLyric 解析独立歌词接口（format=json&nobase64=1）的响应。
func ParseLyric(body []byte) (*string, error) {
	var r lyricResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("解析 QQ 音乐歌词响应失败: %w", err)
	}
	for _, c := range []*int{r.Retcode, r.Code, r.Subcode} {
		if c != nil && *c != 0 {
			return nil, fmt.Errorf("QQ 音乐歌词接口返回错误码 %d", *c)
		}
	}
	return strPtr(strings.TrimSpace(r.Lyric)), nil
}

// ValidMID 校验 QQ 音乐的 MID：仅字母数字。
func ValidMID(mid string) bool {
	if len(mid) < 10 || len(mid) > 20 {
		return false
	}
	for _, c := range mid {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// CoverSizes 是 QQ 封面构造地址的档位；并非每张专辑都有 1200，采用时逐级回退。
var CoverSizes = []int{300, 500, 800, 1200}

// CoverURL 由专辑 MID 构造封面地址。
func CoverURL(albumMID string, size int) string {
	return fmt.Sprintf("https://y.qq.com/music/photo_new/T002R%dx%dM000%s.jpg", size, size, albumMID)
}

func durationOf(seconds int) *int {
	if seconds <= 0 {
		return nil
	}
	ms := seconds * 1000
	return &ms
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func parseYear(date string) *int {
	if len(date) < 4 {
		return nil
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil || y <= 0 {
		return nil
	}
	return &y
}
