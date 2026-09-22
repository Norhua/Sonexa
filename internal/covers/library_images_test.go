package covers

import (
	"bytes"
	"database/sql"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func imageLibrary(t *testing.T) (*LibraryImages, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	root := filepath.Join(dir, "covers")
	l, err := NewLibraryImages(st, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, st, root
}

func TestLibraryImagesAdoptionAtomicAndRepeatable(t *testing.T) {
	l, st, root := imageLibrary(t)
	releaseID, err := st.FindOrCreateLocalRelease("专辑", nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := l.ImportLocal(releaseID, pngBytes(50, 40), 0)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := st.ListReleaseCoverFiles(releaseID)
	original := files[0]
	// 模拟提交引用失败，刚落盘的新文件必须回收，旧图片不受影响。
	if _, err := st.DB().Exec(`CREATE TRIGGER fail_selection BEFORE UPDATE ON release_cover_selection BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ImportLocal(releaseID, pngBytes(60, 60), first.Revision); err == nil {
		t.Fatal("故障提交不应成功")
	}
	got, _ := st.GetCoverSelection(releaseID)
	if got.Revision != first.Revision || got.ImageID != first.ImageID {
		t.Fatal("失败改写了旧选择")
	}
	data, err := l.Read(original)
	if err != nil || !bytes.Equal(data, pngBytes(50, 40)) {
		t.Fatal("旧图片丢失")
	}
	entries, err := os.ReadDir(filepath.Join(root, "albums", "release-1"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("失败留下文件: %v %v", entries, err)
	}
	if _, err := st.DB().Exec(`DROP TRIGGER fail_selection`); err != nil {
		t.Fatal(err)
	}
	second, err := l.ImportLocal(releaseID, pngBytes(50, 40), first.Revision)
	if err != nil || second.Revision != 2 {
		t.Fatalf("重复采用失败: %+v %v", second, err)
	}
	if _, err := l.ImportLocal(releaseID, pngBytes(50, 40), 0); !errors.Is(err, store.ErrCoverSelectionChanged) {
		t.Fatalf("旧版本应拒绝: %v", err)
	}
	if _, err := l.Read(original); err != nil {
		t.Fatal("失败的重复采用误删复用文件")
	}
}

func TestLibraryImagesReuseAfterCandidateAdmission(t *testing.T) {
	l, st, _ := imageLibrary(t)
	key := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "mb-release"}
	if _, err := st.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "front", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "250"}}}}); err != nil {
		t.Fatal(err)
	}
	catalog, _ := st.GetCoverCatalog(key)
	if _, _, err := l.SaveVariant(0, catalog.Images[0].Variants[0].ID, pngBytes(250, 250), false, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"track", "release"} {
		var n int
		if err := st.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("预览创建了 %s: %d %v", table, n, err)
		}
	}
	title, album, ref := "歌", "专辑", "mb-release"
	trackID, err := st.AdmitCandidate(provider.Candidate{Provider: provider.MusicBrainz, Title: &title, AlbumTitle: &album, ReleaseRef: &ref, MatchRef: "track"})
	if err != nil {
		t.Fatal(err)
	}
	track, _ := st.GetTrack(trackID)
	for range 2 {
		if err := l.ReuseReleasePreviews(*track.ReleaseID); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := st.ListReleaseCoverFiles(*track.ReleaseID)
	if len(files) != 1 || files[0].Protected || files[0].Width != 250 {
		t.Fatalf("没有复用目录和文件: %+v", files)
	}
	if chosen, err := st.GetCoverSelection(*track.ReleaseID); err != nil || chosen != nil {
		t.Fatal("自动复用伪造了用户采用")
	}
}

func TestLegacyCoverMigrationResumesWithoutDeletingOldFiles(t *testing.T) {
	l, st, root := imageLibrary(t)
	if err := os.MkdirAll(filepath.Join(root, "adopted"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "adopted", "hash.png")
	data := solidPNG(800, 600, color.RGBA{R: 80, A: 255})
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A", "B", "missing"} {
		id, err := st.FindOrCreateLocalRelease(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		file := legacy
		if name == "missing" {
			file += ".missing"
		}
		if err := st.SetReleaseCover(id, file, "https://is1.mzstatic.com/image.png"); err != nil {
			t.Fatal(err)
		}
		if _, err := st.DB().Exec(`INSERT INTO cover_legacy_migration(release_id,legacy_path) VALUES(?,?)`, id, file); err != nil {
			t.Fatal(err)
		}
	}
	if failures := l.MigrateLegacy(root); len(failures) != 1 {
		t.Fatalf("应仅报告缺失文件: %v", failures)
	}
	for _, id := range []int64{1, 2} {
		selected, err := st.GetCoverSelection(id)
		if err != nil || selected == nil {
			t.Fatalf("迁移缺少选择: %v", err)
		}
		files, _ := st.ListReleaseCoverFiles(id)
		if len(files) != 1 || !files[0].Protected || files[0].Width != 800 {
			t.Fatal("迁移丢失尺寸/保护状态")
		}
		key, _ := st.CoverCatalogKeyForImage(selected.ImageID)
		if key.Source != "itunes" {
			t.Fatal("来源不准确")
		}
	}
	// 模拟采用已提交、迁移完成标记还未写的中断。
	if _, err := st.DB().Exec(`UPDATE cover_legacy_migration SET state='pending' WHERE release_id=1`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy+".missing", data, 0o600); err != nil {
		t.Fatal(err)
	}
	if failures := l.MigrateLegacy(root); len(failures) != 0 {
		t.Fatalf("恢复失败: %v", failures)
	}
	selected, _ := st.GetCoverSelection(1)
	if selected.Revision != 1 {
		t.Fatal("重试重复采用")
	}
	if old, err := os.ReadFile(legacy); err != nil || !bytes.Equal(old, data) {
		t.Fatal("迁移删除/改写了旧文件")
	}
	if pending, err := st.PendingLegacyCovers(); err != nil || len(pending) != 0 {
		t.Fatalf("迁移未完成: %+v %v", pending, err)
	}
}

func TestMissingTargetDoesNotCommitFile(t *testing.T) {
	l, _, _ := imageLibrary(t)
	if _, err := l.ImportLocal(123, pngBytes(20, 20), 0); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在歌集应失败: %v", err)
	}
}
