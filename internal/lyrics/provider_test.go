package lyrics

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/provider"
)

// Deliberately implements only identity and lyrics, without embedding metadata.
type lyricsOnly struct {
	id    provider.ProviderID
	calls int
}

func (p *lyricsOnly) ID() provider.ProviderID { return p.id }
func (*lyricsOnly) DisplayName() string       { return "歌词来源" }
func (p *lyricsOnly) SearchLyrics(_ context.Context, q Query) ([]Candidate, error) {
	p.calls++
	return []Candidate{{Source: string(p.id), Ref: "native", Title: q.Title}}, nil
}
func (p *lyricsOnly) FetchLyrics(_ context.Context, c Candidate) (*lyricdoc.Document, error) {
	p.calls++
	return lyricdoc.ParseText(c.Title)
}

var _ Provider = (*lyricsOnly)(nil)
var _ provider.Upstream = (*lyricsOnly)(nil)

func TestLyricsOnlyContract(t *testing.T) {
	var p Provider = &lyricsOnly{id: "only"}
	rows, err := p.SearchLyrics(context.Background(), Query{Title: "歌名"})
	if err != nil || len(rows) != 1 || rows[0].Ref != "native" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	payload, err := p.FetchLyrics(context.Background(), rows[0])
	if err != nil || payload.Text != "歌名" {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}
	if _, ok := p.(provider.MetadataProvider); ok {
		t.Fatal("歌词来源不应要求元数据能力")
	}
}

func TestPublicDTOStructure(t *testing.T) {
	for _, tc := range []struct {
		value any
		keys  []string
	}{
		{Query{}, []string{"title", "artist", "album", "durationMs"}},
		{Candidate{Ref: "private-native-reference"}, []string{"id", "source", "title", "artist", "album", "durationMs", "matchScore", "totalScore", "scoreTerms", "autoPreviewEligible", "matchTerms", "sortKey", "highConfidence", "matchNotes"}},
		{lyricdoc.Document{}, []string{"text", "format", "rows", "offsetMs"}},
		{Source{}, []string{"id", "name"}},
	} {
		body, err := json.Marshal(tc.value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != len(tc.keys) {
			t.Fatalf("%T: %s", tc.value, body)
		}
		for _, key := range tc.keys {
			if _, ok := fields[key]; !ok {
				t.Fatalf("%T missing %s", tc.value, key)
			}
		}
		if strings.Contains(string(body), "private-native-reference") {
			t.Fatal("原生引用泄漏")
		}
	}
	// Optional channel names are also part of the public document contract.
	v := reflect.TypeOf(lyricdoc.Document{})
	for field, tag := range map[string]string{"Original": "original,omitempty", "Translation": "translation,omitempty", "Romanization": "romanization,omitempty", "WordTiming": "wordTiming,omitempty", "Warnings": "warnings,omitempty"} {
		f, ok := v.FieldByName(field)
		if !ok || f.Tag.Get("json") != tag {
			t.Fatalf("%s JSON tag changed", field)
		}
	}
}
