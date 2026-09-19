package matching

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseCreditIdentities(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"Kirara Magic feat. Kitsui Akira", []string{"Kirara Magic", "Kitsui Akira"}},
		{" A Feat. B & C ", []string{"A", "B", "C"}},
		{"A ft. B × C;D、E", []string{"A", "B", "C", "D", "E"}},
		{"A featuring B", []string{"A", "B"}},
		{"涼宮ハルヒ(CV:平野綾) feat. 初音ミク", []string{"涼宮ハルヒ(CV:平野綾)", "初音ミク"}},
		{"A(CV: B & C)", []string{"A(CV: B & C)"}},
		{"\t平野綾\n", []string{"平野綾"}},
		{"AC/DC", []string{"AC/DC"}},
		{"Earth, Wind & Fire (US)", []string{"Earth, Wind", "Fire (US)"}}, // 无结构化边界时，仅按明确连接词拆
		{"Band (US)", []string{"Band (US)"}},
		{"", nil},
	} {
		t.Run(tc.text, func(t *testing.T) {
			got := DefaultRules().ParseCredit(tc.text)
			if got.Raw != tc.text || got.Uncertain {
				t.Fatalf("parse = %+v", got)
			}
			var names []string
			for _, a := range got.Artists {
				names = append(names, a.Name)
				if tc.text[a.Start:a.End] != a.Name {
					t.Fatalf("wrong byte span: %+v", a)
				}
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("names = %v, want %v", names, tc.want)
			}
		})
	}
}

func TestCVIsOneIdentity(t *testing.T) {
	for _, prefix := range []string{"CV:", "cv:", "ＣＶ:", "C.V.", "声:", "声優:", "声优:", "配音:", "Vo.", "CV："} {
		for _, brackets := range [][2]string{{"(", ")"}, {"（", "）"}, {"【", "】"}} {
			text := "涼宮ハルヒ" + brackets[0] + prefix + "平野綾" + brackets[1]
			got := DefaultRules().ParseCredit(text)
			if got.Uncertain || len(got.Artists) != 1 || got.Artists[0].Name != text || got.Artists[0].VoiceActor != "平野綾" {
				t.Fatalf("CV must stay intact: %+v", got)
			}
		}
	}
}

func TestStructuredCreditNameNeverSplitsBand(t *testing.T) {
	got := DefaultRules().ParseCreditName("Earth, Wind & Fire")
	if len(got.Artists) != 1 || got.Artists[0].Name != got.Raw {
		t.Fatalf("upstream identity must win: %+v", got)
	}
}

func TestUncertainCreditCreatesNoArtists(t *testing.T) {
	for _, text := range []string{"Artist (CV: Live Mix)", "A feat. ", "A;;B"} {
		got := DefaultRules().ParseCredit(text)
		if !got.Uncertain || len(got.Artists) != 0 || got.Raw != text {
			t.Fatalf("must preserve uncertain input: %+v", got)
		}
	}
}

func TestCreditSpansPreserveNamesAndJoinPhrases(t *testing.T) {
	text := "涼宮ハルヒ(CV:平野綾) feat. 初音ミク"
	parsed := DefaultRules().ParseCredit(text)
	spans := BuildSpans(parsed, map[string]int64{parsed.Artists[0].Name: 42, "初音ミク": 17})
	if len(spans.Spans) != 2 || text[spans.Spans[0].End:spans.Spans[1].Start] != " feat. " {
		t.Fatalf("spans = %+v", spans)
	}
	data, err := json.Marshal(spans)
	if err != nil {
		t.Fatal(err)
	}
	var round CreditSpans
	if err := json.Unmarshal(data, &round); err != nil || !reflect.DeepEqual(spans, round) {
		t.Fatalf("roundtrip = %+v, %v", round, err)
	}
	if got := BuildSpans(parsed, map[string]int64{"初音ミク": 17}); len(got.Spans) != 1 || got.Spans[0].ArtistID != 17 {
		t.Fatalf("unknown fragments should remain plain text: %+v", got)
	}
}

func TestRulesAreExtensible(t *testing.T) {
	r := DefaultRules()
	text := "涼宮ハルヒ(CAST:平野綾)"
	if got := r.ParseCredit(text); got.Artists[0].VoiceActor != "" {
		t.Fatal(got)
	}
	r.CVPrefixes = append(r.CVPrefixes, `CAST`)
	r.compile()
	if got := r.ParseCredit(text); got.Artists[0].VoiceActor != "平野綾" {
		t.Fatal(got)
	}
}

func containsSub(s, sub string) bool { return strings.Contains(s, sub) }
