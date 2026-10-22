package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

// Reproduce the reported catalog shape with synthetic lyric text. The primary
// credit matches the query but has no lyrics; title search recalls the vocalist.
func TestQQDifferentCreditRecallPrefetchAndAdoption(t *testing.T) {
	const emptyMID, goodMID = "003iQsFU4IGFlR", "001Buqii4Rxrm5"
	const album = "アコースティック・エピソード"
	original := bundleFixture(t, "qrc", "[1000,1000]かな(1000,400)の歌(1500,500)")
	roma := bundleFixture(t, "qrc", "[1000,1000]kana (1000,400)no uta(1500,500)")
	calls := 0
	client := provider.NewClient(nil, forbiddenLyricCache{t}, "test")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		var response any
		if r.URL.Host == "c.y.qq.com" {
			if r.URL.Query().Get("songmid") != emptyMID {
				t.Error("wrong legacy identity")
			}
			response = map[string]int{"code": -1901, "retcode": -1901, "subcode": -1901}
		} else {
			var req map[string]struct {
				Method string
				Param  map[string]any
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				return nil, err
			}
			switch req["req"].Method {
			case "DoSearchForQQMusicDesktop":
				if req["req"].Param["query"] != "夕立 CROW'SCLAW" || req["title"].Param["query"] != "夕立" || len(req) != 3 {
					t.Error("missing batch query")
				}
				song := func(mid, artist, album string) any {
					return map[string]any{"mid": mid, "name": "夕立", "singer": []any{map[string]string{"name": artist}}, "album": map[string]string{"name": album}, "interval": 287}
				}
				block := func(rows ...any) any {
					return map[string]any{"code": 0, "data": map[string]any{"body": map[string]any{"song": map[string]any{"list": rows}}}}
				}
				response = map[string]any{"code": 0, "req": block(song(emptyMID, "CROW'SCLAW", "")), "title": block(song(goodMID, "3L", album), song(emptyMID, "CROW'SCLAW", ""))}
			case "GetPlayLyricInfo":
				mid := req["req"].Param["songMid"]
				artist, record := "3L", album
				if mid == emptyMID {
					artist, record = "CROW'SCLAW", ""
				} else if mid != goodMID {
					t.Error("forged candidate identity")
				}
				for _, name := range []string{"req", "plain"} {
					p := req[name].Param
					if p["songMid"] != mid || p["songName"] != base64.StdEncoding.EncodeToString([]byte("夕立")) || p["singerName"] != base64.StdEncoding.EncodeToString([]byte(artist)) || p["albumName"] != base64.StdEncoding.EncodeToString([]byte(record)) || p["interval"] != float64(287) {
						t.Error("target facts replaced candidate facts")
					}
				}
				data := map[string]any{}
				if mid == goodMID {
					data = map[string]any{"qrc": 1, "crypt": 0, "lyric": original, "trans": base64.StdEncoding.EncodeToString([]byte("[00:01]合成译文")), "roma": roma}
				}
				response = map[string]any{"code": 0, "req": map[string]any{"code": 0, "data": data}, "plain": map[string]any{"code": 0, "data": map[string]any{}}}
			default:
				return nil, fmt.Errorf("unexpected protocol")
			}
		}
		raw, err := json.Marshal(response)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw))}, err
	})
	a, done := newTestApp(t)
	defer done()
	a.enricher.Pause()
	service := sourceService(client)
	a.lyricSvc = &service
	target := lyricTrack(t, a)
	id, err := a.CreateLyricsSearch(target)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(id)
	cards, err := a.SearchLyrics(id, "qq", lyrics.Query{Title: "夕立", Artist: "CROW'SCLAW", Album: album, DurationMS: 287000})
	if err != nil || len(cards) != 2 || calls != 1 {
		t.Fatal(cards, err, calls)
	}
	goodID := ""
	for _, c := range cards {
		if c.Artist == "3L" {
			goodID = c.ID
			if c.Album != album || c.HighConfidence {
				t.Fatal("credit mismatch silently upgraded", c)
			}
		}
	}
	if goodID == "" {
		t.Fatal("vocalist not recalled")
	}
	snapshot := finishLyricsPrefetch(t, a, id)
	if snapshot.Selected != 2 || snapshot.Success != 1 || calls != 4 || a.lyricSessions[id].roundBudget.Sent() != 4 {
		t.Fatal("source stopped or requests expanded", snapshot, calls)
	}
	for _, c := range snapshot.Candidates {
		if c.Candidate.ID == goodID {
			if c.Translation.Status != "available" || c.Romanization.Status != "available" || c.Candidate.AutoPreviewEligible {
				t.Fatal("bad channel/selection status", c)
			}
		} else if c.ErrorKind != "no_lyrics" || c.StopReason != "" || c.Candidate.TotalScore != nil {
			t.Fatal("empty candidate misreported or scored", c)
		}
	}
	rev, _ := a.LyricsRevision(target)
	if _, err := a.AdoptLyrics(id, goodID, rev); err == nil {
		t.Fatal("prefetch permitted adoption")
	}
	preview, err := a.FetchLyrics(id, goodID)
	if err != nil || calls != 4 {
		t.Fatal(err, calls)
	}
	d := preview.Document
	if d.Format != "xlrc" || d.WordTiming != "available" || len(d.Rows[0].Translation) != 1 || len(d.Rows[0].Romanization) != 1 {
		t.Fatal("incomplete document", d)
	}
	if err := a.AcknowledgeLyricsPreview(id, goodID, preview.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptLyrics(id, goodID, rev); err != nil {
		t.Fatal(err)
	}
	saved, err := a.GetTrackDetail(target)
	if err != nil || saved.Metadata.Lyrics == nil || *saved.Metadata.Lyrics != d.Text {
		t.Fatal("adoption changed text", err)
	}
	round, err := lyricdoc.ParseXLRC(*saved.Metadata.Lyrics)
	if err != nil || !reflect.DeepEqual(round.Rows, d.Rows) {
		t.Fatal("roundtrip changed rows", err)
	}
}
