// Package lyrics owns lyric search validation, scoring and document assembly.
// Concrete upstream packages implement its consumer-owned Provider contract.
package lyrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

const MaxResults = 20
const MaxTextBytes = 2 << 20

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

// Channel distinguishes absent optional content from a protocol/decoder error.
type Channel struct {
	Track       *Track       `json:"-"`
	Text        string       `json:"text,omitempty"`
	Status      string       `json:"status"`
	Origin      string       `json:"origin"`
	SyncStatus  string       `json:"syncStatus"`
	Message     string       `json:"message,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Err         error        `json:"-"`
}
type Document struct {
	Text         string       `json:"text"`
	Format       string       `json:"format"`
	Rows         []PreviewRow `json:"rows"`
	OffsetMS     int64        `json:"offsetMs"`
	Additional   []*Channel   `json:"additional,omitempty"`
	Original     *Channel     `json:"original,omitempty"`
	Translation  *Channel     `json:"translation,omitempty"`
	Romanization *Channel     `json:"romanization,omitempty"`
	WordTiming   string       `json:"wordTiming,omitempty"`
	Warnings     []string     `json:"warnings,omitempty"`
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

func (s Service) Fetch(ctx context.Context, c Candidate) (*Document, error) {
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
	payload, err := p.FetchLyrics(ctx, c)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return DocumentFromPayload(payload)
}

func DocumentFromPayload(p *Payload) (*Document, error) {
	if p == nil {
		return nil, fmt.Errorf("歌词来源未返回正文")
	}
	original := p.Original
	if original == nil && p.Text != "" {
		original = TextChannel(p.Text, nil)
	}
	warnings := append([]string(nil), p.Warnings...)
	timing := p.WordTiming
	if p.WordOriginal != nil {
		word := normalizeChannel(p.WordOriginal)
		if word.Status == "available" && hasWords(word.Track) {
			original = word
		} else if word.Status == "error" {
			timing = "error"
			warnings = append(warnings, "逐字原文解析失败："+word.Message)
		} else if original == nil || normalizeChannel(original).Status != "available" {
			original = word
		}
	}
	return buildDocument(original, p.Translation, p.Romanization, p.Additional, timing, warnings)
}
