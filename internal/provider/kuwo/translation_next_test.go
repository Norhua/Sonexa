package kuwo

import (
	"fmt"
	"testing"
)

// Successor foreign starts coincide with the preceding Chinese source row.
func nextTimeRows(count int, tail bool) []lyricRow {
	rows := []lyricRow{}
	for i := 0; i < count; i++ {
		rows = append(rows, lyricRow{timeMS: int64(i+1) * 3000, text: fmt.Sprintf("Original phrase %d", i)}, lyricRow{timeMS: int64(i+2) * 3000, text: fmt.Sprintf("译文%d", i)})
	}
	if !tail {
		rows = append(rows, lyricRow{timeMS: int64(count+1) * 3000, text: "Next original"})
	}
	for i := range rows {
		rows[i].position = i
	}
	return rows
}

func TestNextTimeFourStrongMatchesAndTail(t *testing.T) {
	for _, tt := range []struct {
		name  string
		count int
		tail  bool
		want  int
	}{
		{"four strong", 4, false, 4}, {"three strong", 3, false, 0}, {"three plus tail", 4, true, 0}, {"four plus tail", 5, true, 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := nextTimeAssociations(nextTimeRows(tt.count, tt.tail), nil)
			if len(got) != tt.want {
				t.Fatalf("%d != %d", len(got), tt.want)
			}
		})
	}
	rows := nextTimeRows(5, true)
	rows[8].text = "Ooh Yeah"
	if got := nextTimeAssociations(rows, nil); len(got) != 4 {
		t.Fatal("vocal tail extracted", got)
	}
	rows = nextTimeRows(5, true)
	rows[7].text = "男："
	if got := nextTimeAssociations(rows, []inferredLine{{row: 100}}); len(got) != 3 {
		t.Fatal("tail crossed broken chain", got)
	}
	rows = nextTimeRows(4, false)
	rows[0].text = "Ooh Yeah"
	if got := nextTimeAssociations(rows, nil); len(got) != 0 {
		t.Fatal("vocal sound counted toward threshold", got)
	}
	for _, text := range []string{"Ooh", "Yeah", "Oh oh", "Woah", "Ah", "La la", "Hmm"} {
		if !onlyVocalSounds(text) {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"Oh my love", "Yeah we go", "Hello world", "かな"} {
		if onlyVocalSounds(text) {
			t.Fatal(text)
		}
	}
}

func TestNextTimeMixedSingleHanAndBarriers(t *testing.T) {
	for _, tt := range []struct {
		name, text string
		want       int
	}{
		{"single Han", "爱", 5}, {"mixed short", "爱你 X", 5}, {"borrowed", "爱 Original", 5},
		{"mixed too much", "爱 Alice", 4}, {"kana ambiguity", "爱かな", 0}, {"label", "合：", 4}, {"credit", "翻译：姓名", 4}, {"empty", "", 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rows := nextTimeRows(5, false)
			rows[1].text = tt.text
			got := nextTimeAssociations(rows, nil)
			// Kana is an F row. At the next original's timestamp it also
			// makes that original ambiguous, leaving only three strong matches.
			if len(got) != tt.want {
				t.Fatalf("%d != %d", len(got), tt.want)
			}
		})
	}
	rows := nextTimeRows(5, false)
	rows[1].text, rows[1].timeMS = "爱かな", 4500
	if got := nextTimeAssociations(rows, nil); len(got) != 4 {
		t.Fatal("unambiguous kana must not remove unrelated strong matches", got)
	}
	rows = nextTimeRows(4, false)
	rows[8].text = "" // empty timing line is valid successor evidence
	if got := nextTimeAssociations(rows, nil); len(got) != 4 {
		t.Fatal(got)
	}
	for _, barrier := range []string{"男：", "版权声明：保留", "???"} {
		rows := nextTimeRows(5, false)
		rows = append(rows[:1], append([]lyricRow{{timeMS: 4500, text: barrier}}, rows[1:]...)...)
		if got := nextTimeAssociations(rows, nil); len(got) != 4 {
			t.Fatal("crossed written boundary", barrier, got)
		}
	}
	rows = nextTimeRows(5, false)
	rows[1].timeMS = rows[0].timeMS
	if got := nextTimeAssociations(rows, nil); len(got) != 4 {
		t.Fatal("simultaneous Chinese is not translation", got)
	}
	rows = nextTimeRows(5, false)
	rows[2].timeMS = rows[0].timeMS
	if got := nextTimeAssociations(rows, nil); len(got) != 0 {
		t.Fatal("ambiguous chorus bypassed threshold", got)
	}
}

func TestSecondLayerAfterFirstAndChineseSong(t *testing.T) {
	rows := alternation(8, 8, 8)
	// Single Han cannot be extracted by layer one; layer two may supplement
	// only this one line once the whole-song evidence is established.
	rows = append(rows, lyricRow{position: 16, timeMS: 20000, text: "Final original"}, lyricRow{position: 17, timeMS: 23000, text: "爱"}, lyricRow{position: 18, timeMS: 23000, text: ""})
	first := wholeAlternation(rows, foreignSong)
	second := nextTimeAssociations(rows, first)
	if len(first) != 8 || len(second) != 1 || second[0].row != 17 {
		t.Fatal(first, second)
	}
	d := fetchSyntheticRows(t, rows, foreignSong, false)
	if len(d.Translation.Track.Lines) != 9 || len(d.Translation.Diagnostics) < 2 || d.Translation.Diagnostics[1].Inference.Layer != 2 {
		t.Fatal(d.Translation)
	}
	// No foreign-song metadata condition applies to layer two.
	chinese := foreignSong
	chinese.Title = "中文歌曲"
	chinese.Artist = "中文歌手"
	d = fetchSyntheticRows(t, nextTimeRows(4, false), chinese, false)
	if d.Translation.Status != "available" || d.Translation.Diagnostics[0].Inference.Layer != 2 {
		t.Fatal(d.Translation)
	}
	// Removing first-layer rows must never create multiple layer-two matches
	// for a single original.
	seen := map[int]bool{}
	for _, match := range nextTimeAssociations(nextTimeRows(5, false), nil) {
		if seen[match.original] {
			t.Fatal(match)
		}
		seen[match.original] = true
	}
}
