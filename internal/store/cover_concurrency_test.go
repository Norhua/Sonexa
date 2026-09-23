package store

import (
	"testing"
	"time"
)

// 目录写事务已持有写锁时，预览登记必须等待它提交，而不是先建立读快照，
// 再在升级写锁时立即 SQLITE_BUSY。此竞争无需真实 HTTP 或随机调度重现。
func TestSaveCoverFileWaitsForConcurrentCatalogWriter(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "insert"
		if existing {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			s := openTestStore(t)
			catalog := mustSaveCatalog(t, s, CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "concurrent"}, sampleCoverImages())
			file := sampleCoverFile(catalog.Images[0].Variants[0].ID, 0, "cache/sources/caa-test/Front-caa-250.jpg")
			file.ReleaseID = nil
			if existing {
				if _, err := s.SaveCoverFile(file); err != nil {
					t.Fatal(err)
				}
			}
			writer, err := s.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback()
			if _, err := writer.Exec(`UPDATE cover_catalog SET version=version+1 WHERE id=?`, catalog.ID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := s.SaveCoverFile(file); done <- err }()
			select {
			case err := <-done:
				t.Fatalf("目录事务提交前登记已返回，应等待写锁: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			if err := writer.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("目录提交后预览登记仍未完成")
			}
			files, err := s.PreviewFilesForVariant(file.VariantID)
			if err != nil || len(files) != 1 || !files[0].Available {
				t.Fatal(files, err)
			}
		})
	}
}

func TestSaveCoverFileRollbackRestoresVariantAfterFileConflict(t *testing.T) {
	s := openTestStore(t)
	catalog := mustSaveCatalog(t, s, CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "rollback"}, sampleCoverImages())
	first := sampleCoverFile(catalog.Images[0].Variants[0].ID, 0, "cache/sources/caa-test/Front-caa-250.jpg")
	first.ReleaseID = nil
	if _, err := s.SaveCoverFile(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.VariantID = catalog.Images[1].Variants[0].ID
	second.Width, second.Height = 600, 400
	if _, err := s.SaveCoverFile(second); err == nil {
		t.Fatal("不同图片不应覆盖相同路径")
	}
	variant, err := s.GetCoverVariant(second.VariantID)
	if err != nil || variant.State != "unverified" || variant.Width != nil || variant.Height != nil {
		t.Fatalf("失败文件写入留下虚假的规格验证: %+v %v", variant, err)
	}
	files, err := s.PreviewFilesForVariant(first.VariantID)
	if err != nil || len(files) != 1 || files[0].Width != 250 {
		t.Fatal(files, err)
	}
}
