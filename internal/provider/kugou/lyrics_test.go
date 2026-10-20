package kugou

import (
	"testing"

	"github.com/norhua/sonexa/internal/lyrics"
)

func TestSelectLyricCandidateIdentityBeforeDuration(t *testing.T) {
	c := lyrics.Candidate{Title: "Song", Artist: "Artist", Ref: "hash", DurationMS: 100000}
	rows := []lyricCandidate{
		{ID: "broken", Song: "Song", Duration: 100000},
		{ID: "wrong-version", Accesskey: "key", Song: "Song (off vocal)", Duration: 100000},
		{ID: "wrong-title", Accesskey: "key", Song: "Unrelated", Duration: 100000},
		{ID: "b", Accesskey: "key", Song: "Song", Singer: "uploader", Duration: 100100},
		{ID: "a", Accesskey: "key", Song: "Song", Singer: "another uploader", Duration: 100100},
	}
	got, err := selectLyricCandidate(c, rows)
	if err != nil || got.ID != "a" {
		t.Fatal(got, err)
	}
	rows[3].Duration = 100010
	got, err = selectLyricCandidate(c, rows)
	if err != nil || got.ID != "b" {
		t.Fatal(got, err)
	}
	if _, err := selectLyricCandidate(c, rows[:3]); err == nil {
		t.Fatal("invalid candidates accepted")
	}
}
