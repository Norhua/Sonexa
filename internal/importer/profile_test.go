package importer

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestDurationProfileUnitsAndPrecision(t *testing.T) {
	for _, tt := range []struct {
		raw, header      string
		value, precision int64
	}{
		{"10:20", "", 620000, 1000}, {"620 s", "", 620000, 1000},
		{"620000 ms", "", 620000, 1}, {"620", "时长(秒)", 620000, 1000},
		{"620000", "Duration (ms)", 620000, 1}, {"620000", "duration_ms", 620000, 1},
		{"10:20.125", "", 620125, 1}, {"620.1 秒", "", 620100, 100},
		{"1:02:03", "", 3723000, 1000},
	} {
		t.Run(tt.raw+tt.header, func(t *testing.T) {
			got := ParseDurationEvidence(tt.raw, tt.header)
			if got.State != matching.ValueKnown || got.Value != tt.value || got.Precision != tt.precision || got.Raw != tt.raw {
				t.Fatalf("unexpected duration %+v", got)
			}
		})
	}
	for _, raw := range []string{"620", "620000", "10:60", "1:60:00", "-2 s", "NaN s", "1 day", "0 ms", "999999999999999999999 s"} {
		if got := ParseDurationEvidence(raw, ""); got.State != matching.ValueInvalid || got.Error == "" {
			t.Fatalf("invalid duration accepted: %+v", got)
		}
	}
	if got := ParseDurationEvidence("", ""); got.State != matching.ValueMissing {
		t.Fatal(got)
	}
}

func TestMappedProfilePreservedThroughQueueAndRunner(t *testing.T) {
	st := openStore(t)
	res := &ParseResult{
		Headers:   []string{"Title", "Artist", "Album", "Year", "Track No", "Disc No", "Duration (ms)"},
		ColumnMap: []Field{FieldTitle, FieldArtist, FieldAlbum, FieldYear, FieldTrackNo, FieldDiscNo, FieldDuration},
		Rows:      [][]string{{" Ｓｏｎｇ ", "Artist", "Album", "2020-02-29", "3/12", "", "620000"}},
	}
	row := AllRows(res)[0]
	if row.SkipReason != "" || row.DiscNo != nil || row.Year == nil || *row.Year != 2020 {
		t.Fatalf("bad row %+v", row)
	}
	if row.Profile.Title.Raw != res.Rows[0][0] || !reflect.DeepEqual(row.Raw, res.Rows[0]) {
		t.Fatal("raw text changed")
	}
	data, err := json.Marshal(row.Raw)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := st.CreateBatch(&store.ImportBatch{Filename: "profile.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: string(data), Profile: &row.Profile,
		InTitle: &row.Title, InArtist: row.Artist, InAlbum: row.Album, InYear: row.Year, InTrackNo: row.TrackNo, InDurationMS: row.DurationMS}}); err != nil {
		t.Fatal(err)
	}
	item, err := st.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	in := inputFromItem(item)
	if !reflect.DeepEqual(in.Profile, &row.Profile) || in.Profile.Disc.State != matching.ValueMissing || in.Profile.Track.Value != 3 {
		t.Fatalf("Runner lost evidence: %+v", in.Profile)
	}
	searcher := &recordingSearcher{}
	r := newRunner(st, searcher)
	r.processItem(context.Background(), item)
	saved, err := st.GetItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RawRow != string(data) || !reflect.DeepEqual(saved.Profile, &row.Profile) {
		t.Fatal("matching rewrote input")
	}
	if len(searcher.queries) == 0 || searcher.queries[0].DurationMS == nil || *searcher.queries[0].DurationMS != 620000 {
		t.Fatal("Runner did not use canonical duration")
	}
}

func TestInvalidMappedNumbersRemainInputEvidence(t *testing.T) {
	row := mapRow(0, []string{"2020 garbage", "0", "-2", "620"}, []Field{FieldYear, FieldTrackNo, FieldDiscNo, FieldDuration})
	if row.SkipReason != "" {
		t.Fatal("mapped parsing failures must remain reviewable")
	}
	for _, n := range []matching.NumberInput{row.Profile.Year, row.Profile.Track, row.Profile.Disc, row.Profile.Duration} {
		if n.State != matching.ValueInvalid || n.Raw == "" || n.Error == "" {
			t.Fatalf("lost parse failure: %+v", n)
		}
	}
	for _, raw := range []string{"2/1", "0/2", "-1", "1.5", "A1"} {
		if got := ParsePositionEvidence(raw, "track"); got.State != matching.ValueInvalid {
			t.Fatal(got)
		}
	}
	if got := ParseYearEvidence("2020-02-30"); got.State != matching.ValueInvalid {
		t.Fatal(got)
	}
	blank := mapRow(0, []string{"Song", "  ", " \t"}, []Field{FieldTitle, FieldArtist, FieldDiscNo})
	if blank.Artist != nil || blank.DiscNo != nil || blank.Profile.Artist.Text.Raw != "  " || blank.Profile.Disc.Raw != " \t" || blank.Profile.Disc.State != matching.ValueMissing {
		t.Fatalf("blank original cells lost: %+v", blank)
	}
}
