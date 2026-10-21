package lyricdoc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMergeNeverDropsUntimedExtras(t *testing.T) {
	for _, original := range []string{"plain original", "[00:01]original", "[00:01]<00:01>original<00:02>"} {
		doc, err := channelDocument(TextChannel(original, nil), TextChannel("plain translation", nil), TextChannel("[00:01]roma\nuntimed tail", nil), "unavailable", nil)
		if err != nil || !strings.Contains(doc.Text, "plain translation") || !strings.Contains(doc.Text, "untimed tail") || !strings.Contains(doc.Text, "roma") {
			t.Fatalf("%+v %v", doc, err)
		}
	}
	got, err := mergeTimed("[00:01]original\n[00:02]two", "[00:02]second\n[00:01]first")
	if err != nil {
		t.Fatal(err)
	}
	if got != "[00:01.000]original\n[00:01.000]first\n[00:02.000]two\n[00:02.000]second" {
		t.Fatal(got)
	}
}
func TestConservativeAlignmentAndAdditional(t *testing.T) {
	p := &Input{
		Original:    TextChannel("[00:02]B\n[00:01]A\n[00:01]other\n[00:10.1][00:20.100]chorus\n[00:10.100]chorus", nil),
		Translation: TextChannel("[00:20.1]repeat\n[00:01]ambiguous\n[00:02]second\n[00:03]own time\nuntimed", nil),
		Additional:  []*Channel{{Status: "unknown", Origin: "embedded", Text: "unclassified"}},
	}
	d, err := Build(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(d.Text, "[00:10.100]chorus") != 1 || !strings.Contains(d.Text, "[00:20.100]chorus") {
		t.Fatal(d.Text)
	}
	found := map[string]PreviewRow{}
	for _, r := range d.Rows {
		found[r.Text] = r
	}
	if len(found["A"].Translation) != 0 || found["ambiguous"].Channel != "translation" || len(found["B"].Translation) != 1 || found["B"].Translation[0] != "second" {
		t.Fatal(d.Rows)
	}
	if *found["own time"].TimeMS != 3000 || found["untimed"].TimeMS != nil || found["unclassified"].Channel != "additional" || d.Additional[0].Status != "unknown" {
		t.Fatal(d.Rows)
	}
	if d.Translation.SyncStatus != "partial" || len(d.Translation.Diagnostics) == 0 {
		t.Fatal(d.Translation)
	}
}

// Characterizes the current exact-time policy, not a tolerance-based fix.
// A centisecond translation can precede the millisecond original after sorting.
func TestExactAlignmentKeepsSubCentisecondDifferences(t *testing.T) {
	for _, tc := range []struct {
		name        string
		original    string
		translation string
		want        int64
		matched     bool
	}{
		{"exact", "[00:42.743]<00:42.743>Original <00:43.100>line", "[00:42.743]Translation", 42743, true},
		{"three milliseconds", "[00:42.743]<00:42.743>Original <00:43.100>line", "[00:42.74]Translation", 42740, false},
		{"six milliseconds", "[00:47.856]<00:47.856>Original <00:48.100>line", "[00:47.85]Translation", 47850, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Build(&Input{Original: TextChannel(tc.original, nil), Translation: TextChannel(tc.translation, nil)})
			if err != nil {
				t.Fatal(err)
			}
			if tc.matched {
				if len(d.Rows) != 1 || len(d.Rows[0].Translation) != 1 || d.Rows[0].Translation[0] != "Translation" {
					t.Fatalf("expected exact association: %+v", d.Rows)
				}
			} else {
				if len(d.Rows) != 2 || d.Rows[0].Channel != "translation" || *d.Rows[0].TimeMS != tc.want || d.Rows[1].Channel != "original" || len(d.Rows[1].Translation) != 0 || *d.Rows[0].TimeMS >= *d.Rows[1].TimeMS {
					t.Fatalf("expected independent earlier translation: %+v", d.Rows)
				}
				if len(d.Translation.Diagnostics) != 1 || d.Translation.Diagnostics[0].Code != "unaligned" {
					t.Fatalf("missing alignment diagnostic: %+v", d.Translation.Diagnostics)
				}
			}
			if !strings.Contains(d.Text, tc.original) || !strings.Contains(d.Text, "["+clock(tc.want)+"]Translation") || *d.Translation.Track.Lines[0].StartMS != tc.want {
				t.Fatalf("normalization changed original timing: %q", d.Text)
			}
		})
	}
}

func TestOneOriginalAndDistinctWordTimelines(t *testing.T) {
	p := &Input{Original: TextChannel("[00:01]line version", nil), WordOriginal: TextChannel("[00:02]<00:02>word <00:02.5>version", nil), Translation: TextChannel("[00:01]translation", nil)}
	d, err := Build(p)
	if err != nil || strings.Contains(d.Text, "line version") || d.Rows[0].Channel != "translation" || len(d.Rows[1].Translation) != 0 {
		t.Fatal(d, err)
	}
	p = &Input{Original: TextChannel("[00:01]same\n[00:01]<00:01>same<00:02>\n[00:01]<00:01.5>same<00:02>", nil), Translation: TextChannel("plain extra", nil)}
	d, err = Build(p)
	if err != nil || strings.Count(d.Text, "same") != 2 || strings.Contains(d.Text, "[00:01.000]same\n") {
		t.Fatal(d, err)
	}
}

func TestIndependentOffsetsAppliedOnce(t *testing.T) {
	for _, tt := range []struct {
		name, original, translation string
		wantOffset, wantTime        int64
	}{
		{"positive", "[offset:500]\n[00:01]A", "[offset:-500]\n[00:00]B", 0, 500},
		{"negative actual", "[offset:1500]\n[00:01]A", "[offset:500]\n[00:00]B", 500, 0},
		{"same", "[offset:1000]\n[00:01]A", "[offset:1000]\n[00:01]B", 1000, 1000},
		{"negative", "[offset:-500]\n[00:01]A", "[00:01.5]B", 0, 1500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Build(&Input{Original: TextChannel(tt.original, nil), Translation: TextChannel(tt.translation, nil)})
			if err != nil {
				t.Fatal(err)
			}
			if d.OffsetMS != tt.wantOffset || len(d.Rows) != 1 || *d.Rows[0].TimeMS != tt.wantTime || len(d.Rows[0].Translation) != 1 {
				t.Fatalf("%+v rows=%+v", d, d.Rows)
			}
			round, err := ParseText(d.Text)
			if err != nil || round.OffsetMS != d.OffsetMS || *round.Rows[0].TimeMS-round.OffsetMS != tt.wantTime-tt.wantOffset {
				t.Fatal(round, err)
			}
			if strings.Count(d.Text, "[offset:") > 1 {
				t.Fatal(d.Text)
			}
		})
	}
}

func TestConflictingOffsetsDiagnosedAndSingleTextPreserved(t *testing.T) {
	for _, text := range []string{"[offset:500]\n[00:01]A", "[offset:500]\n[offset:-1]\n[00:01]A", "[offset:bad]\n[00:01]A"} {
		d, err := ParseText(text)
		if err != nil || d.Text != text {
			t.Fatal(d, err)
		}
		if strings.Contains(text, "bad") || strings.Contains(text, "-1") {
			if len(d.Warnings) == 0 {
				t.Fatal("ambiguous offset not diagnosed")
			}
			if _, err := Build(&Input{Text: text, Translation: TextChannel("[00:01]B", nil)}); err == nil {
				t.Fatal("ambiguous offsets merged")
			}
		}
	}
}

func TestTimestampExpansionIsBounded(t *testing.T) {
	var timestamps strings.Builder
	for i := 0; i < 1000; i++ {
		timestamps.WriteString(fmt.Sprintf("[%02d:%02d.000]", i/60, i%60))
	}
	if _, err := mergeTimed(timestamps.String()+strings.Repeat("a", 10000), "translation"); !errors.Is(err, ErrTooLarge) {
		t.Fatal("unbounded timestamp expansion")
	}
}
