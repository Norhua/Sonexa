package lyricdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func precisionLinks(ch *Channel) []LineAssociation {
	for _, d := range ch.Diagnostics {
		if d.Code == "time_precision_alignment" && d.Inference != nil {
			return d.Inference.Associations
		}
	}
	return nil
}

func TestPrecisionAlignmentBoundaries(t *testing.T) {
	for _, delta := range []int64{-11, -10, -1, 0, 1, 10, 11} {
		t.Run(fmt.Sprint(delta), func(t *testing.T) {
			before := 1000 + delta
			d, err := Build(&Input{Text: "[00:01]Original", Translation: TextChannel("["+clock(before)+"]Translation", nil), Romanization: TextChannel("["+clock(before)+"]Roma", nil)})
			if err != nil {
				t.Fatal(err)
			}
			want := before
			if delta >= -10 && delta <= 10 {
				want = 1000
				if len(d.Rows) != 1 || len(d.Rows[0].Translation) != 1 || len(d.Rows[0].Romanization) != 1 {
					t.Fatalf("not associated: %+v", d.Rows)
				}
			} else if len(d.Rows) != 3 {
				t.Fatalf("out-of-window time changed: %+v", d.Rows)
			}
			for _, ch := range []*Channel{d.Translation, d.Romanization} {
				if *ch.Track.Lines[0].StartMS != want || !strings.Contains(ch.Text, "["+clock(want)+"]"+ch.Track.Lines[0].Text) {
					t.Fatal("text/structure disagree", d.Text)
				}
				if before != want && len(precisionLinks(ch)) != 1 {
					t.Fatal("missing diagnostic")
				}
			}
		})
	}
}

func TestPrecisionAlignmentRejectsAmbiguousAndCrossingClaims(t *testing.T) {
	for _, tc := range []struct {
		name, original, translation string
		want                        []int64
		links                       int
	}{
		{"same-time originals", "[00:01]A\n[00:01]B", "[00:00.995]T", []int64{995}, 0},
		{"two nearby originals", "[00:01]A\n[00:01.009]B", "[00:01.004]T", []int64{1004}, 0},
		{"two translations claim one", "[00:01]A", "[00:00.995]T1\n[00:01.005]T2", []int64{995, 1005}, 0},
		{"exact match wins", "[00:01]A", "[00:01]T1\n[00:01.005]T2", []int64{1000, 1005}, 0},
		{"exact with nearby original", "[00:01]A\n[00:01.005]B", "[00:01]T", []int64{1000}, 0},
		{"ambiguous claimant blocks unique claimant", "[00:01]A\n[00:01.015]B", "[00:00.995]T1\n[00:01.007]T2", []int64{995, 1007}, 0},
		{"multiple exact lines retained", "[00:01]A", "[00:01]T1\n[00:01]T2", []int64{1000, 1000}, 0},
		{"must not cross empty timed line forward", "[00:01]A", "[00:00.995]T\n[00:00.998]", []int64{995, 998}, 0},
		{"must not cross empty timed line backward", "[00:01]A", "[00:01.002]\n[00:01.005]T", []int64{1002, 1005}, 0},
		{"canonical order and repeated chorus", "[00:02]Chorus\n[00:01]Chorus", "[00:02.005]T2\n[00:00.995]T1", []int64{1000, 2000}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Build(&Input{Text: tc.original, Translation: TextChannel(tc.translation, nil)})
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range d.Translation.Track.Lines {
				if *line.StartMS != tc.want[i] {
					t.Fatalf("line %d got %d want %d", i, *line.StartMS, tc.want[i])
				}
			}
			if len(precisionLinks(d.Translation)) != tc.links {
				t.Fatalf("unexpected links: %+v", precisionLinks(d.Translation))
			}
			if err := Validate(d); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPrecisionAlignmentLeavesUnknownAndUntimedContent(t *testing.T) {
	d, err := Build(&Input{Text: "[00:01]Original", Translation: TextChannel("untimed\n[00:00.995]   ", nil), Romanization: &Channel{Status: "unknown", Origin: "unknown", Text: "[00:00.995]Unknown"}, Additional: []*Channel{{Status: "unknown", Origin: "embedded", Text: "[00:00.995]Additional"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(precisionLinks(d.Translation)) != 0 || len(precisionLinks(d.Romanization)) != 0 || *d.Additional[0].Track.Lines[0].StartMS != 995 || *d.Romanization.Track.Lines[0].StartMS != 995 {
		t.Fatal("unknown/empty content inferred")
	}
	if !strings.Contains(d.Text, "untimed") || !strings.Contains(d.Text, "   ") {
		t.Fatal("content dropped", d.Text)
	}
}

func TestPrecisionAlignmentPreservesWordsAndInputOwnership(t *testing.T) {
	input := &Input{Text: "[00:01.005]<00:01.005>Original <00:02>line", Translation: TextChannel("[00:01]<00:01>Translated <00:02>line<00:02.5>", nil)}
	before, _ := json.Marshal(input)
	d, err := Build(input)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("builder mutated input")
	}
	if !strings.Contains(d.Text, "[>und]<00:01.000>Translated <00:02.000>line<00:02.500>") || !reflect.DeepEqual(d.Translation.Track.Lines[0].Parts, input.Translation.Track.Lines[0].Parts) {
		t.Fatal("word times changed", d.Text)
	}
	if err := matchingTrackText(d.Translation.Track, TextChannel(d.Translation.Text, nil).Track); err != nil {
		t.Fatal(err)
	}
	*input.Translation.Track.Lines[0].StartMS = 9
	if err := Validate(d); err != nil {
		t.Fatal("retained input pointer", err)
	}
	precisionLinks(d.Translation)[0].SourceTimeMS++
	if err := Validate(d); !errors.Is(err, ErrContract) {
		t.Fatal("modified precision diagnosis accepted", err)
	}
	// A real line end cannot precede its newly normalized start.
	track := &Track{Lines: []Line{{StartMS: ms(1000), EndMS: ms(1002), Text: "Translation"}}}
	d, err = Build(&Input{Text: "[00:01.005]Original", Translation: &Channel{Status: "available", Track: track}})
	if err != nil || *d.Translation.Track.Lines[0].StartMS != 1000 || *d.Translation.Track.Lines[0].EndMS != 1002 {
		t.Fatal("invalid line interval introduced", d, err)
	}
}

func TestPrecisionAlignmentOffsetsAndRebuildDoNotDrift(t *testing.T) {
	for _, tc := range []struct {
		name, original, translation  string
		wantOffset, wantTime, before int64
	}{
		{"different offsets", "[offset:500]\n[00:01.005]Original", "[offset:-500]\n[00:00]Translation", 0, 505, 500},
		{"same offset", "[offset:500]\n[00:01.005]Original", "[offset:500]\n[00:01]Translation", 500, 1005, 1000},
		{"negative actual", "[offset:1500]\n[00:01.005]Original", "[offset:500]\n[00:00]Translation", 500, 5, 0},
		{"zero boundary moves backward", "[offset:1005]\n[00:01]Original", "[00:00]Translation", 5, 0, 5},
		{"negative offsets", "[offset:-500]\n[00:01.005]Original", "[00:01.5]Translation", 0, 1505, 1500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Build(&Input{Text: tc.original, Translation: TextChannel(tc.translation, nil)})
			if err != nil {
				t.Fatal(err)
			}
			if d.OffsetMS != tc.wantOffset || len(d.Rows) != 1 || *d.Rows[0].TimeMS != tc.wantTime {
				t.Fatal("offset mismatch", d)
			}
			for _, ch := range []*Channel{d.Original, d.Translation} {
				if ch.Track.OffsetMS != tc.wantOffset || *ch.Track.Lines[0].StartMS != tc.wantTime {
					t.Fatal("channel axis mismatch", ch)
				}
			}
			links := precisionLinks(d.Translation)
			if len(links) != 1 || links[0].SourceTimeMS != tc.before || links[0].AssociatedTimeMS != tc.wantTime {
				t.Fatal("diagnostic axis mismatch", links)
			}
			rebuilt, err := Build(&Input{Original: d.Original, Translation: d.Translation, Romanization: d.Romanization, WordTiming: d.WordTiming})
			if err != nil || !reflect.DeepEqual(rebuilt, d) {
				t.Fatal("rebuild changed document", err)
			}
			parsed, err := ParseText(d.Text)
			if err != nil || *parsed.Rows[0].TimeMS-parsed.OffsetMS != tc.wantTime-tc.wantOffset {
				t.Fatal("saved text drift", err)
			}
		})
	}
}

func TestPrecisionAlignmentLargeInputAndBoundedDiagnostics(t *testing.T) {
	var original, translation strings.Builder
	for i := int64(1); i <= 4000; i++ {
		fmt.Fprintf(&original, "[%s]Original\n", clock(i*100))
		fmt.Fprintf(&translation, "[%s]Translation\n", clock(i*100-5))
	}
	d, err := Build(&Input{Text: original.String(), Translation: TextChannel(translation.String(), nil)})
	if err != nil || len(precisionLinks(d.Translation)) != 4000 {
		t.Fatal("bounded input failed", err)
	}
	ch := TextChannel("[00:00.995]Translation", nil)
	ch.Diagnostics = []Diagnostic{{Code: "large", Message: strings.Repeat("x", MaxProjectionBytes-64-len("large"))}}
	if _, err := Build(&Input{Text: "[00:01]Original", Translation: ch}); !errors.Is(err, ErrTooLarge) {
		t.Fatal("alignment must account for existing diagnostics", err)
	}
}
