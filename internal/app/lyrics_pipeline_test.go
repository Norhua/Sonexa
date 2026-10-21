package app

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	des "github.com/norhua/sonexa/internal/provider/qqmusic/qrcdes"
)

// Short independently constructed format fixtures, not real song lyrics.
func bundleFixture(t *testing.T, format, text string) string {
	t.Helper()
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	if _, err := z.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), b.Bytes()...)
	if format == "krc" {
		key := []byte{0x40, 0x47, 0x61, 0x77, 0x5e, 0x32, 0x74, 0x47, 0x51, 0x36, 0x31, 0x2d, 0xce, 0xd2, 0x6e, 0x69}
		for i := range data {
			data[i] ^= key[i%len(key)]
		}
		return base64.StdEncoding.EncodeToString(append([]byte("krc1"), data...))
	}
	for len(data)%8 != 0 {
		data = append(data, 0)
	}
	des.New([]byte("!@#)(*$%"), true).TransformBytes(data)
	des.New([]byte("123ZXC!@"), false).TransformBytes(data)
	des.New([]byte("!@#)(NHL"), true).TransformBytes(data)
	return hex.EncodeToString(data)
}

type observedLyrics struct {
	lyrics.Provider
	mu   sync.Mutex
	docs map[string]*lyricdoc.Document
}

func (p *observedLyrics) FetchLyrics(ctx context.Context, c lyrics.Candidate) (*lyricdoc.Document, error) {
	d, err := p.Provider.FetchLyrics(ctx, c)
	if err == nil {
		p.mu.Lock()
		p.docs[c.Ref] = d
		p.mu.Unlock()
	}
	return d, err
}

func TestLyricsFourSourceAppPipeline(t *testing.T) {
	qrc := bundleFixture(t, "qrc", "[1005,1000]Hello (1005,400)world(1500,400)")
	roma := bundleFixture(t, "qrc", "[00:01]ro ma")
	krc := bundleFixture(t, "krc", "[language:"+base64.StdEncoding.EncodeToString([]byte(`{"content":[{"type":1,"lyricContent":[["译文"]]},{"type":0,"lyricContent":[["ro","ma"]]}]}`))+"]\n[1000,1000]<0,400,0>Hello <500,400,0>world")
	for _, source := range []string{"qq", "qq-legacy", "163", "kugou", "kuwo"} {
		t.Run(source, func(t *testing.T) {
			actual := source
			if source == "qq-legacy" {
				actual = "qq"
			}
			var mu sync.Mutex
			sends := 0
			client := provider.NewClient(nil, forbiddenLyricCache{t}, "test")
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				sends++
				mu.Unlock()
				if r.URL.Scheme != "https" {
					t.Error("non-HTTPS request")
				}
				var body any
				switch r.URL.Host {
				case "u.y.qq.com":
					request, _ := io.ReadAll(r.Body)
					if strings.Contains(string(request), "DoSearchForQQMusicDesktop") {
						rows := []any{}
						for _, n := range []int{4, 3, 2, 1, 1} {
							rows = append(rows, map[string]any{"mid": fmt.Sprintf("001wMai814HLg%d", n), "name": "Song", "singer": []any{map[string]any{"name": "Artist"}}, "interval": 90})
						}
						body = map[string]any{"code": 0, "req": map[string]any{"code": 0, "data": map[string]any{"body": map[string]any{"song": map[string]any{"list": rows}}}}}
					} else if strings.Contains(string(request), "GetPlayLyricInfo") {
						lyric := qrc
						if source == "qq-legacy" {
							lyric = ""
						}
						body = map[string]any{"code": 0, "req": map[string]any{"code": 0, "data": map[string]any{"qrc": 1, "lyric": lyric, "trans": base64.StdEncoding.EncodeToString([]byte("[00:01]译文")), "roma": roma}}, "plain": map[string]any{"code": 0, "data": map[string]any{"lyric": ""}}}
					} else {
						t.Error("unexpected QQ protocol")
					}
				case "c.y.qq.com":
					if source != "qq-legacy" {
						t.Error("unnecessary legacy request")
					}
					body = map[string]any{"code": 0, "lyric": base64.StdEncoding.EncodeToString([]byte("[00:01]Hello world"))}
				case "music.163.com":
					if r.URL.Path == "/api/song/lyric/v1" {
						body = map[string]any{"code": 200, "yrc": map[string]string{"lyric": "[1000,1000](1000,400,0)Hello (1500,400,0)world"}, "tlyric": map[string]string{"lyric": "[00:01]译文"}, "romalrc": map[string]string{"lyric": "[00:01]ro ma"}}
					} else {
						rows := []any{}
						for _, n := range []int{4, 3, 2, 1, 1} {
							rows = append(rows, map[string]any{"id": n, "name": "Song", "ar": []any{map[string]string{"name": "Artist"}}, "dt": 90000})
						}
						body = map[string]any{"code": 200, "result": map[string]any{"songs": rows}}
					}
				case "mobileservice.kugou.com":
					rows := []any{}
					for _, n := range []int{4, 3, 2, 1, 1} {
						rows = append(rows, map[string]any{"hash": fmt.Sprint(n), "songname": "Song", "singername": "Artist", "duration": 90})
					}
					body = map[string]any{"status": 1, "errcode": 0, "data": map[string]any{"info": rows}}
				case "lyrics.kugou.com":
					if r.URL.Path == "/search" {
						body = map[string]any{"status": 200, "candidates": []any{map[string]any{"id": "1", "accesskey": "fixture-key", "song": "Song", "duration": 90000}}}
					} else if r.URL.Path == "/download" && r.URL.Query().Get("fmt") == "krc" {
						body = map[string]any{"status": 200, "fmt": "krc", "content": krc}
					} else {
						t.Error("unexpected Kugou protocol")
					}
				case "search.kuwo.cn":
					rows := []any{}
					for _, n := range []int{4, 3, 2, 1, 1} {
						rows = append(rows, map[string]any{"MUSICRID": fmt.Sprint(n), "NAME": "Song", "ARTIST": "Artist", "DURATION": 90})
					}
					body = map[string]any{"TOTAL": 5, "abslist": rows}
				case "www.kuwo.cn":
					rows := []any{}
					for i := 0; i < 8; i++ {
						rows = append(rows, map[string]any{"time": 2*i + 1, "lineLyric": fmt.Sprintf("Original verse %d", i)}, map[string]any{"time": 2*i + 2, "lineLyric": fmt.Sprintf("译文%d", i)})
					}
					body = map[string]any{"code": 200, "data": map[string]any{"lrclist": rows}}
				default:
					t.Error("unexpected host: no generation service allowed", r.URL.Host)
				}
				data, err := json.Marshal(body)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data))}, nil
			})
			service := sourceService(client)
			upstream, err := service.Sources.Get(provider.ProviderID(actual))
			if err != nil {
				t.Fatal(err)
			}
			observed := &observedLyrics{Provider: upstream, docs: map[string]*lyricdoc.Document{}}
			registry, err := lyrics.NewRegistry(lyrics.Entry{Provider: observed})
			if err != nil {
				t.Fatal(err)
			}
			a, done := newTestApp(t)
			defer done()
			a.enricher.Pause()
			a.lyricSvc = &lyrics.Service{Sources: registry}
			target, other := lyricTrack(t, a), lyricTrack(t, a)
			id, err := a.CreateLyricsSearch(target)
			if err != nil {
				t.Fatal(err)
			}
			defer a.CancelLyricsSearch(id)
			cards, err := a.SearchLyrics(id, actual, lyrics.Query{Title: "Song", Artist: "Artist", DurationMS: 90000})
			if err != nil || len(cards) != 4 {
				t.Fatal("search/dedup", len(cards), err)
			}
			snapshot := finishLyricsPrefetch(t, a, id)
			if snapshot.Selected != 3 || snapshot.Success != 3 || snapshot.Candidates[3].Candidate.TotalScore != nil {
				t.Fatal("top 3 contract", snapshot.Selected, snapshot.Success)
			}
			wantSends := 4
			if source == "kugou" || source == "qq-legacy" {
				wantSends = 7
			}
			if sends != wantSends || a.lyricSessions[id].roundBudget.Sent() != wantSends {
				t.Fatal("unexpected sends", sends)
			}
			revision, _ := a.LyricsRevision(target)
			if _, err := a.AdoptLyrics(id, cards[0].ID, revision); err == nil {
				t.Fatal("prefetch permitted adoption")
			}
			preview, err := a.FetchLyrics(id, cards[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			delivered := observed.docs[a.lyricSessions[id].candidates[cards[0].ID].Ref]
			if preview.Document != delivered || sends != wantSends {
				t.Fatal("preview transformed or refetched upstream result")
			}
			if source == "qq" {
				if len(delivered.Rows) != 1 || *delivered.Rows[0].TimeMS != 1005 || len(delivered.Rows[0].Translation) != 1 || len(delivered.Rows[0].Romanization) != 1 || strings.Contains(delivered.Text, "[00:01.000]") {
					t.Fatal("precision alignment missing from provider/preview text", delivered)
				}
				for _, ch := range []*lyricdoc.Channel{delivered.Translation, delivered.Romanization} {
					if *ch.Track.Lines[0].StartMS != 1005 || len(ch.Diagnostics) != 1 || ch.Diagnostics[0].Code != "time_precision_alignment" || ch.Diagnostics[0].Inference.Associations[0].SourceTimeMS != 1000 {
						t.Fatal("precision association missing from channel", ch)
					}
				}
			}
			summary := snapshot.Candidates[0]
			if !reflect.DeepEqual(summary.Translation, lyrics.SummarizeChannel(delivered.Translation)) || !reflect.DeepEqual(summary.Romanization, lyrics.SummarizeChannel(delivered.Romanization)) {
				t.Fatal("summary mismatch")
			}
			terms := summary.Candidate.ScoreTerms
			if terms[3].Value != 15 || (source != "kuwo" && terms[4].Value != 5) {
				t.Fatal("channel bonuses", terms)
			}
			if source == "kuwo" && (delivered.Translation.Origin != "embedded" || len(delivered.Original.Track.Lines) != 8 || len(summary.Translation.Diagnostics) == 0) {
				t.Fatal("inference not propagated")
			}
			raw, _ := json.Marshal(snapshot)
			if strings.Contains(string(raw), "Hello") || strings.Contains(string(raw), "Original verse") || strings.Contains(string(raw), "fixture-key") || strings.Contains(string(raw), "associations") {
				t.Fatal("summary leaked source content")
			}
			if err := a.AcknowledgeLyricsPreview(id, cards[0].ID, preview.Version); err != nil {
				t.Fatal(err)
			}
			if _, err := a.AdoptLyrics(id, cards[0].ID, revision); err != nil {
				t.Fatal(err)
			}
			saved, err := a.GetTrackDetail(target)
			if err != nil || saved.Metadata.Lyrics == nil || *saved.Metadata.Lyrics != delivered.Text {
				t.Fatal("adoption text changed", err)
			}
			untouched, err := a.GetTrackDetail(other)
			if err != nil || untouched.Metadata.Lyrics != nil {
				t.Fatal("wrong target changed", err)
			}
			// Same candidate through explicit refresh follows exactly the same producer
			// path and scoring; only the business version changes.
			refreshed, err := a.RefreshLyrics(id, cards[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(delivered, refreshed.Document) || refreshed.Version != preview.Version+1 {
				t.Fatal("automatic/manual document diverged")
			}
			next, _ := a.LyricsSearchSnapshot(id)
			if !reflect.DeepEqual(next.Candidates[0].Candidate.ScoreTerms, terms) {
				t.Fatal("explicit score diverged")
			}
			if err := lyricdoc.Validate(delivered); err != nil {
				t.Fatal("business mutated source document", err)
			}
		})
	}
}
