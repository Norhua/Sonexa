package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func sampleCoverImages() []CoverImage {
	return []CoverImage{
		{SourceImageID: "front-a", PrimaryType: "Front", Types: []string{"Front"}, Front: true, Variants: []CoverVariant{{Spec: "250", URL: "https://example.org/front-250.jpg"}, {Spec: "1200", URL: "https://example.org/front-1200.jpg"}}},
		{SourceImageID: "front-b", PrimaryType: "Front", Types: []string{"Front", "Booklet"}, Front: true, Variants: []CoverVariant{{Spec: "250", URL: "https://example.org/other-250.jpg"}}},
	}
}

func mustSaveCatalog(t *testing.T, s *Store, key CoverCatalogKey, images []CoverImage) *CoverCatalog {
	t.Helper()
	if _, err := s.SaveCoverCatalog(key, images); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.GetCoverCatalog(key)
	if err != nil || catalog == nil {
		t.Fatalf("目录读取失败: %+v %v", catalog, err)
	}
	return catalog
}

func sampleCoverFile(variantID, releaseID int64, relativePath string) CoverFile {
	return CoverFile{VariantID: variantID, ReleaseID: &releaseID, RelativePath: relativePath, ContentHash: "sample-sha256", ByteSize: 120,
		Width: 250, Height: 250, Format: "jpeg", FetchedAt: time.Now().UTC()}
}

func TestCoverCatalogStateSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "covers.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	key := CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "r"}
	catalog := mustSaveCatalog(t, s, key, sampleCoverImages())
	if catalog.State != "found" || len(catalog.Images) != 2 || len(catalog.Images[0].Variants) != 2 {
		t.Fatalf("图片身份或规格丢失: %+v", catalog)
	}
	if catalog.Images[0].Variants[0].Width != nil {
		t.Fatal("目录档位不能冒充实际尺寸")
	}
	emptyKey := CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "empty"}
	mustSaveCatalog(t, s, emptyKey, nil)
	errorKey := CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "error"}
	for _, k := range []CoverCatalogKey{key, emptyKey, errorKey} {
		if err := s.RecordCoverCatalogError(k, "HTTP 503", time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		key   CoverCatalogKey
		state string
	}{{key, "found"}, {emptyKey, "empty"}, {errorKey, "error"}} {
		got, err := s.GetCoverCatalog(tc.key)
		if err != nil || got.State != tc.state || got.LastError != "HTTP 503" || got.RetryAfter == nil {
			t.Fatalf("状态/故障记忆丢失: %+v %v", got, err)
		}
		if tc.state == "found" && (len(got.Images) != 2 || got.Images[0].ID != catalog.Images[0].ID) {
			t.Fatal("故障覆盖了已有目录")
		}
	}
	key.SourceRef = "new-binding"
	got, err := s.GetCoverCatalog(key)
	if err != nil || got != nil {
		t.Fatalf("新绑定不能沿用旧状态: %+v %v", got, err)
	}
}

func TestCoverCatalogRefreshRetainsAdoptedIdentity(t *testing.T) {
	s := openTestStore(t)
	key := CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "r"}
	catalog := mustSaveCatalog(t, s, key, sampleCoverImages())
	variant := catalog.Images[0].Variants[0]
	releaseID, err := s.FindOrCreateLocalRelease("测试歌集", nil)
	if err != nil {
		t.Fatal(err)
	}
	file := sampleCoverFile(variant.ID, releaseID, "albums/release-1/Front-caa-250.jpg")
	selection, err := s.AdoptCoverFile(file, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdoptCoverFile(file, 0); !errors.Is(err, ErrCoverSelectionChanged) {
		t.Fatalf("旧版本采用必须拒绝: %v", err)
	}
	if err := s.RecordCoverVariantError(catalog.Images[0].Variants[1].ID, true, "404"); err != nil {
		t.Fatal(err)
	}
	refreshed := mustSaveCatalog(t, s, key, sampleCoverImages())
	if refreshed.Images[0].ID != catalog.Images[0].ID || refreshed.Images[0].Variants[0].ID != variant.ID || refreshed.Version != 2 {
		t.Fatal("刷新改变了稳定身份")
	}
	if refreshed.Images[0].Variants[0].State != "verified" || *refreshed.Images[0].Variants[0].Width != 250 || refreshed.Images[0].Variants[1].State != "unverified" {
		t.Fatalf("验证结果未保留或不可用档未允许重试: %+v", refreshed.Images[0].Variants)
	}
	mustSaveCatalog(t, s, key, nil)
	got, err := s.GetCoverSelection(releaseID)
	if err != nil || got.ImageID != selection.ImageID {
		t.Fatalf("目录删除了用户选择: %+v %v", got, err)
	}
	image, err := s.GetCoverImage(got.ImageID)
	if err != nil || image == nil || image.Active || len(image.Variants) != 2 {
		t.Fatalf("历史图片不可读: %+v %v", image, err)
	}
	files, err := s.ListReleaseCoverFiles(releaseID)
	if err != nil || len(files) != 1 || !files[0].Protected || !files[0].Available {
		t.Fatalf("采用文件丢失: %+v %v", files, err)
	}
	if err := s.MarkCoverFileMissing(files[0].ID); err != nil {
		t.Fatal(err)
	}
	missing, _ := s.GetCoverFile(files[0].ID)
	v, _ := s.GetCoverVariant(variant.ID)
	if missing.Available || v.State != "verified" {
		t.Fatal("文件状态与目录规格没有分离")
	}
}

func TestCoverCatalogInvalidRefreshIsAtomic(t *testing.T) {
	s := openTestStore(t)
	key := CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "r"}
	catalog := mustSaveCatalog(t, s, key, sampleCoverImages())
	bad := sampleCoverImages()
	bad[1].SourceImageID = bad[0].SourceImageID
	if _, err := s.SaveCoverCatalog(key, bad); err == nil {
		t.Fatal("重复图片身份应拒绝")
	}
	got, _ := s.GetCoverCatalog(key)
	if got.Version != catalog.Version || len(got.Images) != 2 {
		t.Fatal("失败刷新留下部分数据")
	}
}

func TestCoverAdoptionFailurePreservesSelection(t *testing.T) {
	s := openTestStore(t)
	key := CoverCatalogKey{Source: "qq", ObjectType: "album", SourceRef: "mid"}
	catalog := mustSaveCatalog(t, s, key, sampleCoverImages())
	releaseID, err := s.FindOrCreateLocalRelease("测试歌集", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := sampleCoverFile(catalog.Images[0].Variants[0].ID, releaseID, "albums/release-1/Front-qq-250.jpg")
	selected, err := s.AdoptCoverFile(first, 0)
	if err != nil {
		t.Fatal(err)
	}
	second := sampleCoverFile(catalog.Images[1].Variants[0].ID, releaseID, first.RelativePath)
	if _, err := s.AdoptCoverFile(second, selected.Revision); err == nil {
		t.Fatal("不允许覆盖另一张图片的路径")
	}
	got, _ := s.GetCoverSelection(releaseID)
	if got.Revision != selected.Revision || got.ImageID != selected.ImageID {
		t.Fatal("失败采用修改了旧选择")
	}
	for _, unsafe := range []string{"../outside.jpg", "/absolute.jpg", "a/../../b.jpg", `a\b.jpg`, "C:/outside.jpg"} {
		second.RelativePath = unsafe
		if _, err := s.SaveCoverFile(second); err == nil {
			t.Errorf("不安全路径可写入: %s", unsafe)
		}
	}
	second.ReleaseID = nil
	if _, err := s.AdoptCoverFile(second, 0); err == nil {
		t.Fatal("无歌集不应采用")
	}
}
