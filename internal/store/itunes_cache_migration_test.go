package store

import (
	"testing"
	"time"
)

func TestITunesCacheMigrationRemovesOnlyItsExpiry(t *testing.T) {
	s, _ := openV1CoverStore(t)
	for _, source := range []string{"itunes", "musicbrainz"} {
		if err := s.PutCache(source, "/lookup", "hash", []byte("response"), time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE provider_cache SET fetched_at=?`, time.Now().AddDate(-1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	entry, err := s.GetCache("itunes", "/lookup", "hash")
	if err != nil || entry.TTL != 0 || string(entry.Body) != "response" {
		t.Fatalf("旧 iTunes 响应仍失效: %+v %v", entry, err)
	}
	if _, err := s.GetCache("musicbrainz", "/lookup", "hash"); err != ErrCacheMiss {
		t.Fatalf("MB 通用 TTL 不应被清除: %v", err)
	}
}
