package caa

import (
	"testing"

	"github.com/norhua/sonexa/internal/domain"
)

func TestArtworkDeclaresValidOrderedCatalogTargets(t *testing.T) {
	strategy := ArtworkStrategy()
	release, group := "2659e6f6-ce21-4198-92e5-4878f5e7e3c0", "e8ec3011-cd49-4558-9d11-01f7fe9b552f"
	keys := strategy.CatalogKeys(release, group)
	if len(keys) != 2 || keys[0] != (domain.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: release}) || keys[1] != (domain.CoverCatalogKey{Source: "caa", ObjectType: "release-group", SourceRef: group}) {
		t.Fatal(keys)
	}
	for _, group := range []string{"", "invalid-group"} {
		if keys := strategy.CatalogKeys(release, group); len(keys) != 1 || keys[0].SourceRef != release {
			t.Fatal(keys)
		}
	}
	if keys := strategy.CatalogKeys("invalid-release", group); len(keys) != 0 {
		t.Fatal("无效主身份不得通过回退取图", keys)
	}
}
