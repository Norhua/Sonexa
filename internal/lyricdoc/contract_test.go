package lyricdoc

import (
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestCompletedDocumentContract(t *testing.T) {
	for _, input := range []*Input{
		{Text: "plain text"},
		{Text: "[00:01]<00:01>Hello <00:02>world<00:03>", Translation: TextChannel("译文", nil)},
		{Text: "[00:01]original", Translation: FailedChannel(errors.New("bad translation")), Romanization: AbsentChannel()},
		{Text: "[offset:500]\n[00:01]original", Translation: TextChannel("[offset:-500]\n[00:00]译文", nil)},
		{Text: "original", Additional: []*Channel{{Status: "unknown", Origin: "embedded", Text: "unclassified"}}},
	} {
		d, err := Build(input)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(d)
		if err := Validate(d); err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(d)
		if string(before) != string(after) {
			t.Fatal("validation changed document")
		}
		if reflect.TypeOf(*d).Field(0).Name == "Version" {
			t.Fatal("business version in document")
		}
		for _, name := range []string{"Version", "PreviewedVersion", "TotalScore"} {
			if _, ok := reflect.TypeOf(*d).FieldByName(name); ok {
				t.Fatalf("business field %s", name)
			}
		}
	}
}

func TestIncompleteOrChangedDocumentIsRejectedWithoutRepair(t *testing.T) {
	for name, change := range map[string]func(*Document){
		"text":             func(d *Document) { d.Text += " changed" },
		"track":            func(d *Document) { d.Original.Track.Lines[0].Text = "changed" },
		"time":             func(d *Document) { *d.Original.Track.Lines[0].StartMS = -1 },
		"rows":             func(d *Document) { d.Rows[0].Text = "changed" },
		"format":           func(d *Document) { d.Format = "text" },
		"missing original": func(d *Document) { d.Original = nil },
		"missing status":   func(d *Document) { d.Translation = nil },
		"false presence":   func(d *Document) { d.Translation.Status = "available" },
		"pending":          func(d *Document) { d.Romanization.Status = "pending" },
		"diagnostic":       func(d *Document) { d.Warnings = append(d.Warnings, "changed") },
		"offset":           func(d *Document) { d.OffsetMS++ },
		"oversize":         func(d *Document) { d.Text = strings.Repeat("x", MaxTextBytes+1) },
	} {
		t.Run(name, func(t *testing.T) {
			d, err := Build(&Input{Text: "[00:01]original", Translation: AbsentChannel(), Romanization: UnknownChannel()})
			if err != nil {
				t.Fatal(err)
			}
			change(d)
			before, _ := json.Marshal(d)
			if err := Validate(d); err == nil {
				t.Fatal("invalid document accepted")
			}
			after, _ := json.Marshal(d)
			if string(before) != string(after) {
				t.Fatal("invalid document repaired")
			}
		})
	}
	for _, d := range []*Document{nil, {}, {Text: "text"}, {Text: "text", Original: TextChannel("text", nil)}} {
		if err := Validate(d); err == nil {
			t.Fatal("half-built result accepted")
		}
	}
}

func TestValidOriginalAndOptionalStates(t *testing.T) {
	for _, ch := range []*Channel{nil, AbsentChannel(), UnknownChannel(), TextChannel("", nil), FailedChannel(ErrFormat), {Status: "unknown", Text: "text"}} {
		if _, err := Build(&Input{Original: ch, Translation: TextChannel("translation", nil)}); err == nil {
			t.Fatal("no original accepted")
		}
	}
	d, err := Build(&Input{Text: "original", Translation: FailedChannel(ErrFormat), Romanization: AbsentChannel()})
	if err != nil || d.Translation.Status != "error" || d.Romanization.Status != "unavailable" || len(d.Warnings) != 1 {
		t.Fatal(d, err)
	}
	d, err = Build(&Input{Text: "original"})
	if err != nil || d.Translation.Status != "unknown" || d.Romanization.Status != "unknown" {
		t.Fatal(d, err)
	}
}

func TestStructuredInputPreservesRealEndsAndOwnership(t *testing.T) {
	track := &Track{Lines: []Line{{StartMS: ms(1000), EndMS: ms(3000), Text: "A B", Parts: []Word{{StartMS: ms(1000), EndMS: ms(1200), Text: "A "}, {StartMS: ms(2000), Text: "B"}}}}}
	d, err := Build(&Input{Original: &Channel{Status: "available", Track: track}})
	if err != nil {
		t.Fatal(err)
	}
	if *d.Original.Track.Lines[0].EndMS != 3000 || d.Original.Track.Lines[0].Parts[1].EndMS != nil {
		t.Fatal("lost or fabricated ends")
	}
	*track.Lines[0].StartMS = 999
	if err := Validate(d); err != nil {
		t.Fatal("builder retained adapter pointers", err)
	}
	if _, err := Build(&Input{Original: &Channel{Status: "available", Track: track, Text: "different text"}}); !errors.Is(err, ErrFormat) {
		t.Fatal(err)
	}
}

func TestPackageHasNoProjectDependencies(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			name, _ := strconv.Unquote(spec.Path.Value)
			if strings.Contains(name, ".") {
				t.Fatalf("%s imports non-stdlib dependency %s", path, name)
			}
		}
	}
}
