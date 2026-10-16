package lyrics

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	des "github.com/norhua/sonexa/internal/lyrics/qrcdes"
	"github.com/norhua/sonexa/internal/provider"
)

func providerClientForTest(fn func(*http.Request) string) *provider.Client {
	c := provider.NewClient(nil, nil, "test")
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { return responseForTest(fn(r)), nil })
	return c
}
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
func encodedQRC(t *testing.T, text string) string {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	data = append(data, make([]byte, (8-len(data)%8)%8)...)
	des.New([]byte("!@#)(*$%"), true).TransformBytes(data)
	des.New([]byte("123ZXC!@"), false).TransformBytes(data)
	des.New([]byte("!@#)(NHL"), true).TransformBytes(data)
	return hex.EncodeToString(data)
}
func playBody(t *testing.T, qrc, plain qqLyricData) string {
	t.Helper()
	code := 0
	b, err := json.Marshal(map[string]any{"code": 0, "req": qqLyricBlock{&code, &qrc}, "plain": qqLyricBlock{&code, &plain}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestQRCConversion(t *testing.T) {
	for _, tt := range []struct{ name, raw, want string }{
		{"absolute times and gaps", "[ti:Synthetic]\n[1000,2000]你(1000,300)好(1500,1000)", "[ti:Synthetic]\n[00:01.000]<00:01.000>你<00:01.300><00:01.500>好<00:02.500>"},
		{"xml entities", `<QrcInfos><LyricInfo><Lyric_1 LyricContent="[1000,1000]A&amp;B(1000,1000)"/></LyricInfo></QrcInfos>`, "[00:01.000]<00:01.000>A&B<00:02.000>"},
		{"credits and empty romaji word", "[0,1000](0,1000)\n[1000,1000]Synthetic credit\n[2000,1000]Test(2000,1000)", "[00:00.000]<00:00.000><00:01.000>\n[00:01.000]Synthetic credit\n[00:02.000]<00:02.000>Test<00:03.000>"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := decodeQRC(encodedQRC(t, tt.raw))
			if err != nil || decoded != tt.raw {
				t.Fatalf("decode %q %v", decoded, err)
			}
			got, err := qrcToLRC(decoded)
			if err != nil || got != tt.want {
				t.Fatalf("%q %v", got, err)
			}
			doc, err := ParseText(got)
			if err != nil || doc.Format != "enhanced_lrc" {
				t.Fatalf("%+v %v", doc, err)
			}
		})
	}
}
func TestQRCRejectsMalformedAndOversized(t *testing.T) {
	for _, encoded := range []string{"", "00", "xx", "0000000000000000", strings.Repeat("a", (4<<20)+1), encodedQRC(t, strings.Repeat("a", MaxTextBytes+1)), encodedQRC(t, string([]byte{0xff})), encodedQRC(t, "bad\x00text")} {
		if _, err := decodeQRC(encoded); err == nil {
			t.Fatal("accepted bad QRC")
		}
	}
	for _, raw := range []string{"<bad", "<QrcInfos/>", "text", "[100,200]x(0,100)", "[100,200]x(100,300)", "[0,1000]a(500,200)b(600,200)", "[0,1000]x(0,1000)untimed", "[9999999999999999999999,0]x(0,0)", "[0,100]credit only", "[0,100](0,100)"} {
		if _, err := qrcToLRC(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestQQPlayPreservesChannelsAndUsesExplicitFlags(t *testing.T) {
	calls := 0
	client := providerClientForTest(func(r *http.Request) string {
		calls++
		if r.URL.Host != "u.y.qq.com" || r.Header.Get("Referer") != "https://y.qq.com/" {
			t.Fatal(r.URL)
		}
		var request map[string]struct {
			Module, Method string
			Param          map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		for name, flag := range map[string]float64{"req": 1, "plain": 0} {
			p := request[name]
			if p.Module != "music.musichallSong.PlayLyricInfo" || p.Method != "GetPlayLyricInfo" || p.Param["songMid"] != "001wMai814HLgJ" || p.Param["qrc"] != flag || p.Param["trans"] != float64(1) || p.Param["roma"] != float64(1) || p.Param["crypt"] != float64(0) {
				t.Fatalf("%+v", p)
			}
		}
		return playBody(t, qqLyricData{QRC: 1, Lyric: encodedQRC(t, "[1000,1000]原文(1000,1000)"), Trans: b64("[00:01.00]翻译"), Roma: encodedQRC(t, "[1000,1000]roma(1000,1000)")}, qqLyricData{Lyric: b64("[00:01.00]原文")})
	})
	doc, err := (Service{client}).Fetch(context.Background(), Candidate{Source: "qq", Ref: "001wMai814HLgJ"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || doc.Format != "enhanced_lrc" || doc.WordTiming != "available" || doc.Translation.Status != "available" || doc.Romanization.Status != "available" || len(doc.Warnings) != 0 {
		t.Fatalf("%+v", doc)
	}
	for _, part := range []string{"<00:01.000>原文", "[00:01.000]翻译", "<00:01.000>roma"} {
		if !strings.Contains(doc.Text, part) {
			t.Fatal(doc.Text)
		}
	}
}

func TestQQOptionalFailuresAndPlainFallback(t *testing.T) {
	for _, tt := range []struct {
		name                        string
		qrc                         qqLyricData
		plain                       qqLyricData
		timing, translation, format string
	}{
		{"no extras", qqLyricData{Lyric: b64("[00:01]original")}, qqLyricData{}, "unavailable", "unavailable", "lrc"},
		{"broken QRC retains translation", qqLyricData{QRC: 1, Lyric: "broken", Trans: b64("plain translation")}, qqLyricData{Lyric: b64("[00:01]original")}, "error", "available", "lrc"},
		{"broken translation retains QRC", qqLyricData{QRC: 1, Lyric: encodedQRC(t, "[1000,1000]original(1000,1000)"), Trans: "%%%"}, qqLyricData{}, "available", "error", "enhanced_lrc"},
		{"invalid UTF8", qqLyricData{Lyric: b64("[00:01]original"), Trans: b64(string([]byte{0xff}))}, qqLyricData{}, "unavailable", "error", "lrc"},
		{"encrypted translation", qqLyricData{Crypt: 1, Lyric: encodedQRC(t, "[00:01]original"), Trans: encodedQRC(t, "[00:01]translation")}, qqLyricData{}, "unavailable", "available", "lrc"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			channels := parseQQPlay([]byte(playBody(t, tt.qrc, tt.plain)))
			doc, err := channels.document()
			if err != nil {
				t.Fatal(err)
			}
			if doc.WordTiming != tt.timing || doc.Translation.Status != tt.translation || doc.Format != tt.format {
				t.Fatalf("%+v", doc)
			}
			if tt.translation == "error" && len(doc.Warnings) == 0 {
				t.Fatal("silent optional failure")
			}
			if tt.name == "broken QRC retains translation" && !strings.Contains(doc.Text, "plain translation") {
				t.Fatal("lost untimed translation")
			}
		})
	}
}

func TestQQFallbackKeepsNewTranslationAndReportsFailure(t *testing.T) {
	calls := 0
	client := providerClientForTest(func(r *http.Request) string {
		calls++
		if r.URL.Host == "u.y.qq.com" {
			return playBody(t, qqLyricData{QRC: 1, Lyric: "broken", Trans: b64("untimed translation")}, qqLyricData{})
		}
		if r.URL.Query().Get("songmid") != "mid" {
			t.Fatal(r.URL)
		}
		return `{"code":0,"retcode":0,"subcode":0,"lyric":"[00:01]original"}`
	})
	doc, err := (Service{client}).Fetch(context.Background(), Candidate{Source: "qq", Ref: "mid"})
	if err != nil || calls != 2 || doc.Format != "lrc" || doc.WordTiming != "error" || !strings.Contains(doc.Text, "untimed translation") || len(doc.Warnings) == 0 {
		t.Fatalf("%+v %v", doc, err)
	}
}

func TestQQServiceFailureIsNotReportedAsMissingTranslation(t *testing.T) {
	client := providerClientForTest(func(r *http.Request) string {
		if r.URL.Host == "u.y.qq.com" {
			return `{"code":2001}`
		}
		return `{"code":0,"lyric":"[00:01]original","trans":""}`
	})
	doc, err := (Service{client}).Fetch(context.Background(), Candidate{Source: "qq", Ref: "mid"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Translation.Status != "error" || doc.Romanization.Status != "error" || doc.WordTiming != "error" || len(doc.Warnings) == 0 {
		t.Fatalf("failure hidden as missing fields: %+v", doc)
	}
}

func TestQQErrorsAndCancellation(t *testing.T) {
	for _, body := range []string{`{}`, `{"code":2001}`, `{"code":0,"req":{"code":1},"plain":{"code":2}}`, `{"code":0,"req":{"code":0},"plain":{"code":0}}`, `not json`} {
		if _, err := parseQQPlay([]byte(body)).document(); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	for _, field := range []string{"code", "retcode", "subcode"} {
		if _, err := parseDocument("qq", []byte(fmt.Sprintf(`{"code":0,"%s":1,"lyric":"[00:01]original"}`, field))); err == nil {
			t.Fatalf("ignored %s", field)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	client := providerClientForTest(func(*http.Request) string { calls++; cancel(); return `{"code":2001}` })
	if _, err := (Service{client}).Fetch(ctx, Candidate{Source: "qq", Ref: "mid"}); err == nil || calls != 1 {
		t.Fatalf("fallback after cancel: %d %v", calls, err)
	}
}

func TestMergeNeverDropsUntimedExtras(t *testing.T) {
	for _, original := range []string{"plain original", "[00:01]original", "[00:01]<00:01>original<00:02>"} {
		doc, err := channelDocument(textChannel(original, nil), textChannel("plain translation", nil), textChannel("[00:01]roma\nuntimed tail", nil), "unavailable", nil)
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

func TestTimestampExpansionIsBounded(t *testing.T) {
	var timestamps strings.Builder
	for i := 0; i < 1000; i++ {
		timestamps.WriteString("[" + clock(i*1000) + "]")
	}
	if _, err := mergeTimed(timestamps.String()+strings.Repeat("a", 10000), "translation"); err == nil {
		t.Fatal("unbounded timestamp expansion")
	}
}

func FuzzQRCParser(f *testing.F) {
	for _, s := range []string{"", "[0,1000]word(0,1000)", "<QrcInfos/>", "[0,1]x(1,2)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = qrcToLRC(s) })
}
