package lyrics

import (
	"errors"
	"github.com/norhua/sonexa/internal/lyricdoc"
	"strings"
	"testing"
)

func scoreDocument(t *testing.T, p *lyricdoc.Input, match, duration int) ContentScore {
	t.Helper()
	d, err := lyricdoc.Build(p)
	if err != nil {
		t.Fatal(err)
	}
	s := ScoreContent(Candidate{MatchScore: match, HighConfidence: true}, d, duration)
	if s.TotalScore == nil {
		t.Fatal("missing score")
	}
	sum := 0
	for _, term := range s.Terms {
		sum += term.Value
	}
	if sum != *s.TotalScore {
		t.Fatal("terms don't sum")
	}
	return s
}

func TestContentScoreFormula(t *testing.T) {
	for _, tt := range []struct {
		name, text        string
		score, sync, word int
	}{
		{"plain", "AB CD", 80, 0, 0},
		{"synced", "[00:01]AB CD", 90, 10, 0},
		{"words", "[00:01]<00:01>AB <00:02>CD<00:03>", 110, 10, 20},
		{"single word sentence", "[00:01]<00:01>AB CD<00:03>", 90, 10, 0},
		{"simultaneous", "[00:01]<00:01>AB <00:01>CD", 90, 10, 0},
		{"partial", "[00:01]<00:01>AB <00:02>CD\nEF GH", 95, 5, 10},
		{"unicode", "[00:01]<00:01>日é <00:02>語2 😀", 110, 10, 20},
		{"repeat copy", "[00:01]AB CD\n[00:01]<00:01>AB <00:02>CD", 110, 10, 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := scoreDocument(t, &lyricdoc.Input{Text: tt.text}, 80, 0)
			if *s.TotalScore != tt.score || s.Terms[1].Value != tt.sync || s.Terms[2].Value != tt.word {
				t.Fatal(s)
			}
		})
	}
	full := scoreDocument(t, &lyricdoc.Input{Text: "[00:01]<00:01>A <00:02>B", Translation: lyricdoc.TextChannel("translation", nil), Romanization: lyricdoc.TextChannel("roma", nil)}, 100, 0)
	if *full.TotalScore != 150 {
		t.Fatal(full)
	}
	for _, doc := range []*lyricdoc.Document{nil, {}, {Translation: lyricdoc.TextChannel("only translation", nil)}} {
		if ScoreContent(Candidate{MatchScore: 100}, doc, 0).TotalScore != nil {
			t.Fatal("no original scored")
		}
	}
}

func TestChannelPresenceAndBonusesAgree(t *testing.T) {
	for _, tt := range []struct {
		name              string
		translation, roma *lyricdoc.Channel
		bonus             int
	}{
		{"none", lyricdoc.AbsentChannel(), lyricdoc.AbsentChannel(), 0},
		{"roma", lyricdoc.AbsentChannel(), lyricdoc.TextChannel("roma", nil), 5},
		{"translation", lyricdoc.TextChannel("translation", nil), lyricdoc.AbsentChannel(), 15},
		{"both", lyricdoc.TextChannel("translation", nil), lyricdoc.TextChannel("roma", nil), 20},
		{"error", lyricdoc.FailedChannel(errors.New("decode")), lyricdoc.UnknownChannel(), 0},
		{"empty", lyricdoc.TextChannel("[00:01]", nil), lyricdoc.AbsentChannel(), 0},
		{"unknown origin", &lyricdoc.Channel{Status: "available", Origin: "unknown", Text: "unclassified"}, lyricdoc.UnknownChannel(), 0},
		{"duplicate", lyricdoc.TextChannel("original", nil), lyricdoc.AbsentChannel(), 0},
		{"partial", &lyricdoc.Channel{Status: "available", Origin: "embedded", SyncStatus: "partial", Text: "[00:01]translation"}, lyricdoc.AbsentChannel(), 15},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, err := lyricdoc.Build(&lyricdoc.Input{Text: "[00:01]original", Translation: tt.translation, Romanization: tt.roma})
			if err != nil {
				t.Fatal(err)
			}
			s := ScoreContent(Candidate{MatchScore: 80}, d, 0)
			if s.TotalScore == nil || *s.TotalScore != 90+tt.bonus {
				t.Fatal(s)
			}
			for i, ch := range []*lyricdoc.Channel{d.Translation, d.Romanization} {
				if (ch.Status == "available") != lyricdoc.AvailableContent(ch) || lyricdoc.AvailableContent(ch) != (s.Terms[3+i].Value > 0) {
					t.Fatal("status and score disagree", ch, s)
				}
				if ch.Track != nil && !strings.Contains(d.Text, ch.Track.Lines[0].Text) {
					t.Fatal("retained text lost")
				}
			}
			if tt.name == "partial" && d.Translation.SyncStatus != "partial" {
				t.Fatal("partial lost")
			}
		})
	}
}

func TestCoverageDoesNotRewardLineCountOrTailLength(t *testing.T) {
	base := "[00:01]<00:01>A <00:02>B"
	a := scoreDocument(t, &lyricdoc.Input{Text: base}, 85, 200000)
	b := scoreDocument(t, &lyricdoc.Input{Text: base + "\n[00:10]<00:10>A <00:11>B"}, 85, 200000)
	if *a.TotalScore != *b.TotalScore || !a.AutoPreviewEligible || !b.AutoPreviewEligible {
		t.Fatal(a, b)
	}
	plain := scoreDocument(t, &lyricdoc.Input{Text: "[00:01]original"}, 90, 0)
	translated := scoreDocument(t, &lyricdoc.Input{Text: "[00:01]original", Translation: lyricdoc.TextChannel("translation", nil)}, 85, 0)
	if *plain.TotalScore != 100 || *translated.TotalScore != 110 {
		t.Fatal(plain, translated)
	}
	// Optional track times do not penalize the selected original timeline.
	extra := scoreDocument(t, &lyricdoc.Input{Text: base, Translation: lyricdoc.TextChannel("[23:00]translation", nil)}, 85, 200000)
	if extra.Terms[5].Value != 0 || !extra.AutoPreviewEligible {
		t.Fatal(extra)
	}
}

func TestTimelinePenaltyAndEligibility(t *testing.T) {
	for _, tt := range []struct {
		text            string
		duration, total int
		eligible        bool
	}{
		{"[00:15]A", 10000, 10, true},
		{"[00:15.001]A", 10000, -20, false},
		{"[00:01]<00:01>A<00:15.001>", 10000, -20, false},
		{"[offset:1000]\n[00:16]A", 10000, 10, true},
		{"[00:59]A", 0, 10, true},
		{"[00:01]A", 300000, 10, true},
	} {
		s := scoreDocument(t, &lyricdoc.Input{Text: tt.text}, 0, tt.duration)
		if *s.TotalScore != tt.total || s.AutoPreviewEligible != tt.eligible {
			t.Fatalf("%s: %+v", tt.text, s)
		}
	}
}
