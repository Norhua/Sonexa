package qqmusic

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
)

func TestQQOnlineNearbyLabelsBeforeDelivery(t *testing.T) {
	body := playBody(t, qqLyricData{QRC: 1,
		Lyric: encodedQRC(t, "[42570,3000]かな(42570,1000)の歌(43570,2000)"),
		Trans: b64("[00:41.36]中文翻译"),
		Roma:  encodedQRC(t, "[42800,3000]kana no uta(42800,3000)"),
	}, qqLyricData{})
	calls := 0
	client := providerClientForTest(func(*http.Request) string { calls++; return body })
	d, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Ref: "mid"})
	if err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if len(d.Rows) != 1 || len(d.Rows[0].Translation) != 1 || len(d.Rows[0].Romanization) != 1 || !strings.Contains(d.Text, "[>zh]中文翻译") || !strings.Contains(d.Text, "[>ja-Latn]") {
		t.Fatal(d.Text)
	}
	if *d.Romanization.Track.Lines[0].Parts[0].StartMS != 42800 || *d.Romanization.Track.Lines[0].EndMS != 45800 {
		t.Fatal("native word/line ends changed")
	}
	round, err := lyricdoc.ParseXLRC(d.Text)
	if err != nil || !reflect.DeepEqual(round.Rows, d.Rows) {
		t.Fatal("roundtrip changed association", err)
	}
}

func TestQRCXLRCVoiceRubyAndExplicitAdjacentEnds(t *testing.T) {
	ch := qqDecodedChannel("[1000,2000][v:A]私[わたし](1000,500)が(1500,500)", nil)
	d, err := lyricdoc.Build(&lyricdoc.Input{Original: ch})
	if err != nil {
		t.Fatal(err)
	}
	l := d.Original.Track.Lines[0]
	if l.Voice != "A" || l.Text != "私が" || len(l.Ruby) != 1 || *l.EndMS != 3000 || *l.Parts[0].EndMS != 1500 || *l.Parts[1].StartMS != 1500 {
		t.Fatal(l)
	}
	next, err := lyricdoc.ParseXLRC(d.Text)
	if err != nil || next.Rows[0].Voice != "A" || len(next.Rows[0].Ruby) != 1 || next.Original.Track.Lines[0].Parts[0].EndMS == nil {
		t.Fatal(next, err)
	}
}
