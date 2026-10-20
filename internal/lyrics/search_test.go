package lyrics

import (
	"strings"
	"testing"
)

func TestLyricsRelevance(t *testing.T) {
	q := Query{Title: "綿雪の夢", Artist: "ハトリアヤ", Album: "狼と巫女とお菓子の家", DurationMS: 321000}
	candidates := []Candidate{
		{Ref: "unrelated", Title: "minamo", Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "other", Title: q.Title, Artist: "Other", Album: "Other", DurationMS: 100000},
		{Ref: "accompaniment", Title: q.Title + " (伴奏)", Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "instrumental", Title: q.Title + " (Off vocal)", Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "version", Title: q.Title + " (Applice Version)", Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "group", Title: q.Title + " (繁雪之梦)", Artist: "Crest", Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "long", Title: q.Title, Artist: q.Artist, Album: q.Album, DurationMS: 400000},
		{Ref: "exact", Title: q.Title, Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
	}
	got := rankCandidates(q, candidates)
	if len(got) != 3 || got[0].Ref != "exact" || !got[0].HighConfidence || got[0].MatchScore != 100 {
		t.Fatalf("%+v", got)
	}
	for _, c := range got[1:] {
		if c.HighConfidence || !strings.Contains(strings.Join(c.MatchNotes, " "), "需确认") {
			t.Fatalf("unsafe automatic match %+v", c)
		}
	}
	// Close duration alone only rescues an exact title, never an unrelated title.
	q.Album = "different album"
	got = rankCandidates(q, []Candidate{candidates[5]})
	if len(got) != 1 || got[0].HighConfidence {
		t.Fatalf("group credit not retained for confirmation: %+v", got)
	}
	q.DurationMS = 0
	if got := rankCandidates(q, []Candidate{candidates[5]}); len(got) != 0 {
		t.Fatalf("accepted uncorroborated title: %+v", got)
	}
}

func TestMatchTopDedupTermsAndStableOrder(t *testing.T) {
	q := Query{Title: "Song", Artist: "Artist", Album: "Album", DurationMS: 100000}
	base := Candidate{Source: "qq", Ref: "b", Title: q.Title, Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS}
	a, low, c, d := base, base, base, base
	a.Ref, c.Ref, d.Ref = "a", "c", "d"
	low.Artist, low.Album = "", ""
	rows := rankCandidates(q, []Candidate{d, low, c, base, a, base})
	if len(rows) != 4 {
		t.Fatal(rows)
	}
	top := MatchTop(rows)
	if len(top) != 3 || top[0].Ref != "a" || top[1].Ref != "b" || top[2].Ref != "c" || top[1].MatchScore != 100 {
		t.Fatal(top)
	}
	if len(MatchTop(rows[:2])) != 2 || len(MatchTop(nil)) != 0 {
		t.Fatal("invented candidates")
	}
	for _, c := range rows {
		sum := 0
		for _, term := range c.MatchTerms {
			sum += term.Value
		}
		if sum != c.MatchScore || len(c.MatchTerms) != 4 {
			t.Fatal(c)
		}
	}
	base.DurationMS = 111000
	long := rankCandidates(q, []Candidate{base})[0]
	if long.MatchScore != 75 || long.HighConfidence || long.MatchTerms[4].Value != -20 {
		t.Fatal(long)
	}
	// A manual-confirmation candidate with a better score precedes high confidence.
	if !matchLess(Candidate{MatchScore: 90}, Candidate{MatchScore: 80, HighConfidence: true}) {
		t.Fatal("confidence before score")
	}
}

func TestLyricsPartialQueriesAndNormalization(t *testing.T) {
	for _, tt := range []struct {
		name       string
		q          Query
		c          Candidate
		keep, high bool
	}{
		{"fullwidth", Query{Title: "ＡＢＣ", Artist: "ハトリアヤ"}, Candidate{Title: "abc", Artist: "はとりあや"}, true, true},
		{"title only", Query{Title: "Same"}, Candidate{Title: "Same"}, true, false},
		{"artist only", Query{Artist: "Alice"}, Candidate{Title: "Song", Artist: "Alice"}, true, false},
		{"wrong artist", Query{Artist: "Alice"}, Candidate{Title: "Song", Artist: "Bob"}, false, false},
		{"missing artist", Query{Artist: "Alice"}, Candidate{Title: "Song"}, false, false},
		{"album only", Query{Album: "Album"}, Candidate{Title: "Song", Album: "Album"}, true, false},
		{"wrong album", Query{Album: "Album"}, Candidate{Title: "Song", Album: "Unrelated"}, false, false},
		{"duration missing", Query{Title: "Song", Artist: "Alice"}, Candidate{Title: "Song", Artist: "Alice"}, true, true},
		{"explicit accompaniment", Query{Title: "Song (伴奏)", Artist: "Alice"}, Candidate{Title: "Song (伴奏)", Artist: "Alice"}, true, true},
		{"short title", Query{Title: "A", Artist: "Alice"}, Candidate{Title: "Another", Artist: "Alice"}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := rankCandidates(tt.q, []Candidate{tt.c})
			if (len(got) > 0) != tt.keep || len(got) > 0 && got[0].HighConfidence != tt.high {
				t.Fatalf("%+v", got)
			}
		})
	}
}
