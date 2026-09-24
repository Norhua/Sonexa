package qqmusic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseSearch(t *testing.T) {
	cands, err := ParseSearch(fixture(t, "search_qingtian.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hit *provider.Candidate
	for i := range cands {
		if cands[i].MatchRef == "0039MnYb0qxYhV" {
			hit = &cands[i]
		}
	}
	if hit == nil {
		t.Fatalf("未找到《晴天》原版候选: %d 条", len(cands))
	}
	if hit.Provider != provider.QQMusic || *hit.ArtistName != "周杰伦" || hit.ReleaseRef == nil || *hit.ReleaseRef != "000MkMni19ClKG" {
		t.Fatalf("候选字段错误: %+v", hit)
	}
	if hit.DurationMS == nil || *hit.DurationMS <= 0 || hit.ThumbURL == nil || !strings.Contains(*hit.ThumbURL, "T002R300x300M000000MkMni19ClKG") {
		t.Fatalf("时长或缩略图错误: %+v", hit)
	}
	if hit.Year == nil || *hit.Year != 2003 {
		t.Fatalf("年份错误: %v", hit.Year)
	}
}

func TestParseSearchRejectsErrorCode(t *testing.T) {
	if _, err := ParseSearch([]byte(`{"code":0,"req":{"code":2001,"data":{}}}`)); err == nil {
		t.Fatal("请求块错误码应报错")
	}
	if _, err := ParseSearch([]byte(`{"code":500001}`)); err == nil {
		t.Fatal("顶层错误码应报错")
	}
}

func TestCandidateSkipsInvalidAlbum(t *testing.T) {
	c := candidateFrom(song{MID: "003RcjkV1XjQQy", Name: "x", Album: albumRef{ID: 0, MID: ""}, IndexCD: 1, IndexAlbum: 14})
	if c.ReleaseRef != nil || c.HasCover {
		t.Fatal("无效专辑不能作为发行身份")
	}
	if c.TrackNumber != nil || *c.DiscNumber != 2 {
		t.Fatal("第 2 碟的全专辑序号不能当作碟内序号")
	}
}

func detailFromFixture(t *testing.T, name, album, track string) *provider.Detail {
	t.Helper()
	blocks, err := decodeBlocks(fixture(t, name), "album", "songs", "song")
	if err != nil {
		t.Fatal(err)
	}
	var parts detailParts
	json.Unmarshal(blocks["album"], &parts.album)
	page, err := parseSongPage(blocks["songs"])
	if err != nil {
		t.Fatal(err)
	}
	parts.total = page.TotalNum
	for _, s := range page.SongList {
		parts.songs = append(parts.songs, s.SongInfo)
	}
	var detail songDetailData
	json.Unmarshal(blocks["song"], &detail)
	parts.detail = &detail
	d, err := buildDetail(album, track, parts)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func roles(d *provider.Detail) map[string][]string {
	out := map[string][]string{}
	for _, a := range d.Artists {
		key := a.Role
		if a.Attribute != nil {
			key += ":" + *a.Attribute
		}
		out[key] = append(out[key], a.Name)
	}
	return out
}

func TestDetailWorldIsMine(t *testing.T) {
	d := detailFromFixture(t, "detail_world.json", "000BDJvT2aWQYZ", "003RcjkV1XjQQy")
	if d.MatchRef != "003RcjkV1XjQQy" || *d.ReleaseRef != "000BDJvT2aWQYZ" {
		t.Fatalf("身份错误: %s %v", d.MatchRef, d.ReleaseRef)
	}
	if *d.Title != "ワールドイズマイン (World is Mine)" || *d.AlbumTitle != "supercell" {
		t.Fatalf("标题错误: %s / %s", *d.Title, *d.AlbumTitle)
	}
	if *d.TrackCredit != "supercell / 初音未来" || len(d.Credits) != 2 || (*d.TrackCredit)[d.Credits[1].Start:d.Credits[1].End] != "初音未来" {
		t.Fatalf("署名错误: %q %+v", *d.TrackCredit, d.Credits)
	}
	if *d.TrackNumber != 7 || *d.DiscNumber != 1 || *d.TrackTotal != 12 || *d.DiscTotal != 1 || len(d.Tracklist) != 12 {
		t.Fatalf("序号错误: %d %d %d", *d.TrackNumber, *d.DiscNumber, *d.TrackTotal)
	}
	if *d.Year != 2009 || *d.Genre != "Pop" || *d.PrimaryType != "Album" || d.Label != nil || d.CatalogNumber != nil {
		t.Fatalf("发行字段错误: %+v", d)
	}
	r := roles(d)
	if strings.Join(r["vocal"], ",") != "supercell,初音未来" || r["lyricist"][0] != "ryo" || r["composer"][0] != "ryo" ||
		r["arranger"][0] != "ryo" || r["album_artist"][0] != "supercell" {
		t.Fatalf("角色错误: %v", r)
	}
	if d.Lyrics == nil || !strings.HasPrefix(*d.Lyrics, "[ti:ワールドイズマイン") {
		t.Fatal("缺少歌词")
	}
}

func TestDetailQingtianRoles(t *testing.T) {
	d := detailFromFixture(t, "detail_qingtian.json", "000MkMni19ClKG", "0039MnYb0qxYhV")
	r := roles(d)
	if r["vocal:合声"][0] != "周杰伦" || r["producer"][0] != "周杰伦" || r["instrument:吉他"][0] != "蔡科俊Again" ||
		r["instrument:贝斯"][0] != "陈任佑" || r["instrument:鼓"][0] != "陈柏州" {
		t.Fatalf("角色错误: %v", r)
	}
	for key := range r {
		if strings.Contains(key, "合声编写") || strings.Contains(key, "录音") {
			t.Fatalf("无对应角色的条目不应写入: %s", key)
		}
	}
	if d.Label == nil || *d.Label != "杰威尔音乐有限公司" {
		t.Fatalf("厂牌错误: %v", d.Label)
	}
}

func TestDetailMultiDiscOrdering(t *testing.T) {
	blocks, err := decodeBlocks(fixture(t, "album_multidisc.json"), "album", "songs")
	if err != nil {
		t.Fatal(err)
	}
	var parts detailParts
	page, _ := parseSongPage(blocks["songs"])
	parts.total = page.TotalNum
	var second string
	for _, s := range page.SongList {
		parts.songs = append(parts.songs, s.SongInfo)
		if s.SongInfo.IndexCD == 1 && s.SongInfo.IndexAlbum == 14 {
			second = s.SongInfo.MID
		}
	}
	d, err := buildDetail("0032ezFm3F53yO", second, parts)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tracklist) != 25 || *d.DiscTotal != 2 {
		t.Fatalf("清单不完整: %d", len(d.Tracklist))
	}
	for i, e := range d.Tracklist[:13] {
		if e.DiscNumber != 1 || e.TrackNumber != i+1 {
			t.Fatalf("第 1 碟排序错误: %d %+v", i, e)
		}
	}
	if *d.DiscNumber != 2 || *d.TrackNumber != 1 || *d.TrackNumberText != "14" {
		t.Fatalf("第 2 碟应派生碟内序号: %d-%d %s", *d.DiscNumber, *d.TrackNumber, *d.TrackNumberText)
	}
}

func TestDetailMissingTrack(t *testing.T) {
	if _, err := buildDetail("000BDJvT2aWQYZ", "0000000000XXXX", detailParts{}); err == nil {
		t.Fatal("目标曲目不在清单中时必须报错")
	}
}

func TestFetchDetailOverHTTP(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "lyric") {
			w.Write([]byte(`{"retcode":0,"code":0,"subcode":0,"lyric":"[00:00.00]x"}`))
			return
		}
		posts++
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.Header.Get("Referer") != "https://y.qq.com/" || !strings.Contains(string(body), `"song_mid":"003RcjkV1XjQQy"`) {
			t.Errorf("请求错误: %s %s", r.Method, body)
		}
		raw := fixture(t, "detail_world.json")
		// 去掉歌词条目，验证回退到独立歌词接口
		raw = []byte(strings.Replace(string(raw), `"title":"歌词"`, `"title":"无关"`, 1))
		raw = []byte(strings.Replace(string(raw), `"type":"lyric"`, `"type":"x"`, 1))
		w.Write(raw)
	}))
	defer srv.Close()
	oldM, oldL := MusicuURL, LyricURL
	MusicuURL, LyricURL = srv.URL+"/musicu", srv.URL+"/lyric"
	defer func() { MusicuURL, LyricURL = oldM, oldL }()

	client := provider.NewClient(nil, nil, "test")
	p := New(client, Config{}, nil)
	d, err := p.FetchDetail(context.Background(), provider.Ref{Provider: provider.QQMusic, ID: "000BDJvT2aWQYZ", TrackRef: "003RcjkV1XjQQy"})
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 || d.Lyrics == nil || *d.Lyrics != "[00:00.00]x" {
		t.Fatalf("posts=%d lyrics=%v", posts, d.Lyrics)
	}
}
