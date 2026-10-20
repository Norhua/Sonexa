package covers

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
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
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(path.Dir(original.RelativePath))))
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
	l.catalogKeys = func(r *store.Release) []store.CoverCatalogKey {
		return (&musicbrainz.Provider{}).ArtworkStrategy().CatalogKeys(*r.ProviderRef, "")
	}
	key := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: testReleaseMBID}
	if _, err := st.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "front", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "250"}}}}); err != nil {
		t.Fatal(err)
	}
	catalog, _ := st.GetCoverCatalog(key)
	if _, _, err := l.SaveVariant(0, catalog.Images[0].Variants[0].ID, pngBytes(250, 250), false, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkAutoCoverPreview(catalog.Images[0].Variants[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"track", "release"} {
		var n int
		if err := st.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("预览创建了 %s: %d %v", table, n, err)
		}
	}
	title, album, ref := "歌", "专辑", testReleaseMBID
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

func TestMissingTargetDoesNotCommitFile(t *testing.T) {
	l, _, _ := imageLibrary(t)
	if _, err := l.ImportLocal(123, pngBytes(20, 20), 0); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在歌集应失败: %v", err)
	}
}
