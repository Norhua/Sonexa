package kuwo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func syntheticRows(texts ...string) []lyricRow {
	rows := make([]lyricRow, len(texts))
	for i, text := range texts {
		rows[i] = lyricRow{position: i, timeMS: int64(i+1) * 1000, text: text}
	}
	return rows
}

func alternation(foreign, han, paired int) []lyricRow {
	texts := []string{}
	for i := 0; i < paired; i++ {
		texts = append(texts, fmt.Sprintf("Original verse %d", i), fmt.Sprintf("译文%d", i))
	}
	for i := paired; i < foreign; i++ {
		texts = append(texts, fmt.Sprintf("Remaining verse %d", i))
	}
	for i := paired; i < han; i++ {
		texts = append(texts, fmt.Sprintf("余下译文%d", i))
	}
	return syntheticRows(texts...)
}

var foreignSong = lyrics.Candidate{Title: "Song", Artist: "Artist"}

func TestWholeAlternationThresholds(t *testing.T) {
	for _, tt := range []struct {
		name               string
		f, h, paired, want int
	}{
		{"minimum", 8, 8, 8, 8}, {"too few foreign", 7, 8, 7, 0}, {"too few Han", 8, 7, 7, 0},
		{"lower ratio exact", 20, 14, 14, 14}, {"below ratio", 20, 13, 13, 0},
		{"multiple translations", 10, 12, 10, 12}, {"upper ratio but insufficient alternating", 10, 13, 10, 0}, {"above upper ratio", 10, 14, 10, 0},
		{"80 percent exact", 10, 10, 7, 10}, {"below 80 percent", 10, 10, 6, 0},
		{"paragraph bilingual", 10, 10, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rows := alternation(tt.f, tt.h, tt.paired)
			got := wholeAlternation(rows, foreignSong)
			if len(got) != tt.want {
				t.Fatalf("got %d want %d", len(got), tt.want)
			}
		})
	}
}

func TestWholeAlternationForeignSongAndScripts(t *testing.T) {
	for _, tt := range []struct {
		name, title, artist, foreign string
		east, want                   int
	}{
		{"Chinese title English body", "中文歌", "Artist", "かなの歌", 0, 0},
		{"missing artist", "Song", "", "かなの歌", 0, 0},
		{"Japanese auxiliary exact", "中文标题", "艺术家", "かなの歌", 4, 8},
		{"Japanese auxiliary below", "中文标题", "艺术家", "かなの歌", 3, 0},
		{"Korean title", "노래", "가수", "노래 가사", 8, 8},
		{"Japanese Han title", "雪の夢", "歌手", "かなの歌", 8, 8},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rows := alternation(8, 8, 8)
			for i := 0; i < tt.east; i++ {
				rows[i*2].text = tt.foreign
			}
			got := wholeAlternation(rows, lyrics.Candidate{Title: tt.title, Artist: tt.artist})
			if len(got) != tt.want {
				t.Fatalf("got %d", len(got))
			}
		})
	}
	for text, want := range map[string]lineClass{
		"A": mixedLine, "AB": foreignLine, "中文": hanLine, "中": mixedLine, "AB中文": mixedLine,
		"日かな本": foreignLine, "한글中文": foreignLine, "译文é": mixedLine, "男：": ignoredLine, "合：": ignoredLine,
		"作词：姓名": ignoredLine, "Copyright: someone": ignoredLine, "© 2026": ignoredLine, "普通版权歌词": hanLine,
	} {
		if got := classifyRow(lyricRow{text: text}); got != want {
			t.Fatalf("%q: %d != %d", text, got, want)
		}
	}
}

func TestWholeAlternationConservativeAssociation(t *testing.T) {
	for _, tt := range []struct {
		name, insert string
		want         bool
	}{
		{"borrowed name", "译文 Original", true}, {"unknown name", "译文 Alice", false},
		{"one Han", "字 Original", false}, {"credit", "翻译：姓名", false}, {"copyright", "版权所有", false}, {"label", "女：", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rows := alternation(10, 10, 10)
			rows = append(rows, lyricRow{position: 20, timeMS: 21000, text: "Original final"}, lyricRow{position: 21, timeMS: 22000, text: tt.insert})
			found := false
			for _, match := range wholeAlternation(rows, foreignSong) {
				if match.row == 21 {
					found = true
				}
			}
			if found != tt.want {
				t.Fatal("mixed/credit classification", found)
			}
		})
	}
	rows := alternation(10, 10, 10)
	// First two foreign lines share a time: neither may own a translation.
	rows[2].timeMS = rows[0].timeMS
	got := wholeAlternation(rows, foreignSong)
	if len(got) != 8 {
		t.Fatalf("ambiguous original: %+v", got)
	}
	// Written labels cut association even though excluded from statistics.
	rows = alternation(10, 10, 10)
	rows = append(rows[:1], append([]lyricRow{{text: "男：", timeMS: 1500}}, rows[1:]...)...)
	for _, match := range wholeAlternation(rows, foreignSong) {
		if match.row == 2 {
			t.Fatal("crossed a label")
		}
	}
}

func fetchSyntheticRows(t *testing.T, rows []lyricRow, c lyrics.Candidate, h5 bool) *lyricdoc.Document {
	t.Helper()
	list := []map[string]any{}
	for _, row := range rows {
		list = append(list, map[string]any{"time": fmt.Sprintf("%.3f", float64(row.timeMS)/1000), "lineLyric": row.text})
	}
	key := "code"
	if h5 {
		key = "status"
	}
	body, err := json.Marshal(map[string]any{key: 200, "data": map[string]any{"lrclist": list}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := provider.NewClient(nil, nil, "test")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		text := string(body)
		if h5 && calls == 1 {
			text = `{"code":200,"data":null}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(text))}, nil
	})
	d, err := New(client).FetchLyrics(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if err := lyricdoc.Validate(d); err != nil {
		t.Fatal(err)
	}
	want := 1
	if h5 {
		want = 2
	}
	if calls != want {
		t.Fatal(calls)
	}
	return d
}

func TestWholeAlternationDeliveredBeforeScoring(t *testing.T) {
	for _, h5 := range []bool{false, true} {
		rows := alternation(8, 8, 8)
		d := fetchSyntheticRows(t, rows, foreignSong, h5)
		if d.Translation.Status != "available" || d.Translation.Origin != "embedded" || len(d.Original.Track.Lines) != 8 || len(d.Rows) != 8 {
			t.Fatal(d)
		}
		diagnostic := d.Translation.Diagnostics[0]
		if diagnostic.Code != "heuristic_translation" || diagnostic.Inference.Layer != 1 || diagnostic.Inference.Count != 8 || diagnostic.Inference.RuleVersion != translationRuleVersion {
			t.Fatal(diagnostic)
		}
		for i, row := range d.Rows {
			if len(row.Translation) != 1 || row.Translation[0] != rows[i*2+1].text || *row.TimeMS != rows[i*2].timeMS {
				t.Fatal(row)
			}
			assoc := diagnostic.Inference.Associations[i]
			if assoc.SourcePosition != i*2+1 || assoc.OriginalPosition != i*2 || assoc.SourceTimeMS != rows[i*2+1].timeMS || assoc.AssociatedTimeMS != rows[i*2].timeMS {
				t.Fatal(assoc)
			}
		}
		score := lyrics.ScoreContent(lyrics.Candidate{MatchScore: 80}, d, 0)
		if *score.TotalScore != 105 || score.Terms[3].Value != 15 {
			t.Fatal(score)
		}
		for _, row := range rows {
			if strings.Count(d.Text, row.text) != 1 {
				t.Fatalf("text lost/duplicated: %q", row.text)
			}
		}
	}
	d := fetchSyntheticRows(t, alternation(7, 7, 7), foreignSong, false)
	if d.Translation.Status != "unknown" || len(d.Original.Track.Lines) != 14 {
		t.Fatal(d)
	}
	rows := append(syntheticRows("无前句中文", "版权声明：保留", "[ti:Synthetic]"), alternation(12, 13, 12)...)
	d = fetchSyntheticRows(t, rows, foreignSong, false)
	for _, text := range []string{"无前句中文", "版权声明：保留", "[ti:Synthetic]", "余下译文12"} {
		if strings.Count(d.Text, text) != 1 {
			t.Fatal("statement/paragraph text lost", text)
		}
	}
	if d.Translation.Status != "available" || len(d.Translation.Track.Lines) != 13 {
		t.Fatal(d.Translation)
	}
}
