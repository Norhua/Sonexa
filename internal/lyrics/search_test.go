package lyrics

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLyricsRelevance(t *testing.T) {
	q := Query{Title: "綿雪の夢", Artist: "ハトリアヤ", Album: "狼と巫女とお菓子の家", DurationMS: 321000}
	candidates := []Candidate{
		{Ref: "unrelated", Title: "minamo", Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "other", Title: q.Title, Artist: "Other", Album: "Other", DurationMS: 100000},
		{Ref: "accompaniment", Title: q.Title + " (伴奏)", Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "instrumental", Title: q.Title + " (Off vocal)", Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "version", Title: q.Title + " (Applice Version)", Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "group", Title: q.Title + " (繁雪之梦)", Artist: "Crest", Album: q.Album, DurationMS: q.DurationMS},
		{Ref: "long", Title: q.Title, Artist: q.Artist, Album: q.Album, DurationMS: 400000},
		{Ref: "exact", Title: q.Title, Artist: q.Artist, Album: q.Album, DurationMS: q.DurationMS},
	}
	got := rankCandidates(q, candidates)
	if len(got) != 3 || got[0].Ref != "exact" || !got[0].HighConfidence || got[0].Score != 100 {
		t.Fatalf("%+v", got)
	}
	for _, c := range got[1:] {
		if c.HighConfidence || !strings.Contains(strings.Join(c.MatchNotes, " "), "需确认") {
			t.Fatalf("unsafe automatic match %+v", c)
		}
	}
	// Close duration alone only rescues an exact title, never an unrelated title.
	q.Album = "different album"
	got = rankCandidates(q, []Candidate{candidates[5]})
	if len(got) != 1 || got[0].HighConfidence {
		t.Fatalf("group credit not retained for confirmation: %+v", got)
	}
	q.DurationMS = 0
	if got := rankCandidates(q, []Candidate{candidates[5]}); len(got) != 0 {
		t.Fatalf("accepted uncorroborated title: %+v", got)
	}
}

func TestLyricsPartialQueriesAndNormalization(t *testing.T) {
	for _, tt := range []struct {
		name       string
		q          Query
		c          Candidate
		keep, high bool
	}{
		{"fullwidth", Query{Title: "ＡＢＣ", Artist: "ハトリアヤ"}, Candidate{Title: "abc", Artist: "はとりあや"}, true, true},
		{"title only", Query{Title: "Same"}, Candidate{Title: "Same"}, true, false},
		{"artist only", Query{Artist: "Alice"}, Candidate{Title: "Song", Artist: "Alice"}, true, false},
		{"wrong artist", Query{Artist: "Alice"}, Candidate{Title: "Song", Artist: "Bob"}, false, false},
		{"missing artist", Query{Artist: "Alice"}, Candidate{Title: "Song"}, false, false},
		{"album only", Query{Album: "Album"}, Candidate{Title: "Song", Album: "Album"}, true, false},
		{"wrong album", Query{Album: "Album"}, Candidate{Title: "Song", Album: "Unrelated"}, false, false},
		{"duration missing", Query{Title: "Song", Artist: "Alice"}, Candidate{Title: "Song", Artist: "Alice"}, true, true},
		{"explicit accompaniment", Query{Title: "Song (伴奏)", Artist: "Alice"}, Candidate{Title: "Song (伴奏)", Artist: "Alice"}, true, true},
		{"short title", Query{Title: "A", Artist: "Alice"}, Candidate{Title: "Another", Artist: "Alice"}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := rankCandidates(tt.q, []Candidate{tt.c})
			if (len(got) > 0) != tt.keep || len(got) > 0 && got[0].HighConfidence != tt.high {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestAllSourcesRankAndDeduplicate(t *testing.T) {
	fixtures := map[string]string{
		"qq":    `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[{"mid":"0039MnYb0qxYhV","title":"Wrong","singer":[{"name":"Alice"}]},{"mid":"001wMai814HLgJ","title":"Song","singer":[{"name":"Alice"}]},{"mid":"001wMai814HLgJ","title":"Song","singer":[{"name":"Alice"}]}]}}}}}`,
		"163":   `{"code":200,"result":{"songs":[{"id":1,"name":"Wrong","ar":[{"name":"Alice"}]},{"id":2,"name":"Song","ar":[{"name":"Alice"}]},{"id":2,"name":"Song","ar":[{"name":"Alice"}]}]}}`,
		"kugou": `{"status":1,"errcode":0,"data":{"info":[{"hash":"1","songname":"Wrong","singername":"Alice"},{"hash":"2","songname":"Song","singername":"Alice"},{"hash":"2","songname":"Song","singername":"Alice"}]}}`,
		"kuwo":  `{"TOTAL":"3","abslist":[{"MUSICRID":"1","NAME":"Wrong","ARTIST":"Alice"},{"MUSICRID":"2","NAME":"Song","ARTIST":"Alice"},{"MUSICRID":"2","NAME":"Song","ARTIST":"Alice"}]}`,
	}
	for source, body := range fixtures {
		t.Run(source, func(t *testing.T) {
			got, err := parseSearch(source, []byte(body), Query{Title: "Song", Artist: "Alice"})
			if err != nil || len(got) != 1 || !got[0].HighConfidence || got[0].Source != source {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestSearchUsesTargetDuration(t *testing.T) {
	client := providerClientForTest(func(r *http.Request) string {
		var request struct {
			Req struct{ Param struct{ Query string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Req.Param.Query != "Song Alice" {
			t.Fatalf("bad query: %+v %v", request, err)
		}
		return `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[{"mid":"001wMai814HLgJ","title":"Song","singer":[{"name":"Alice"}],"interval":321}]}}}}}`
	})
	got, err := (Service{client}).Search(context.Background(), "qq", Query{Title: "Song", Artist: "Alice", DurationMS: 321000})
	if err != nil || len(got) != 1 || got[0].Score != 80 {
		t.Fatalf("%+v %v", got, err)
	}
}

func responseForTest(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
