package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSubmitImportKeepsMissingBasicFieldsAndRawRows(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.runner = nil
	path := filepath.Join(t.TempDir(), "missing.csv")
	data := "曲名,艺术家,专辑,年份,碟号\n, 艺术家 ,,,\n,,专辑,,\n,,,2026,\n,,,,2\n,,,,\n\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := a.RemapImportColumns(path, "", []string{"title", "artist", "album", "year", "disc_no"}, "UTF-8", ",", true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Preview.ImportableRows != 4 || preview.Preview.SkippedRows != 2 || preview.Preview.MissingTitleRows != 4 {
		t.Fatalf("preview=%+v", preview.Preview)
	}
	batch, err := a.SubmitImport(path, "", preview.ColumnMap, "UTF-8", ",", true)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := a.store.Stats(batch)
	if err != nil || stats.Total != 4 {
		t.Fatalf("stats=%+v %v", stats, err)
	}
	for i := 0; i < 4; i++ {
		item, err := a.store.ClaimNextItem(batch)
		if err != nil || item == nil {
			t.Fatalf("item=%+v %v", item, err)
		}
		var raw []string
		if err := json.Unmarshal([]byte(item.RawRow), &raw); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(raw, preview.Preview.Rows[i].Raw) {
			t.Fatalf("raw=%q", raw)
		}
		if i == 3 && (item.InDiscNo == nil || *item.InDiscNo != 2) {
			t.Fatalf("disc lost: %+v", item)
		}
	}
}
