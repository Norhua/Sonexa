package app

import (
	"testing"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/covers"
)

func newTestCoverService(t *testing.T, dir string, cur config.Settings) *covers.Service {
	t.Helper()
	cache, err := covers.NewDiskCache(dir, 0, 0)
	if err != nil {
		t.Fatalf("建立封面缓存失败: %v", err)
	}
	svc := covers.NewService(cache, cur.Cache.CoverConcurrency, "test")
	// Keep adapter defaults (including CAA index decoding) identical to the
	// production constructor. Tests vary storage/network, not protocol wiring.
	if err := configureCoverDefaults(svc); err != nil {
		t.Fatalf("装配封面默认能力失败: %v", err)
	}
	return svc
}
