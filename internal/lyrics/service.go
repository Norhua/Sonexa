// Package lyrics owns lyric search validation, scoring and orchestration.
// Concrete upstream packages implement its consumer-owned Provider contract.
package lyrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/provider"
)

const MaxResults = 20

type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Query struct {
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int    `json:"durationMs"`
}
type Candidate struct {
	ID                  string      `json:"id"`
	Source              string      `json:"source"`
	Ref                 string      `json:"-"`
	Title               string      `json:"title"`
	Artist              string      `json:"artist"`
	Album               string      `json:"album"`
	DurationMS          int         `json:"durationMs"`
	MatchScore          int         `json:"matchScore"`
	TotalScore          *int        `json:"totalScore"`
	ScoreTerms          []ScoreTerm `json:"scoreTerms"`
	AutoPreviewEligible bool        `json:"autoPreviewEligible"`
	MatchTerms          []ScoreTerm `json:"matchTerms"`
	SortKey             string      `json:"sortKey"`
	HighConfidence      bool        `json:"highConfidence"`
	MatchNotes          []string    `json:"matchNotes"`
}

type Service struct{ Sources *Registry }

// SearchTerm is a business rule shared by lyric capabilities, not a protocol.
func SearchTerm(q Query) string {
	term := strings.TrimSpace(q.Title + " " + q.Artist)
	if term == "" {
		term = strings.TrimSpace(q.Album)
	}
	return term
}

func (s Service) Search(ctx context.Context, source string, q Query) ([]Candidate, error) {
	p, err := s.Sources.Get(provider.ProviderID(source))
	if err != nil {
		return nil, err
	}
	term := SearchTerm(q)
	if term == "" {
		return nil, fmt.Errorf("请输入歌词搜索条件")
	}
	if len(term)+len(q.Album) > 2000 {
		return nil, fmt.Errorf("搜索条件过长")
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := p.SearchLyrics(ctx, q)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Source = source
	}
	out := []Candidate{}
	seen := map[string]bool{}
	for _, c := range rankCandidates(q, rows) {
		if c.Ref == "" || seen[c.Ref] {
			continue
		}
		seen[c.Ref] = true
		c.Source = source
		out = append(out, c)
		if len(out) == MaxResults {
			break
		}
	}
	return out, nil
}

func (s Service) Fetch(ctx context.Context, c Candidate) (*lyricdoc.Document, error) {
	p, err := s.Sources.Get(provider.ProviderID(c.Source))
	if err != nil {
		return nil, err
	}
	// One deadline covers quota waiting, retries and every fallback/download.
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = provider.WithStopRestrictedRetries(ctx)
	doc, err := p.FetchLyrics(ctx, c)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := lyricdoc.Validate(doc); err != nil {
		return nil, err
	}
	return doc, nil
}
