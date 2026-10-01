package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/export"
	"github.com/norhua/sonexa/internal/store"
)

func TestExportAndSearchReadCurrentWithoutLyrics(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	year := 2014
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "", Credit: strPtrOrNil("旧署名"), Album: strPtrOrNil("专辑"), Metadata: &store.MetadataInput{Year: &year}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.ApplyMetadataEdits(id, map[string]string{"credit": "新署名", "year": "2012"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	draft, err := app.store.StageLyrics(context.Background(), "ONLYLYRICTEXT")
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := app.store.TrackRevision(id)
	if err := app.store.SaveLyrics(context.Background(), id, &draft, rev); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"旧署名", "ONLYLYRICTEXT"} {
		hits, err := app.store.SearchTrackIDs(query, 10)
		if err != nil || len(hits) != 0 {
			t.Fatalf("stale/lyrics indexed: %s %v %v", query, hits, err)
		}
	}
	hits, err := app.store.SearchTrackIDs("新署名", 10)
	if err != nil || len(hits) != 1 || hits[0] != id {
		t.Fatal("current credit not indexed", hits, err)
	}
	tracks, err := app.resolveExportScope(ExportRequest{Scope: "all"})
	if err != nil || len(tracks) != 1 {
		t.Fatal(err)
	}
	if tracks[0].Title != "" || tracks[0].Year == nil || *tracks[0].Year != 2012 || tracks[0].Artist == nil || *tracks[0].Artist != "新署名" {
		t.Fatalf("wrong current export: %+v", tracks)
	}
	var data bytes.Buffer
	if err := export.Write(&data, tracks, export.Options{Format: export.FormatJSON, Depth: export.DepthFull}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(data.String(), "ONLYLYRICTEXT") || strings.Contains(strings.ToLower(data.String()), "lyrics") {
		t.Fatal("lyrics exported", data.String())
	}
}
