package lyrics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

type businessSource struct {
	search func(context.Context, Query) ([]Candidate, error)
	fetch  func(context.Context, Candidate) (*Payload, error)
}

func (*businessSource) ID() provider.ProviderID { return "only" }
func (*businessSource) DisplayName() string     { return "Only" }
func (p *businessSource) SearchLyrics(ctx context.Context, q Query) ([]Candidate, error) {
	return p.search(ctx, q)
}
func (p *businessSource) FetchLyrics(ctx context.Context, c Candidate) (*Payload, error) {
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
	p := &businessSource{fetch: func(ctx context.Context, _ Candidate) (*Payload, error) {
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
		s := businessService(t, &businessSource{fetch: func(context.Context, Candidate) (*Payload, error) {
			return &Payload{Text: text}, nil
		}})
		doc, err := s.Fetch(context.Background(), Candidate{Source: "only"})
		if err != nil || doc.Text != text {
			t.Fatalf("单能力来源正文被公共层解释: %+v %v", doc, err)
		}
		channel := TextChannel(text, nil)
		if channel.Status != "available" {
			t.Fatal(channel)
		}
		if _, err := DocumentFromPayload(&Payload{Original: channel}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPayloadStructuredModelAndUnknownChannels(t *testing.T) {
	text := "[00:01.20]<00:01.200>Hello <00:01.500>world<00:02.000>"
	for _, p := range []*Payload{{Text: text}, {Original: TextChannel(text, nil)}} {
		d, err := DocumentFromPayload(p)
		if err != nil {
			t.Fatal(err)
		}
		if d.Text != text || d.Format != "enhanced_lrc" || d.Original.Status != "available" || d.WordTiming != "available" || len(d.Rows) != 1 || d.Rows[0].Text != "Hello world" || *d.Rows[0].TimeMS != 1200 {
			t.Fatalf("%+v", d)
		}
		parts := d.Original.Track.Lines[0].Parts
		if len(parts) != 2 || parts[0].EndMS != nil || *parts[1].EndMS != 2000 {
			t.Fatalf("invented word ends: %+v", parts)
		}
		if d.Translation.Status != "unknown" || d.Romanization.Status != "unknown" {
			t.Fatal("missing capability is not an absence claim")
		}
	}
	d, err := DocumentFromPayload(&Payload{Text: "original", Translation: AbsentChannel(), Romanization: UnknownChannel()})
	if err != nil || d.Translation.Status != "unavailable" || d.Romanization.Status != "unknown" || d.Original.SyncStatus != "unsynced" {
		t.Fatal(d, err)
	}
	for _, ch := range []*Channel{{Status: "available", Text: ""}, {Status: "unknown", Text: "unclassified"}, {Status: "available", Track: &Track{}}} {
		if _, err := DocumentFromPayload(&Payload{Original: ch, Translation: TextChannel("translation", nil)}); !errors.Is(err, ErrNoLyrics) {
			t.Fatal(ch, err)
		}
	}
}

func TestPayloadBusinessValidation(t *testing.T) {
	for _, p := range []*Payload{nil, {}, {Translation: TextChannel("translation", nil)}, {Original: FailedChannel(errors.New("decode"))}, {Text: strings.Repeat("x", MaxTextBytes+1)}} {
		if _, err := DocumentFromPayload(p); err == nil {
			t.Fatal("invalid original accepted")
		}
	}
	p := &Payload{Original: TextChannel("[00:01]original", nil), Translation: FailedChannel(errors.New("bad translation")), Romanization: AbsentChannel(), WordTiming: "unavailable"}
	d, err := DocumentFromPayload(p)
	if err != nil || d.Text != "[00:01]original" || d.Translation.Status != "error" || len(d.Warnings) != 1 {
		t.Fatal(d, err)
	}
	if len(p.Warnings) != 0 {
		t.Fatal("document mutated provider payload")
	}
}
