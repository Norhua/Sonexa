package covers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/store"
)

func TestManualPreviewNeverBecomesAutomaticAlbumImage(t *testing.T) {
	l, st, _ := imageLibrary(t)
	key := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: testReleaseMBID}
	_, err := st.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "manual", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "250"}}}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := st.GetCoverCatalog(key)
	if _, _, err := l.SaveVariant(0, catalog.Images[0].Variants[0].ID, pngBytes(250, 250), false, 0); err != nil {
		t.Fatal(err)
	}
	source, ref := "musicbrainz", testReleaseMBID
	id, err := st.UpsertRelease(&store.Release{Provider: &source, ProviderRef: &ref, Title: "album", TracklistState: store.TracklistNone})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.ReuseReleasePreviews(id); err != nil {
		t.Fatal(err)
	}
	files, _ := st.ListReleaseCoverFiles(id)
	if len(files) != 0 {
		t.Fatal("手动浏览改变了歌集图片")
	}
}

func TestLibraryCacheClearTTLAndMoveRoot(t *testing.T) {
	l, st, root := imageLibrary(t)
	id, err := st.FindOrCreateLocalRelease("album", nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := l.ImportLocal(id, pngBytes(800, 800), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: testReleaseMBID}
	_, err = st.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "auto", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "250"}}}})
	if err != nil {
		t.Fatal(err)
	}
	cat, _ := st.GetCoverCatalog(key)
	file, _, err := l.SaveVariant(id, cat.Images[0].Variants[0].ID, pngBytes(250, 250), false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE cover_file SET fetched_at=? WHERE id=?`, time.Now().AddDate(-1, 0, 0), file.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.Configure(1<<20, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, file.RelativePath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("TTL 未删除临时图")
	}
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	cat, _ = st.GetCoverCatalog(key)
	if cat.State != "found" {
		t.Fatal("清缓存删除目录")
	}
	files, _ := st.ListReleaseCoverFiles(id)
	var protected store.CoverFile
	for _, f := range files {
		if f.Protected {
			protected = f
		}
	}
	if _, err := l.Read(protected); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(t.TempDir(), "new-covers")
	if err := l.MoveRoot(next, func() error { return os.ErrPermission }); !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	if _, err := l.Read(protected); err != nil {
		t.Fatal("失败切换破坏旧根")
	}
	if err := l.MoveRoot(next, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Read(protected); err != nil {
		t.Fatal("新根丢图")
	}
	got, _ := st.GetCoverSelection(id)
	if got.Revision != selected.Revision {
		t.Fatal("迁移改变用户选择")
	}
}

func TestLibraryCacheCapacityAppliesImmediately(t *testing.T) {
	l, st, _ := imageLibrary(t)
	id, _ := st.FindOrCreateLocalRelease("album", nil)
	key := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "r"}
	_, err := st.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "1", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "250"}, {Spec: "500"}}}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := st.GetCoverCatalog(key)
	for _, v := range catalog.Images[0].Variants {
		if _, _, err := l.SaveVariant(id, v.ID, pngBytes(int(v.ID)*10, 10), false, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Configure(1, 0); err != nil {
		t.Fatal(err)
	}
	files, _ := st.ListReleaseCoverFiles(id)
	for _, f := range files {
		if f.Available {
			t.Fatal("容量变更未生效")
		}
	}
}
