package lyrics

import (
	"context"
	"errors"
	"github.com/norhua/sonexa/internal/lyricdoc"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

type businessSource struct {
	search func(context.Context, Query) ([]Candidate, error)
	fetch  func(context.Context, Candidate) (*lyricdoc.Document, error)
}

func (*businessSource) ID() provider.ProviderID { return "only" }
func (*businessSource) DisplayName() string     { return "Only" }
func (p *businessSource) SearchLyrics(ctx context.Context, q Query) ([]Candidate, error) {
	return p.search(ctx, q)
}
func (p *businessSource) FetchLyrics(ctx context.Context, c Candidate) (*lyricdoc.Document, error) {
	return p.fetch(ctx, c)
}
func businessService(t *testing.T, p *businessSource) Service {
	t.Helper()
	r, err := NewRegistry(Entry{Provider: p})
	if err != nil {
		t.Fatal(err)
	}
	return Service{Sources: r}
}
func TestServiceOwnsValidationRankingAndDeduplication(t *testing.T) {
	calls := 0
	p := &businessSource{search: func(ctx context.Context, q Query) ([]Candidate, error) {
		calls++
		return []Candidate{{Ref: "wrong", Title: "wrong"}, {Ref: "exact", Title: q.Title, Artist: q.Artist, DurationMS: q.DurationMS}, {Ref: "exact", Title: q.Title, Artist: q.Artist}, {Title: q.Title}}, nil
	}}
	s := businessService(t, p)
	for _, q := range []Query{{}, {Title: strings.Repeat("a", 2001)}} {
		if _, err := s.Search(context.Background(), "only", q); err == nil {
			t.Fatal("bad query accepted")
		}
	}
	if _, err := s.Search(context.Background(), "unknown", Query{Title: "song"}); err == nil || calls != 0 {
		t.Fatal("invalid request dispatched")
	}
	rows, err := s.Search(context.Background(), "only", Query{Title: "song", Artist: "artist", DurationMS: 1000})
	if err != nil || len(rows) != 1 || rows[0].Source != "only" || !rows[0].HighConfidence || rows[0].MatchScore != 80 {
		t.Fatal(rows, err)
	}
}
func TestFetchDeadlineIncludesProviderWork(t *testing.T) {
	p := &businessSource{fetch: func(ctx context.Context, _ Candidate) (*lyricdoc.Document, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 40*time.Second {
			t.Fatal("no total deadline")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	s := businessService(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := s.Fetch(ctx, Candidate{Source: "only"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestBusinessTextDoesNotInterpretUpstreamPlaceholders(t *testing.T) {
	for _, text := range []string{"[00:01]纯音乐，请欣赏", "[00:01]真实正文\n[00:02]暂无歌词"} {
		s := businessService(t, &businessSource{fetch: func(context.Context, Candidate) (*lyricdoc.Document, error) {
			return lyricdoc.ParseText(text)
		}})
		doc, err := s.Fetch(context.Background(), Candidate{Source: "only"})
		if err != nil || doc.Text != text {
			t.Fatalf("单能力来源正文被公共层解释: %+v %v", doc, err)
		}
		channel := lyricdoc.TextChannel(text, nil)
		if channel.Status != "available" {
			t.Fatal(channel)
		}
		if _, err := lyricdoc.Build(&lyricdoc.Input{Original: channel}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestServiceRejectsIncompleteDocumentsWithoutRepair(t *testing.T) {
	for _, doc := range []*lyricdoc.Document{nil, {}, {Text: "only text"}, {Text: "only translation", Translation: lyricdoc.TextChannel("only translation", nil)}} {
		calls := 0
		s := businessService(t, &businessSource{fetch: func(context.Context, Candidate) (*lyricdoc.Document, error) {
			calls++
			return doc, nil
		}})
		if got, err := s.Fetch(context.Background(), Candidate{Source: "only"}); err == nil || got != nil || calls != 1 {
			t.Fatal("half-built document was repaired or fetched again", got, err, calls)
		}
	}
	for _, change := range []func(*lyricdoc.Document){
		func(d *lyricdoc.Document) { d.Rows[0].Text = "wrong projection" },
		func(d *lyricdoc.Document) { d.Translation.Status = "available" },
		func(d *lyricdoc.Document) { d.Original.Track.Lines[0].Parts[0].EndMS = nil },
	} {
		d, err := lyricdoc.Build(&lyricdoc.Input{Text: "[00:01]<00:01>A <00:01.2><00:01.5>B"})
		if err != nil {
			t.Fatal(err)
		}
		change(d)
		before := *d
		s := businessService(t, &businessSource{fetch: func(context.Context, Candidate) (*lyricdoc.Document, error) { return d, nil }})
		if _, err := s.Fetch(context.Background(), Candidate{Source: "only"}); !errors.Is(err, lyricdoc.ErrContract) {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*d, before) {
			t.Fatal("invalid document changed")
		}
	}
}

func TestPrecisionAlignmentDoesNotChangePresenceScores(t *testing.T) {
	var baseline ContentScore
	for _, translation := range []string{"[00:01.005]Translation", "[00:01]Translation"} {
		d, err := lyricdoc.Build(&lyricdoc.Input{Text: "[00:01.005]<00:01.005>A <00:01.500>B", Translation: lyricdoc.TextChannel(translation, nil)})
		if err != nil {
			t.Fatal(err)
		}
		s := businessService(t, &businessSource{fetch: func(context.Context, Candidate) (*lyricdoc.Document, error) { return d, nil }})
		got, err := s.Fetch(context.Background(), Candidate{Source: "only"})
		if err != nil || got != d {
			t.Fatal("business reconstructed aligned document", err)
		}
		score := ScoreContent(Candidate{MatchScore: 80, HighConfidence: true}, got, 5000)
		if baseline.TotalScore == nil {
			baseline = score
		} else if !reflect.DeepEqual(score, baseline) {
			t.Fatal("precision diagnostic changed score", score, baseline)
		}
		if err := lyricdoc.Validate(d); err != nil {
			t.Fatal("business mutated aligned document", err)
		}
	}
}

func TestServiceAndScoreReadCompletedDocumentUnchanged(t *testing.T) {
	d, err := lyricdoc.Build(&lyricdoc.Input{
		Original:     lyricdoc.TextChannel("[00:03]unused ordinary", nil),
		WordOriginal: lyricdoc.TextChannel("[offset:500]\n[00:01]<00:01>A <00:01.2><00:01.5>B\n[00:01]A B", nil),
		Translation:  lyricdoc.TextChannel("[offset:-500]\n[00:00]译文", nil),
		Additional:   []*lyricdoc.Channel{{Status: "unknown", Origin: "embedded", Text: "unclassified"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := businessService(t, &businessSource{fetch: func(context.Context, Candidate) (*lyricdoc.Document, error) { return d, nil }})
	got, err := s.Fetch(context.Background(), Candidate{Source: "only"})
	if err != nil || got != d {
		t.Fatal("service replaced provider document", err)
	}
	score := ScoreContent(Candidate{MatchScore: 80, HighConfidence: true}, got, 5000)
	if score.TotalScore == nil || *score.TotalScore != 125 {
		t.Fatal(score)
	}
	if err := lyricdoc.Validate(d); err != nil {
		t.Fatal("scoring changed sealed content", err)
	}
	if len(d.Original.Track.Lines) != 1 || len(d.Rows[0].Translation) != 1 {
		t.Fatal(d)
	}
}
