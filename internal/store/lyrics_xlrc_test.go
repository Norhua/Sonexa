package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestXLRCImmutableExtensionAndBytes(t *testing.T) {
	s := openTestStore(t)
	for _, tc := range []struct{ text, extension string }{
		{"[xlrc:0.1]\n[x-sonexa:1]\n[00:01][v:A]私[わたし]\n[>en]I", ".xlrc"},
		{"[lang:ja]\n[00:01]原文\n[>en]translation", ".xlrc"},
		{"literal [>en] not a tag", ".lrc"},
	} {
		draft, err := s.StageLyrics(context.Background(), tc.text)
		if err != nil {
			t.Fatal(err)
		}
		prepared, err := s.prepareLyrics(context.Background(), 1, draft)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(prepared.path, tc.extension) {
			t.Fatal(prepared.path)
		}
		b, err := os.ReadFile(filepath.Join(s.lyricsRoot, prepared.path))
		if err != nil || string(b) != tc.text {
			t.Fatal(string(b), err)
		}
		if err := s.DiscardLyrics(draft); err != nil {
			t.Fatal(err)
		}
	}
}
