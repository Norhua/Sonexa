package store

import (
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func TestWriteDetailColumnsLyrics(t *testing.T) {
	s := openTestStore(t)
	title, album, ref := "ワールドイズマイン", "supercell", "000BDJvT2aWQYZ"
	id, err := s.AdmitCandidate(provider.Candidate{Provider: provider.QQMusic, MatchRef: "003RcjkV1XjQQy", Title: &title, AlbumTitle: &album, ReleaseRef: &ref})
	if err != nil {
		t.Fatal(err)
	}
	lyrics := "[00:00.00]歌词"
	if err := s.WriteDetailColumns(id, &provider.Detail{Provider: provider.QQMusic, MatchRef: "003RcjkV1XjQQy", Lyrics: &lyrics}); err != nil {
		t.Fatal(err)
	}
	// 后续无歌词的详情不清空已有歌词
	if err := s.WriteDetailColumns(id, &provider.Detail{Provider: provider.QQMusic, MatchRef: "003RcjkV1XjQQy"}); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Lyrics == nil || *m.Lyrics != lyrics {
		t.Fatalf("歌词未写入: %v", m.Lyrics)
	}
}
