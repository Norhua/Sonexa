package matching

import (
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func songFacts(providerID, recording, title, credit string, durationMS *int) SongFacts {
	return SongFacts{Provider: provider.ProviderID(providerID), RecordingRef: recording, Title: title, Credit: credit, DurationMS: durationMS}
}

func TestCompareSongRecordingLevelWins(t *testing.T) {
	d := 200000
	cases := []struct {
		name     string
		a, b     SongFacts
		expected SongIdentity
	}{
		{"同来源同录音", songFacts("musicbrainz", "rec-1", "Song", "Artist", &d), songFacts("musicbrainz", "rec-1", "Song (Remastered)", "Artist", &d), SongSameRecording},
		{"同来源不同录音不做文本级", songFacts("musicbrainz", "rec-1", "Song", "Artist", &d), songFacts("musicbrainz", "rec-2", "Song", "Artist", &d), SongDifferent},
		{"跨来源退到文本级", songFacts("musicbrainz", "rec-1", "Song", "Artist", &d), songFacts("qq", "rec-1", "Song", "Artist", &d), SongSameText},
		{"单侧无录音标识退到文本级", songFacts("musicbrainz", "", "Song", "Artist", &d), songFacts("musicbrainz", "rec-1", "Song", "Artist", &d), SongSameText},
		{"版本标记不同即不同", songFacts("qq", "", "Song", "Artist", &d), songFacts("qq", "", "Song (Live)", "Artist", &d), SongDifferent},
		{"时长超出容差即不同", songFacts("qq", "", "Song", "Artist", &d), songFacts("qq", "", "Song", "Artist", ptrInt(d+4000)), SongDifferent},
		{"时长缺失时只看曲名", songFacts("qq", "", "Song", "Artist", nil), songFacts("qq", "", "Song", "Artist", &d), SongSameText},
		{"署名冲突即不同", songFacts("qq", "", "Song", "Artist A", &d), songFacts("qq", "", "Song", "完全不同的名字", &d), SongDifferent},
		{"无可比事实", songFacts("qq", "", "", "Artist", &d), songFacts("qq", "", "Song", "Artist", &d), SongUnknown},
	}
	if got := CompareSong(songFacts("musicbrainz", "rec-1", "Song", "Artist", &d), songFacts("qq", "", "Song", "Artist", &d)); got != SongSameText {
		t.Fatalf("跨来源文本级: %v", got)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompareSong(tc.a, tc.b); got != tc.expected {
				t.Fatalf("got %v want %v", got, tc.expected)
			}
			if got := CompareSong(tc.b, tc.a); got != tc.expected {
				t.Fatalf("not symmetric: %v", got)
			}
		})
	}
}

func ptrInt(v int) *int { return &v }
