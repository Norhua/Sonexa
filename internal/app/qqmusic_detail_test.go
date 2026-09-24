package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

// QQ 音乐详情经唯一的落库入口写入：署名按结构化区间保留，演唱等角色只取
// 上游明确列出的条目（虚拟歌姬出现在「演唱者」中），歌词与风格一并写入。
func TestQQMusicDetailAppliedToTrack(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	body, err := os.ReadFile("../provider/qqmusic/testdata/detail_world.json")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := qqmusic.ParseDetailJSON(body, "000BDJvT2aWQYZ", "003RcjkV1XjQQy")
	if err != nil {
		t.Fatal(err)
	}
	title, album, ref := "ワールドイズマイン", "supercell", "000BDJvT2aWQYZ"
	id, err := app.store.AdmitCandidate(provider.Candidate{Provider: provider.QQMusic, MatchRef: "003RcjkV1XjQQy", Title: &title, AlbumTitle: &album, ReleaseRef: &ref})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyDetailToTrack(context.Background(), id, "qq", detail); err != nil {
		t.Fatal(err)
	}
	d, err := app.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.CreditText != "supercell / 初音未来" || len(d.CreditSpans) != 2 {
		t.Fatalf("credit = %q %+v", d.CreditText, d.CreditSpans)
	}
	groups := map[string][]string{}
	for _, g := range d.Participants {
		for _, a := range g.Artists {
			groups[g.Role] = append(groups[g.Role], a.Name)
		}
	}
	if strings.Join(groups["vocal"], ",") != "supercell,初音未来" || strings.Join(groups["composer"], ",") != "ryo" ||
		strings.Join(groups["lyricist"], ",") != "ryo" || strings.Join(groups["arranger"], ",") != "ryo" {
		t.Fatalf("participants = %v", groups)
	}
	m := d.Metadata
	if m.Lyrics == nil || !strings.Contains(*m.Lyrics, "ワールドイズマイン") || m.Genre == nil || *m.Genre != "Pop" ||
		m.TrackNumber == nil || *m.TrackNumber != 7 || m.Year == nil || *m.Year != 2009 {
		t.Fatalf("metadata = %+v", m)
	}
	if len(d.Metadata.Title) == 0 || !strings.Contains(d.Metadata.Title, "World is Mine") {
		t.Fatalf("title = %q", d.Metadata.Title)
	}
}
