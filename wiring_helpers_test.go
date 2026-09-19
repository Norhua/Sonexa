package main

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
	return covers.NewService(cache, cur.Cache.CoverConcurrency, "test")
}
