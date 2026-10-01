package covers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

func TestArtworkDraftCancelFailureMoveAndCommit(t *testing.T) {
	l, st, _ := imageLibrary(t)
	release, err := st.FindOrCreateLocalRelease("专辑", nil)
	if err != nil {
		t.Fatal(err)
	}
	old, err := l.ImportLocal(release, pngBytes(100, 100), 0)
	if err != nil {
		t.Fatal(err)
	}
	oldFiles, _ := st.ListReleaseCoverFiles(release)
	draft, err := l.StageArtwork(pngBytes(200, 200), 0)
	if err != nil {
		t.Fatal(err)
	}
	selection, _ := st.GetCoverSelection(release)
	if selection.Revision != old.Revision {
		t.Fatal("staging applied before save")
	}
	if err := l.DiscardArtwork(draft.Token); err != nil {
		t.Fatal(err)
	}
	if err := l.WithPreparedArtwork(draft.Token, release, func(store.CoverFile) error { return nil }); err == nil {
		t.Fatal("discarded draft still usable")
	}
	draft, err = l.StageArtwork(pngBytes(200, 200), 0)
	if err != nil {
		t.Fatal(err)
	}
	failedPath := ""
	if err := l.WithPreparedArtwork(draft.Token, release, func(file store.CoverFile) error {
		failedPath = file.RelativePath
		return errors.New("transaction failed")
	}); err == nil {
		t.Fatal("expected transaction failure")
	}
	if _, err := os.Stat(filepath.Join(l.files.root.Name(), failedPath)); !os.IsNotExist(err) {
		t.Fatal("failed final file retained", err)
	}
	if _, err := l.Read(oldFiles[0]); err != nil {
		t.Fatal("failure deleted old selection", err)
	}
	moved := t.TempDir()
	if err := l.MoveRoot(moved, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := l.WithPreparedArtwork(draft.Token, release, func(file store.CoverFile) error { _, err := st.AdoptCoverFile(file, old.Revision); return err }); err != nil {
		t.Fatal(err)
	}
	selection, _ = st.GetCoverSelection(release)
	if selection.Revision != old.Revision+1 {
		t.Fatal("draft not adopted")
	}
	files, err := st.ListReleaseCoverFiles(release)
	if err != nil {
		t.Fatal(err)
	}
	var selected *store.CoverFile
	for i := range files {
		if files[i].VariantID == selection.VariantID {
			selected = &files[i]
		}
	}
	if selected == nil || selected.Width != 200 {
		t.Fatal("wrong draft adopted")
	}
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Read(*selected); err != nil {
		t.Fatal("cache clear deleted draft adoption", err)
	}
}

func TestArtworkDraftDatabaseFailurePreservesOldAndRollsBackLocalIdentity(t *testing.T) {
	l, st, _ := imageLibrary(t)
	release, err := st.FindOrCreateLocalRelease("专辑", nil)
	if err != nil {
		t.Fatal(err)
	}
	old, err := l.ImportLocal(release, pngBytes(100, 100), 0)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := l.StageArtwork(pngBytes(300, 300), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec("CREATE TRIGGER reject_adopt BEFORE UPDATE ON release_cover_selection BEGIN SELECT RAISE(ABORT,'failed commit'); END"); err != nil {
		t.Fatal(err)
	}
	if err := l.WithPreparedArtwork(draft.Token, release, func(file store.CoverFile) error { _, err := st.AdoptCoverFile(file, old.Revision); return err }); err == nil {
		t.Fatal("commit failure accepted")
	}
	var n int
	if err := st.DB().QueryRow("SELECT count(*) FROM cover_catalog WHERE source='local'").Scan(&n); err != nil || n != 1 {
		t.Fatal("local catalog escaped failed transaction", n, err)
	}
	selection, err := st.GetCoverSelection(release)
	if err != nil || selection.VariantID != old.VariantID {
		t.Fatal("old selection changed", err)
	}
}
