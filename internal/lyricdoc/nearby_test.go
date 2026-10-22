package lyricdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func nearbyLinks(ch *Channel) []LineAssociation {
	for _, d := range ch.Diagnostics {
		if d.Code == "time_nearby_alignment" && d.Inference != nil {
			return d.Inference.Associations
		}
	}
	return nil
}

func TestOnlineNearbyAlignmentAndGuards(t *testing.T) {
	for _, tc := range []struct {
		name, original, extra string
		want                  []int64
		links                 int
	}{
		{"screenshot shifts", "[00:42.570]Original A\n[00:48.330]Original B\n[00:54.490]Original C", "[00:41.360]译一\n[00:47.920]译二\n[00:54.080]译三", []int64{42570, 48330, 54490}, 3},
		{"later translation", "[00:10]Original", "[00:10.5]译文", []int64{10000}, 1},
		{"two second boundary", "[00:10]Original", "[00:08]译文", []int64{10000}, 1},
		{"two second late boundary", "[00:10]Original", "[00:12]译文", []int64{10000}, 1},
		{"outside late bound", "[00:10]Original", "[00:12.001]译文", []int64{12001}, 0},
		{"outside bound", "[00:10]Original", "[00:07.999]译文", []int64{7999}, 0},
		{"equidistant", "[00:10]A\n[00:12]B", "[00:11]译文", []int64{11000}, 0},
		{"same-time voices", "[00:10][v:A]A\n[00:10][v:B]B", "[00:09.5]译文", []int64{9500}, 0},
		{"voice mismatch", "[00:10][v:A]A", "[00:09.5][v:B]译文", []int64{9500}, 0},
		{"two claims", "[00:10]Original", "[00:09.5]译一\n[00:10.5]译二", []int64{9500, 10500}, 0},
		{"exact wins", "[00:10]Original", "[00:09.5]译一\n[00:10]译二", []int64{9500, 10000}, 0},
		{"precision wins", "[00:10]Original", "[00:09.5]译一\n[00:09.995]译二", []int64{9500, 10000}, 0},
		{"multiple exact", "[00:10]Original", "[00:10]译一\n[00:10]译二", []int64{10000, 10000}, 0},
		{"empty forward barrier", "[00:10]Original", "[00:09.5]译文\n[00:09.8]", []int64{9500, 9800}, 0},
		{"empty backward barrier", "[00:10]Original", "[00:10.2]\n[00:10.5]译文", []int64{10200, 10500}, 0},
		{"canonical repeated chorus", "[00:20]Chorus\n[00:10]Chorus", "[00:19.5]副歌二\n[00:09.5]副歌一", []int64{10000, 20000}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := BuildOnline(&Input{Text: tc.original, Translation: TextChannel(tc.extra, nil), Romanization: TextChannel(strings.ReplaceAll(tc.extra, "译", "roma"), nil)}, "zh")
			if err != nil {
				t.Fatal(err)
			}
			for _, ch := range []*Channel{d.Translation, d.Romanization} {
				if len(nearbyLinks(ch)) != tc.links {
					t.Fatalf("unexpected links: %+v", nearbyLinks(ch))
				}
				for i, l := range ch.Track.Lines {
					if *l.StartMS != tc.want[i] {
						t.Fatalf("line %d = %d, want %d", i, *l.StartMS, tc.want[i])
					}
				}
			}
			assertOnlineRoundtrip(t, d)
		})
	}
}

func assertOnlineRoundtrip(t *testing.T, d *Document) {
	t.Helper()
	if err := Validate(d); err != nil {
		t.Fatal(err)
	}
	round, err := ParseXLRC(d.Text)
	if err != nil || !reflect.DeepEqual(round.Rows, d.Rows) {
		t.Fatal("XLRC row roundtrip failed", err)
	}
	text, err := SerializeXLRC(round)
	if err != nil || text != d.Text {
		t.Fatal("XLRC text roundtrip failed", err)
	}
}

func TestOnlineInferencePreservesTimingOwnershipAndLocalImport(t *testing.T) {
	input := &Input{Text: "[offset:1500]\n[00:01.5]かなかな", Translation: TrackChannel(&Track{Lines: []Line{{StartMS: ms(1000), EndMS: ms(1800), Text: "译文", Parts: []Word{{StartMS: ms(1000), EndMS: ms(1800), Text: "译文"}}}}}, nil)}
	before, _ := json.Marshal(input)
	d, err := BuildOnline(input, "zh")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(input)
	l := d.Translation.Track.Lines[0]
	if string(before) != string(after) || *l.StartMS != 0 || *l.EndMS != 1800 || *l.Parts[0].StartMS != 1000 || *l.Parts[0].EndMS != 1800 {
		t.Fatal("timing or ownership lost", l)
	}
	links := nearbyLinks(d.Translation)
	if len(links) != 1 || links[0].SourceTimeMS != 1000 || links[0].AssociatedTimeMS != 0 {
		t.Fatal(links)
	}
	assertOnlineRoundtrip(t, d)
	rebuilt, err := BuildOnline(&Input{Original: d.Original, Translation: d.Translation, Romanization: d.Romanization}, "zh")
	if err != nil || !reflect.DeepEqual(rebuilt, d) {
		t.Fatal("online rebuild drift", err)
	}
	local, err := Build(input)
	if err != nil || *local.Translation.Track.Lines[0].StartMS != 1000 || local.Translation.Track.Lines[0].Language != "" {
		t.Fatal("local builder inferred language or association", err)
	}
	*input.Translation.Track.Lines[0].StartMS = 9
	if err := Validate(d); err != nil {
		t.Fatal("output shares pointers with input", err)
	}
	links[0].SourceTimeMS++
	if err := Validate(d); !errors.Is(err, ErrContract) {
		t.Fatal("changed diagnostics accepted", err)
	}
}

func TestOnlineNearbyPreservesUnconfirmedContentAndLineEnds(t *testing.T) {
	d, err := BuildOnline(&Input{
		Text: "[00:10]Original",
		Translation: TrackChannel(&Track{Lines: []Line{
			{StartMS: ms(9500), EndMS: ms(9800), Text: "real end"},
			{StartMS: ms(9600), Text: "independent", Independent: true},
			{Text: "untimed"},
		}}, nil),
		Romanization: &Channel{Status: "unknown", Origin: "unknown", Text: "[00:09.5]unknown"},
		Additional:   []*Channel{{Status: "unknown", Origin: "embedded", Text: "[00:09.5]additional"}},
	}, "zh")
	if err != nil {
		t.Fatal(err)
	}
	if len(nearbyLinks(d.Translation)) != 0 || *d.Translation.Track.Lines[0].StartMS != 9500 || *d.Romanization.Track.Lines[0].StartMS != 9500 || *d.Additional[0].Track.Lines[0].StartMS != 9500 {
		t.Fatal("unalignable lines changed")
	}
	for _, s := range []string{"real end", "independent", "untimed", "unknown", "additional"} {
		if !strings.Contains(d.Text, s) {
			t.Fatal("content lost", s)
		}
	}
	assertOnlineRoundtrip(t, d)
	// A saved independent XLRC line must remain independent even when its
	// time is close enough for a fresh native online track to be inferred.
	local, err := ParseText("[xlrc:0.1]\n[x-sonexa:1]\n[00:10]Original\n[x-channel:translation:und]\n[00:09.5]译文")
	if err != nil || len(local.Rows) != 2 || local.Translation.Track.Lines[0].Language != "und" {
		t.Fatal("import changed", err)
	}
}

func TestOnlineNearbyKeepsExplicitParents(t *testing.T) {
	d, err := BuildOnline(&Input{Text: "[xlrc:0.1]\n[lang:ja]\n[00:10][v:A]かなかな\n[>zh]译文 A\n[00:10][v:B]うたうた\n[>zh]译文 B"}, "zh")
	if err != nil || len(d.Rows) != 2 || d.Rows[0].Translation[0] != "译文 A" || d.Rows[1].Translation[0] != "译文 B" {
		t.Fatal("explicit parents lost", err)
	}
	assertOnlineRoundtrip(t, d)
}

func TestOnlineNearbyLargeInputAndDiagnosticLimit(t *testing.T) {
	var original, extra strings.Builder
	for i := int64(1); i <= 4000; i++ {
		fmt.Fprintf(&original, "[%s]Original\n", clock(i*4000))
		fmt.Fprintf(&extra, "[%s]译文\n", clock(i*4000-500))
	}
	d, err := BuildOnline(&Input{Text: original.String(), Translation: TextChannel(extra.String(), nil)}, "zh")
	if err != nil || len(nearbyLinks(d.Translation)) != 4000 {
		t.Fatal("bounded input failed", err)
	}
	ch := TextChannel("[00:09.5]译文", nil)
	ch.Diagnostics = []Diagnostic{{Code: "large", Message: strings.Repeat("x", MaxProjectionBytes-64-len("large"))}}
	if _, err := BuildOnline(&Input{Text: "[00:10]Original", Translation: ch}, "zh"); !errors.Is(err, ErrTooLarge) {
		t.Fatal("diagnostics limit ignored", err)
	}
}
