package lyrics

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestTextSharedFixturesAndRoundTrip(t *testing.T) {
	body, err := os.ReadFile("../../testdata/lyrics/text.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Input, Text, Format string
		Rows                      []string
	}
	if err := json.Unmarshal(body, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			d, err := ParseText(f.Input)
			if err != nil {
				t.Fatal(err)
			}
			if d.Text != f.Text || d.Format != f.Format {
				t.Fatalf("%+v", d)
			}
			rows := []string{}
			for _, r := range d.Rows {
				rows = append(rows, r.Text)
			}
			if !reflect.DeepEqual(rows, f.Rows) {
				t.Fatalf("rows %q != %q", rows, f.Rows)
			}
			rendered, err := SerializeTrack(d.Original.Track)
			if err != nil {
				t.Fatal(err)
			}
			again, _, err := parseTrack(rendered)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.Original.Track, again) {
				t.Fatalf("round trip changed track:\n%+v\n%+v\n%s", d.Original.Track, again, rendered)
			}
		})
	}
}

func TestWordEndsAndWhitespace(t *testing.T) {
	for _, tt := range []struct {
		input string
		end   *int64
	}{
		{"[00:01]<00:01>A <00:01.5>B", nil},
		{"[00:01]<00:01>A <00:01.2><00:01.5>B", ms(1200)},
	} {
		d, err := ParseText(tt.input)
		if err != nil {
			t.Fatal(err)
		}
		l := d.Original.Track.Lines[0]
		if l.Text != "A B" || len(l.Parts) != 2 || !reflect.DeepEqual(l.Parts[0].EndMS, tt.end) || l.Parts[1].EndMS != nil {
			t.Fatalf("%+v", l)
		}
	}
	for _, text := range []string{"[00:01]<00:01><00:02>", "[ti:empty]\n[00:01]"} {
		if _, err := ParseText(text); !errors.Is(err, ErrNoLyrics) {
			t.Fatal(err)
		}
	}
	for _, text := range []string{"[00:01]<00:01> \n[00:02]body", "[00:01]body<00:02>"} {
		d, err := ParseText(text)
		if err != nil || d.WordTiming == "available" || d.Format != "lrc" {
			t.Fatal(d, err)
		}
	}
}

func TestStructuredAndPreviewLimits(t *testing.T) {
	for _, payload := range []*Payload{
		{Original: &Channel{Status: "available", Track: &Track{Lines: []Line{{StartMS: ms(-1), Text: "bad"}}}}},
		{Original: &Channel{Status: "available", Track: &Track{Lines: []Line{{Text: "wrong", Parts: []Word{{Text: "mismatch"}}}}}}},
	} {
		if _, err := DocumentFromPayload(payload); !errors.Is(err, ErrFormat) {
			t.Fatal(err)
		}
	}
	large := strings.Repeat("x", MaxTextBytes/2)
	if _, err := DocumentFromPayload(&Payload{Text: large, Translation: TextChannel("y"+large, nil)}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := ParseText(strings.Repeat("\n", MaxTextBytes/96)); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	// Both individual tracks fit, but the final reading projection does not.
	lines := strings.Repeat("a\n", MaxProjectionBytes/128)
	if _, err := ParseText(lines); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}

func TestTimeBoundsAndMalformedTags(t *testing.T) {
	for _, text := range []string{"[999999999999999999999:01]body", "[1440:00.001]body", "[00:99]body", "[00:01]<00:99>body", "[00:01]<00:02>A<00:01>B"} {
		if _, err := ParseText(text); !errors.Is(err, ErrFormat) {
			t.Fatalf("%s: %v", text, err)
		}
	}
	if _, err := ParseText("[1440:00.000]last valid time"); err != nil {
		t.Fatal(err)
	}
}
