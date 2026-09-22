package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOldITunesTTLSettingIsIgnoredAndRemovedOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"cache":{"itunesTracklistTtlDays":1,"providerCacheTtlHours":72,"coverTtlDays":15}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := st.Get()
	if settings.Cache.ProviderCacheTTLh != 72 || settings.Cache.CoverTTLDays != 15 {
		t.Fatal("删除 iTunes 配置不应影响通用 TTL")
	}
	if err := st.Save(settings); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "itunesTracklistTtlDays") {
		t.Fatalf("旧字段仍被写入: %s %v", data, err)
	}
}
