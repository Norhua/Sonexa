package qqmusic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func searchResponse(t *testing.T, groups map[string][]song) string {
	t.Helper()
	body := map[string]any{"code": 0}
	for name, songs := range groups {
		body[name] = map[string]any{"code": 0, "data": map[string]any{"body": map[string]any{"song": map[string]any{"list": songs}}}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestLyricsSearchBatchesDistinctTitleWithoutChangingOtherSearches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query lyrics.Query
		terms map[string]string
	}{
		{"credit differs", lyrics.Query{Title: "夕立", Artist: "CROW'SCLAW", Album: "not a keyword", DurationMS: 287000}, map[string]string{"req": "夕立 CROW'SCLAW", "title": "夕立"}},
		{"title only", lyrics.Query{Title: "  夕立  "}, map[string]string{"req": "夕立"}},
		{"artist only", lyrics.Query{Artist: "Artist"}, map[string]string{"req": "Artist"}},
		{"album only", lyrics.Query{Album: "Album"}, map[string]string{"req": "Album"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := providerClientForTest(func(r *http.Request) string {
				calls++
				var req map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatal(err)
				}
				if r.Method != "POST" || r.URL.Path != musicuPath || len(req) != len(tc.terms)+1 {
					t.Fatal("not a single bounded batch", r.URL, len(req))
				}
				groups := map[string][]song{}
				for name, term := range tc.terms {
					var got, want any
					if err := json.Unmarshal(req[name], &got); err != nil {
						t.Fatal(err)
					}
					raw, _ := json.Marshal(songSearchBlock(term, 20))
					_ = json.Unmarshal(raw, &want)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("%s: %+v", name, got)
					}
					groups[name] = []song{}
				}
				return searchResponse(t, groups)
			})
			budget := provider.NewSendBudget(1)
			rows, err := New(client, Config{}, nil).SearchLyrics(provider.WithSendBudget(context.Background(), budget), tc.query)
			if err != nil || len(rows) != 0 || calls != 1 || budget.Sent() != 1 {
				t.Fatal(rows, err, calls)
			}
		})
	}
}

func TestLyricsSearchRetainsDifferentCreditAndBoundsCandidates(t *testing.T) {
	groups := map[string][]song{"req": {}, "title": {}}
	for _, name := range []string{"req", "title"} {
		for i := 0; i < 25; i++ {
			groups[name] = append(groups[name], song{MID: fmt.Sprintf("%s%010d", name, i), Name: "夕立", Singer: []singer{{Name: "3L"}}, Album: albumRef{Name: "アコースティック・エピソード"}, Interval: 287})
		}
	}
	body := searchResponse(t, groups)
	client := providerClientForTest(func(*http.Request) string { return body })
	p := New(client, Config{}, nil)
	q := lyrics.Query{Title: "夕立", Artist: "CROW'SCLAW", Album: "アコースティック・エピソード", DurationMS: 287000}
	rows, err := p.SearchLyrics(context.Background(), q)
	if err != nil || len(rows) != 40 {
		t.Fatal(len(rows), err)
	}
	registry, err := lyrics.NewRegistry(lyrics.Entry{Provider: p})
	if err != nil {
		t.Fatal(err)
	}
	service := lyrics.Service{Sources: registry}
	rows, err = service.Search(context.Background(), "qq", q)
	if err != nil || len(rows) != 20 || len(lyrics.MatchTop(rows)) != 3 {
		t.Fatal(len(rows), err)
	}
	for _, row := range rows {
		if row.Artist != "3L" || row.HighConfidence || row.MatchTerms[1].Value != 0 {
			t.Fatal("relabelled credit or relaxed confidence", row)
		}
	}
	// Overlap uses the primary query's exact identity facts, not mixed metadata.
	groups["title"] = []song{groups["req"][0], {MID: "short", Name: "invalid"}, {MID: "001Buqii4Rxrm5", Name: "夕立", Singer: []singer{{Name: "3L"}}, Interval: 287}}
	groups["req"] = groups["req"][:1]
	groups["req"][0].Singer = []singer{{Name: "CROW'SCLAW"}}
	body = searchResponse(t, groups)
	rows, err = p.SearchLyrics(context.Background(), q)
	if err != nil || len(rows) != 2 || rows[0].Artist != "CROW'SCLAW" || rows[1].Artist != "3L" {
		t.Fatal(rows, err)
	}
}

func TestLyricsSearchChecksBothBlocksAndCancellation(t *testing.T) {
	for _, bad := range []string{`null`, `{}`, `{"code":24001}`, `{"code":0,"data":{"body":{"song":{"list":null}}}}`} {
		for _, failed := range []string{"req", "title"} {
			calls := 0
			body := map[string]json.RawMessage{}
			_ = json.Unmarshal([]byte(searchResponse(t, map[string][]song{"req": {}, "title": {}})), &body)
			body[failed] = json.RawMessage(bad)
			raw, _ := json.Marshal(body)
			p := New(providerClientForTest(func(*http.Request) string { calls++; return string(raw) }), Config{}, nil)
			if rows, err := p.SearchLyrics(context.Background(), lyrics.Query{Title: "Song", Artist: "Artist"}); err == nil || rows != nil || calls != 1 {
				t.Fatal("failed query hidden or retried", rows, err, calls)
			}
		}
	}
	calls := 0
	p := New(providerClientForTest(func(*http.Request) string { calls++; return "{}" }), Config{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.SearchLyrics(ctx, lyrics.Query{Title: "Song", Artist: "Artist"}); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal(err, calls)
	}
}
