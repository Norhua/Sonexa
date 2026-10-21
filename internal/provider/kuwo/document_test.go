package kuwo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func fetchKuwoBody(t *testing.T, list []map[string]any) (*lyricdoc.Document, error, int) {
	t.Helper()
	calls := 0
	client := provider.NewClient(nil, nil, "test")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		key := "code"
		if r.URL.Host == "m.kuwo.cn" {
			key = "status"
		}
		body, err := json.Marshal(map[string]any{key: 200, "data": map[string]any{"lrclist": list}})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	d, err := New(client).FetchLyrics(context.Background(), foreignSong)
	return d, err, calls
}

func TestKuwoInvalidTimesDisableHeuristicsAndPreserveAllRows(t *testing.T) {
	for _, bad := range []any{nil, "", "bad", "NaN", "Infinity", "-Inf", "-0.1", -1, "86400.001", "1e1000"} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			list := []map[string]any{}
			texts := []string{}
			for _, row := range alternation(8, 8, 8) {
				list = append(list, map[string]any{"time": float64(row.timeMS) / 1000, "lineLyric": row.text})
				texts = append(texts, row.text)
			}
			list[0]["time"] = bad
			list = append(list, map[string]any{"time": 1, "lineLyric": "  1 < 2 (旁白) 😀　"}, map[string]any{"time": 2, "lineLyric": ""})
			texts = append(texts, "  1 < 2 (旁白) 😀　", "")
			d, err, calls := fetchKuwoBody(t, list)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || d.Format != "text" || d.WordTiming != "unsupported" || d.Translation.Status != "unknown" || d.Romanization.Status != "unknown" || len(d.Warnings) == 0 {
				t.Fatal(d, calls)
			}
			if d.Text != strings.Join(texts, "\n") {
				t.Fatal("plain fallback changed source text")
			}
			got := []string{}
			for _, row := range d.Rows {
				if row.TimeMS != nil {
					t.Fatal("fabricated timing")
				}
				got = append(got, row.Text)
			}
			if !reflect.DeepEqual(got, texts) {
				t.Fatal("source order changed")
			}
			if err := lyricdoc.Validate(d); err != nil {
				t.Fatal(err)
			}
			score := lyrics.ScoreContent(lyrics.Candidate{MatchScore: 80}, d, 10000)
			if score.TotalScore == nil || *score.TotalScore != 80 || score.Terms[3].Value != 0 {
				t.Fatal(score)
			}
		})
	}
	// Even an invalid empty timing row disables the heuristic; it is not
	// removed before source validity is known.
	list := []map[string]any{{"time": 1, "lineLyric": "body"}, {"time": "bad", "lineLyric": ""}}
	if d, err, _ := fetchKuwoBody(t, list); err != nil || d.Text != "body\n" || d.Format != "text" {
		t.Fatal(d, err)
	}
}

func TestKuwoOffsetsAndNoContent(t *testing.T) {
	for _, tags := range [][]string{{"[offset:bad]"}, {"[offset:500]", "[offset:-500]"}, {"[offset:999999999999999999999999]"}} {
		list := []map[string]any{}
		texts := []string{}
		for _, row := range alternation(8, 8, 8) {
			texts = append(texts, row.text)
			list = append(list, map[string]any{"time": float64(row.timeMS) / 1000, "lineLyric": row.text})
		}
		// Metadata in the middle of the source list must not be moved to the
		// header or silently interpreted after degrading to plain text.
		for _, tag := range tags {
			texts = append(texts, tag)
			list = append(list, map[string]any{"time": 0, "lineLyric": tag})
		}
		d, err, calls := fetchKuwoBody(t, list)
		if err != nil || calls != 1 {
			t.Fatal(err, calls)
		}
		if d.Text != strings.Join(texts, "\n") || d.Format != "text" || d.OffsetMS != 0 || d.Translation.Status != "unknown" {
			t.Fatal(d)
		}
		if d.Original.Diagnostics[0].Code != "invalid_offset" || len(d.Rows) != len(texts) {
			t.Fatal("lost offset diagnostic/text")
		}
	}
	for _, list := range [][]map[string]any{
		{}, {{"time": 1, "lineLyric": ""}}, {{"time": "bad", "lineLyric": "  "}}, {{"time": 0, "lineLyric": "[ti:Only metadata]"}},
	} {
		if d, err, calls := fetchKuwoBody(t, list); d != nil || !errors.Is(err, lyricdoc.ErrNoLyrics) || calls != 2 {
			t.Fatal(d, err, calls)
		}
	}
	for _, offset := range []int64{-500, 500, 5000} {
		rows := append([]lyricRow{{text: fmt.Sprintf("[offset:%d]", offset)}}, nextTimeRows(4, false)...)
		d := fetchSyntheticRows(t, rows, foreignSong, false)
		if d.OffsetMS != offset || d.Translation.Status != "available" || d.Rows[0].TimeMS == nil || *d.Rows[0].TimeMS != 3000 {
			t.Fatal(d)
		}
		if strings.Count(d.Text, "[offset:") != 1 || *d.Rows[0].TimeMS-d.OffsetMS != 3000-offset {
			t.Fatal("offset drift")
		}
		again, err := lyricdoc.ParseText(d.Text)
		if err != nil || again.OffsetMS != offset || *again.Rows[0].TimeMS-again.OffsetMS != 3000-offset {
			t.Fatal(again, err)
		}
	}
}

func TestKuwoSecondsSortAndDeduplicateAfterDetection(t *testing.T) {
	list := []map[string]any{{"time": "2.3454", "lineLyric": "last"}, {"time": "1.2345", "lineLyric": "first"}, {"time": "1.2345", "lineLyric": "first"}, {"time": "1.2345", "lineLyric": "different"}}
	d, err, calls := fetchKuwoBody(t, list)
	if err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if d.Text != "[00:01.235]first\n[00:01.235]different\n[00:02.345]last" || len(d.Rows) != 3 {
		t.Fatal(d)
	}
}
