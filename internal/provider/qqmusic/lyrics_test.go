package qqmusic

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	des "github.com/norhua/sonexa/internal/provider/qqmusic/qrcdes"
	"io"
)

func providerClientForTest(fn func(*http.Request) string) *provider.Client {
	c := provider.NewClient(nil, nil, "test")
	c.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fn(r)))}, nil
	})
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
	b, err := json.Marshal(map[string]any{"code": 0, "req": map[string]any{"code": 0, "data": qrc}, "plain": map[string]any{"code": 0, "data": plain}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestQRCConversion(t *testing.T) {
	for _, tt := range []struct{ name, raw, want string }{
		{"absolute times and gaps", "[ti:Synthetic]\n[1000,2000]你(1000,300)好(1500,1000)", "[ti:Synthetic]\n[00:01.000]<00:01.000>你<00:01.300><00:01.500>好<00:02.500>"},
		{"dynamic track and literal quotes", `<QrcInfos><LyricInfo><Lyric_12 LyricType="1" LyricContent="[1000,1000]A "quoted" (aside) 😀(1000,1000)"/></LyricInfo></QrcInfos>`, "[00:01.000]<00:01.000>A \"quoted\" (aside) 😀<00:02.000>"},
		{"multiple tracks choose index not order", `<QrcInfos><LyricInfo><Lyric_9 LyricContent="[0,1000]wrong(0,1000)"/><Lyric_2 LyricContent="[0,1000]right(0,1000)"/></LyricInfo></QrcInfos>`, "[00:00.000]<00:00.000>right<00:01.000>"},
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
			doc, err := lyricdoc.ParseText(got)
			if err != nil || doc.Format != "enhanced_lrc" {
				t.Fatalf("%+v %v", doc, err)
			}
		})
	}
}
func TestQRCContainerBoundaries(t *testing.T) {
	for _, raw := range []string{
		`<QrcInfos><LyricInfo><Lyric_2 LyricContent="[0,1000]A "quote"(0,1000)"/>`,
		`<QrcInfos><LyricInfo><Lyric_1 LyricContent="[0,1000]A "quote"(0,1000)"/><Lyric_1 LyricContent="[0,1000]B(0,1000)"/></LyricInfo></QrcInfos>`,
		`<QrcInfos><LyricInfo><Lyric_1 LyricContent="[0,1000]A "quoted"(0,1000)" unknown="tail"/></LyricInfo></QrcInfos>`,
		`<QrcInfos><LyricInfo><Lyric_1 LyricContent="[0,1000]A "<Lyric_2 LyricContent="stolen"/>"(0,1000)"/></LyricInfo></QrcInfos>`,
	} {
		decoded, err := decodeQRC(encodedQRC(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := qrcToLRC(decoded); err == nil {
			t.Fatal("accepted ambiguous container")
		}
	}
}

func TestQRCRejectsMalformedAndOversized(t *testing.T) {
	for _, encoded := range []string{"", "00", "xx", "0000000000000000", strings.Repeat("a", (4<<20)+1), encodedQRC(t, strings.Repeat("a", lyricdoc.MaxTextBytes+1)), encodedQRC(t, string([]byte{0xff})), encodedQRC(t, "bad\x00text")} {
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
	doc, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Source: "qq", Ref: "001wMai814HLgJ"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || doc.Format != "xlrc" || doc.WordTiming != "available" || doc.Translation.Status != "available" || doc.Romanization.Status != "available" || len(doc.Warnings) != 0 {
		t.Fatalf("%+v", doc)
	}
	for _, part := range []string{"<00:01.000>原文", "[>zh]翻译", "<00:01.000>roma"} {
		if !strings.Contains(doc.Text, part) {
			t.Fatal(doc.Text)
		}
	}
}

func TestQQPrecisionAlignmentBeforeDelivery(t *testing.T) {
	calls := 0
	body := playBody(t, qqLyricData{QRC: 1,
		Lyric: encodedQRC(t, "[42743,1000]First (42743,300)line(43100,500)\n[47856,1000]Second (47856,300)line(48200,500)"),
		Trans: b64("[00:42.74]第一句译文\n[00:47.85]第二句译文"),
		Roma:  encodedQRC(t, "[00:42.74]first roma\n[00:47.85]second roma"),
	}, qqLyricData{Lyric: b64("[00:42.74]First line\n[00:47.85]Second line")})
	client := providerClientForTest(func(*http.Request) string { calls++; return body })
	d, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Ref: "001wMai814HLgJ"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(d.Rows) != 2 || d.Format != "xlrc" || d.WordTiming != "available" {
		t.Fatal("not completed before provider return", d, calls)
	}
	for i, want := range []int64{42743, 47856} {
		row := d.Rows[i]
		if *row.TimeMS != want || len(row.Translation) != 1 || len(row.Romanization) != 1 || *d.Translation.Track.Lines[i].StartMS != want || *d.Romanization.Track.Lines[i].StartMS != want {
			t.Fatalf("unaligned row/track: %+v", row)
		}
	}
	for _, ch := range []*lyricdoc.Channel{d.Translation, d.Romanization} {
		if ch.SyncStatus != "synced" || len(ch.Diagnostics) != 1 || ch.Diagnostics[0].Code != "time_precision_alignment" || ch.Diagnostics[0].Inference.Count != 2 || ch.Diagnostics[0].Inference.Associations[0].SourceTimeMS != 42740 {
			t.Fatal("missing alignment facts", ch)
		}
	}
	if strings.Contains(d.Text, "[00:42.740]") || strings.Contains(d.Text, "[00:47.850]") || !strings.Contains(d.Text, "<00:42.743>First <00:43.043><00:43.100>line<00:43.600>") {
		t.Fatal("serialized result drifted", d.Text)
	}
	if err := lyricdoc.Validate(d); err != nil {
		t.Fatal(err)
	}
}

func TestQQEmptyTranslationMarkersSurviveXLRC(t *testing.T) {
	body := playBody(t, qqLyricData{QRC: 1,
		Lyric: encodedQRC(t, "[58061,4000]First (58061,500)line(59000,2000)\n[64490,4000]Second (64490,500)line(65500,2000)"),
		Trans: b64("[00:58.06]第一句译文\n[01:03.30]\n[01:04.49]第二句译文\n[01:14.12]"),
		Roma:  encodedQRC(t, "[58061,4000]first roma(58061,4000)\n[64490,4000]second roma(64490,4000)"),
	}, qqLyricData{})
	calls := 0
	client := providerClientForTest(func(*http.Request) string { calls++; return body })
	doc, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Ref: "mid"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(d *lyricdoc.Document) {
		t.Helper()
		if d.Format != "xlrc" || d.Translation.Status != "available" || d.Romanization.Status != "available" {
			t.Fatal("lost available channels")
		}
		markers, originals := []int64{}, 0
		for _, row := range d.Rows {
			switch row.Channel {
			case "original":
				originals++
				if len(row.Translation) != 1 || len(row.Romanization) != 1 {
					t.Fatal("lost associated text")
				}
			case "translation":
				if row.TimeMS == nil || row.Text != "" {
					t.Fatal("expected an empty timed translation marker")
				}
				markers = append(markers, *row.TimeMS)
			default:
				t.Fatal("unexpected independent channel", row.Channel)
			}
		}
		if originals != 2 || len(markers) != 2 || markers[0] != 63300 || markers[1] != 74120 {
			t.Fatal("lost or moved timing markers", markers, originals)
		}
		for _, marker := range []string{"[01:03.300]", "[01:14.120]"} {
			if !strings.Contains(d.Text, marker) {
				t.Fatal("XLRC lost a timing marker")
			}
		}
		if err := lyricdoc.Validate(d); err != nil {
			t.Fatal(err)
		}
	}
	check(doc)
	round, err := lyricdoc.ParseXLRC(doc.Text)
	if err != nil {
		t.Fatal(err)
	}
	check(round)
	text, err := lyricdoc.SerializeXLRC(round)
	if err != nil || text != doc.Text || calls != 1 {
		t.Fatal("roundtrip changed XLRC or added requests", err, calls)
	}
}

func TestQQOptionalFailuresAndPlainFallback(t *testing.T) {
	for _, tt := range []struct {
		name                        string
		qrc                         qqLyricData
		plain                       qqLyricData
		timing, translation, format string
	}{
		{"qrc request returns lrc", qqLyricData{QRC: 1, Lyric: encodedQRC(t, "[00:01]original")}, qqLyricData{}, "unavailable", "unavailable", "lrc"},
		{"qrc request returns plain", qqLyricData{QRC: 1, Lyric: encodedQRC(t, "original <b>literal</b>")}, qqLyricData{}, "unavailable", "unavailable", "text"},
		{"empty word shell", qqLyricData{QRC: 1, Lyric: encodedQRC(t, "[0,100](0,100)")}, qqLyricData{Lyric: b64("[00:01]original")}, "error", "unavailable", "lrc"},
		{"qrc translation", qqLyricData{Lyric: b64("[00:01]original"), Trans: b64(`<QrcInfos><LyricInfo><Lyric_2 LyricContent="[1000,1000]translated(1000,1000)"/></LyricInfo></QrcInfos>`)}, qqLyricData{}, "unavailable", "available", "enhanced_lrc"},
		{"no extras", qqLyricData{Lyric: b64("[00:01]original")}, qqLyricData{}, "unavailable", "unavailable", "lrc"},
		{"broken QRC retains translation", qqLyricData{QRC: 1, Lyric: "broken", Trans: b64("plain translation")}, qqLyricData{Lyric: b64("[00:01]original")}, "error", "available", "lrc"},
		{"broken translation retains QRC", qqLyricData{QRC: 1, Lyric: encodedQRC(t, "[1000,1000]original(1000,1000)"), Trans: "%%%"}, qqLyricData{}, "available", "error", "enhanced_lrc"},
		{"invalid UTF8", qqLyricData{Lyric: b64("[00:01]original"), Trans: b64(string([]byte{0xff}))}, qqLyricData{}, "unavailable", "error", "lrc"},
		{"encrypted translation", qqLyricData{Crypt: 1, Lyric: encodedQRC(t, "[00:01]original"), Trans: encodedQRC(t, "[00:01]translation")}, qqLyricData{}, "unavailable", "available", "lrc"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := providerClientForTest(func(*http.Request) string {
				calls++
				return playBody(t, tt.qrc, tt.plain)
			})
			doc, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Ref: "mid"})
			if err != nil {
				t.Fatal(err)
			}
			if err := lyricdoc.Validate(doc); err != nil || calls != 1 {
				t.Fatal("incomplete provider result", err, calls)
			}
			if doc.WordTiming != tt.timing || doc.Translation.Status != tt.translation || doc.Format != "xlrc" {
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

func TestQQPlayBlockErrorsPreserveOtherChannels(t *testing.T) {
	for _, failedName := range []string{"req", "plain"} {
		for _, bad := range []string{`null`, `{}`, `{"code":2001}`, `{"code":0}`, `{"code":0,"data":null}`, `{"code":0,"data":[]}`, `{"code":"bad","data":{}}`} {
			t.Run(failedName+"/"+bad, func(t *testing.T) {
				good := map[string]any{"code": 0, "data": qqLyricData{Lyric: b64("[00:01]original"), Trans: b64("[00:01]translation")}}
				response := map[string]any{"code": 0, "req": good, "plain": good}
				response[failedName] = json.RawMessage(bad)
				body, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				calls := 0
				client := providerClientForTest(func(r *http.Request) string {
					calls++
					if r.URL.Host != "u.y.qq.com" {
						t.Fatal("可用普通块不应触发旧接口回退")
					}
					return string(body)
				})
				doc, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Source: "qq", Ref: "mid"})
				if err != nil || calls != 1 {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
				if doc.Original.Status != "available" || doc.Translation.Status != "available" || !strings.Contains(doc.Text, "translation") || len(doc.Warnings) == 0 {
					t.Fatalf("部分失败丢失可用通道: %+v", doc)
				}
			})
		}
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
	doc, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Source: "qq", Ref: "mid"})
	if err != nil || calls != 2 || doc.Format != "xlrc" || doc.WordTiming != "error" || !strings.Contains(doc.Text, "untimed translation") || len(doc.Warnings) == 0 {
		t.Fatalf("%+v %v", doc, err)
	}
}

func TestQQPartialBusinessFailureStopsBudgetButKeepsOrdinaryBlock(t *testing.T) {
	calls := 0
	client := providerClientForTest(func(*http.Request) string {
		calls++
		return `{"code":0,"req":{"code":2001},"plain":{"code":0,"data":{"lyric":"` + b64("[00:01]ordinary") + `"}}}`
	})
	budget := provider.NewSendBudget(16)
	ctx := provider.WithSendBudget(context.Background(), budget)
	doc, err := New(client, Config{}, nil).FetchLyrics(ctx, lyrics.Candidate{Ref: "mid"})
	if err != nil || doc == nil || doc.Original.Status != "available" {
		t.Fatal("same-response ordinary block lost", err)
	}
	if budget.Err() == nil || budget.Sent() != 1 || calls != 1 {
		t.Fatal("partial restriction hidden from round", budget.Err(), calls)
	}
	if _, err := New(client, Config{}, nil).FetchLyrics(ctx, lyrics.Candidate{Ref: "next"}); err == nil || calls != 1 {
		t.Fatal("restricted budget sent next candidate", err, calls)
	}
}

func TestQQServiceFailureStopsFallback(t *testing.T) {
	calls := 0
	client := providerClientForTest(func(r *http.Request) string {
		calls++
		return `{"code":2001}`
	})
	_, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Source: "qq", Ref: "mid"})
	var business *provider.BusinessError
	if !errors.As(err, &business) || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestQQRestrictedErrorsNeverFallback(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		calls := 0
		client := provider.NewClient(nil, nil, "test")
		client.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("restricted"))}, nil
		})
		if _, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Source: "qq", Ref: "mid"}); err == nil || calls != 1 {
			t.Fatal(err, calls)
		}
	}
	calls := 0
	client := provider.NewClient(nil, nil, "test")
	client.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) { calls++; return nil, x509.UnknownAuthorityError{} })
	if _, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Source: "qq", Ref: "mid"}); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
	for _, body := range []string{strings.Repeat("x", (4<<20)+1), playBody(t, qqLyricData{QRC: 1, Lyric: encodedQRC(t, strings.Repeat("x", lyricdoc.MaxTextBytes+1))}, qqLyricData{})} {
		calls = 0
		client = providerClientForTest(func(*http.Request) string { calls++; return body })
		if _, err := New(client, Config{}, nil).FetchLyrics(context.Background(), lyrics.Candidate{Source: "qq", Ref: "mid"}); !errors.Is(err, lyricdoc.ErrTooLarge) || calls != 1 {
			t.Fatal(err, calls)
		}
	}
}

func TestQQErrorsAndCancellation(t *testing.T) {
	for _, body := range []string{`{}`, `{"code":2001}`, `{"code":0,"req":{"code":1},"plain":{"code":2}}`, `{"code":0,"req":{"code":0},"plain":{"code":0}}`, `not json`} {
		channels, _ := parseQQPlay([]byte(body))
		if _, err := lyricdoc.Build(channels); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	for _, field := range []string{"code", "retcode", "subcode"} {
		if _, err := parseLegacyLyrics([]byte(fmt.Sprintf(`{"code":0,"%s":1,"lyric":"[00:01]original"}`, field))); err == nil {
			t.Fatalf("ignored %s", field)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	client := providerClientForTest(func(*http.Request) string { calls++; cancel(); return `{"code":2001}` })
	if _, err := New(client, Config{}, nil).FetchLyrics(ctx, lyrics.Candidate{Source: "qq", Ref: "mid"}); err == nil || calls != 1 {
		t.Fatalf("fallback after cancel: %d %v", calls, err)
	}
}

func FuzzQRCParser(f *testing.F) {
	for _, s := range []string{"", "[0,1000]word(0,1000)", "<QrcInfos/>", "[0,1]x(1,2)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = qrcToLRC(s) })
}
