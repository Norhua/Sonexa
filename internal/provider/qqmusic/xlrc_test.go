package qqmusic

import (
	"github.com/norhua/sonexa/internal/lyricdoc"
	"testing"
)

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
