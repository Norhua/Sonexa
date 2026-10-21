package lyricdoc

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestXLRCStandardVoicesLanguagesRubyAndWords(t *testing.T) {
	raw := "[lang:ja]\n[langs:ja,en,ja-Latn,fr]\n[00:01.005][v:A]<00:01.005>私[わたし]<00:01.500>が歌[うた]う\n[>en]I sing\n[>ja-Latn]watashi ga utau\n[>fr]Je chante\n[00:01.005][v:B]君[きみ]も\n[>en][v:ignored]You too\n[00:03.000]"
	d, err := ParseXLRC(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != "xlrc" || len(d.Rows) != 3 || d.Rows[0].Text != "私が歌う" || d.Rows[0].Voice != "A" || d.Rows[1].Voice != "B" {
		t.Fatal(d)
	}
	if len(d.Rows[0].Translation) != 2 || d.Rows[1].Translation[0] != "You too" || len(d.Rows[0].Annotations) != 3 {
		t.Fatal(d.Rows)
	}
	if d.Rows[0].Annotations[2].Language != "ja-Latn" || d.Romanization.Status != "available" {
		t.Fatal(d)
	}
	if !reflect.DeepEqual(d.Rows[0].Ruby, []Ruby{{0, 1, "わたし"}, {2, 3, "うた"}}) {
		t.Fatal(d.Rows[0].Ruby)
	}
	encoded, err := SerializeXLRC(d)
	if err != nil {
		t.Fatal(err)
	}
	round, err := ParseXLRC(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Rows, round.Rows) {
		t.Fatalf("rows changed\n%+v\n%+v\n%s", d.Rows, round.Rows, encoded)
	}
	if round.Original.Track.Lines[0].Parts[0].EndMS != nil {
		t.Fatal("invented word end")
	}
	if err := Validate(round); err != nil {
		t.Fatal(err)
	}
}

func TestXLRCNativeEndsIndependentTextAndEscapingRoundTrip(t *testing.T) {
	original := &Track{Lines: []Line{{StartMS: ms(1005), EndMS: ms(2500), Text: "私が", Voice: "A", Ruby: []Ruby{{0, 1, "わたし"}}, Parts: []Word{{StartMS: ms(1005), EndMS: ms(1200), Text: "私", Ruby: []Ruby{{0, 1, "わたし"}}}, {StartMS: ms(1600), Text: "が"}}}, {Text: `literal [>en] \ 私[かな] <00:01>`}}}
	d, err := Build(&Input{Original: &Channel{Status: "available", Track: original}, Translation: TextChannel("[00:01]译文\n[00:04]独立\n没有时间", nil), Romanization: TextChannel("[00:01]roma", nil), Additional: []*Channel{{Status: "unknown", Origin: "unknown", Text: "谐音"}}})
	if err != nil {
		t.Fatal(err)
	}
	round, err := ParseXLRC(d.Text)
	if err != nil {
		t.Fatal(err, d.Text)
	}
	if !reflect.DeepEqual(d.Rows, round.Rows) {
		t.Fatalf("rows changed\n%+v\n%+v\n%s", d.Rows, round.Rows, d.Text)
	}
	a, b := d.Original.Track.Lines[0], round.Original.Track.Lines[0]
	if *b.EndMS != *a.EndMS || !reflect.DeepEqual(a.Parts, b.Parts) {
		t.Fatal(a, b)
	}
	if round.Additional[0].Status != "unknown" || round.Translation.Track.Lines[2].StartMS != nil {
		t.Fatal(round)
	}
	encoded, err := SerializeXLRC(round)
	if err != nil || encoded != d.Text {
		t.Fatalf("unstable serialization %v\n%s\n%s", err, d.Text, encoded)
	}
}

func TestXLRCSameTextDistinctVoicesAndExplicitParents(t *testing.T) {
	d, err := ParseXLRC("[00:01][v:A]同文\n[>en]same\n[00:01][v:B]同文\n[>en]same")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rows) != 2 || len(d.Rows[0].Translation) != 1 || len(d.Rows[1].Translation) != 1 {
		t.Fatal(d.Rows)
	}
	d, err = Build(&Input{Text: "[00:01][v:A]同文\n[00:01][v:B]同文", Translation: TextChannel("[00:01]ambiguous", nil)})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rows) != 3 || !strings.Contains(d.Text, "[x-channel:translation:und]") {
		t.Fatal(d)
	}
}

func TestXLRCMalformedAndBounded(t *testing.T) {
	for _, raw := range []string{"[>en]orphan", "[00:61]bad", "[00:01]ok\n[>bad_tag]bad", "[00:01][v:]empty", "[xlrc:9]\n[00:01]no", "[xlrc:0.1]\n[x-sonexa:1]\n[00:01][x-end:999]bad", "[xlrc:0.1]\n[x-sonexa:1]\n[00:01]ok\n[>en][x-role:original]bad", "[x-channel:translation:en]\n[00:01]no original", "[00:01]<00:02>A<00:01>B"} {
		if _, err := ParseXLRC(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if _, err := ParseXLRC(strings.Repeat("x", MaxTextBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	raw := strings.Repeat("[00:01]", 1000) + "a\n[>en]" + strings.Repeat("b", 10000)
	if _, err := ParseXLRC(raw); !errors.Is(err, ErrTooLarge) {
		t.Fatal("expansion not bounded", err)
	}
	d, err := ParseXLRC("[00:01]食べる[たべる] 私[bad] x <b>literal</b>")
	if err != nil || d.Rows[0].Text != "食べる[たべる] 私[bad] x <b>literal</b>" {
		t.Fatal(d, err)
	}
}

func TestXLRCRubyOwnershipAndUnicodeOffsets(t *testing.T) {
	d, err := ParseXLRC("[00:01]😀私[わたし]が")
	if err != nil {
		t.Fatal(err)
	}
	if d.Rows[0].Ruby[0].Start != 1 || d.Rows[0].Ruby[0].End != 2 {
		t.Fatal(d.Rows)
	}
	input := &Input{Original: d.Original, Translation: d.Translation, Romanization: d.Romanization}
	built, err := Build(input)
	if err != nil {
		t.Fatal(err)
	}
	d.Original.Track.Lines[0].Ruby[0].Reading = "あ"
	if err := Validate(built); err != nil {
		t.Fatal("aliased ruby", err)
	}
	built.Rows[0].Ruby[0].Reading = "い"
	if err := Validate(built); err == nil {
		t.Fatal("mutation accepted")
	}
}

func TestXLRCExplicitRelationshipsCannotLieAndUnknownCannotBecomeTranslation(t *testing.T) {
	original := &Track{Lines: []Line{{ID: "a", StartMS: ms(1000), Text: "original", Voice: "A"}}}
	for _, l := range []Line{{ParentID: "missing", StartMS: ms(1000), Text: "T"}, {ParentID: "a", StartMS: ms(2000), Text: "T"}, {ParentID: "a", StartMS: ms(1000), Voice: "B", Text: "T"}} {
		if _, err := Build(&Input{Original: TrackChannel(original, nil), Translation: TrackChannel(&Track{Lines: []Line{l}}, nil)}); !errors.Is(err, ErrFormat) {
			t.Fatal("invalid parent accepted", err)
		}
	}
	d, err := Build(&Input{Text: "[00:01]original", Translation: &Channel{Status: "unknown", Origin: "unknown", Text: "[00:01]unclassified"}})
	if err != nil {
		t.Fatal(err)
	}
	round, err := ParseXLRC(d.Text)
	if err != nil {
		t.Fatal(err)
	}
	if AvailableContent(round.Translation) || len(round.Additional) != 1 || len(d.Rows) != 2 || d.Rows[1].Channel != "additional" {
		t.Fatal("unknown promoted", d, round)
	}
	d, err = ParseXLRC("[00:01]same\n[>en]same")
	if err != nil || !AvailableContent(d.Translation) {
		t.Fatal("explicit same text translation discarded", d, err)
	}
	input := TrackChannel(&Track{Lines: []Line{{Text: `literal [v:X] 私[かな]`, StartMS: ms(1000)}}}, nil)
	first, err := Build(&Input{Original: input})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(&Input{Original: first.Original, Translation: first.Translation, Romanization: first.Romanization})
	if err != nil || second.Text != first.Text {
		t.Fatal("literal structured text reparsed", err)
	}
}

func TestXLRCExtensionNegotiationAndUnknownHeaders(t *testing.T) {
	d, err := ParseXLRC("[custom_tag:kept]\n[00:01]text")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := SerializeXLRC(d)
	if err != nil || !strings.Contains(encoded, "[custom_tag:kept]") {
		t.Fatal(encoded, err)
	}
	if _, err := ParseXLRC("[x-text]not standard"); err == nil {
		t.Fatal("extension without marker accepted")
	}
}

func FuzzXLRC(f *testing.F) {
	for _, s := range []string{"[00:01][v:A]私[わたし]\n[>en]I", "[xlrc:0.1]\n[x-sonexa:1]\n[x-text]literal \\[tag]", "[00:01]<00:01>A<00:02><00:03>B"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := ParseXLRC(s)
		if err != nil {
			return
		}
		out, err := SerializeXLRC(d)
		if err != nil {
			t.Fatal(err)
		}
		next, err := ParseXLRC(out)
		if err != nil {
			t.Fatal(err, out)
		}
		again, err := SerializeXLRC(next)
		if err != nil || again != out {
			t.Fatalf("non-idempotent %v\n%s\n%s", err, out, again)
		}
	})
}
