package lyrics

import "github.com/norhua/sonexa/internal/lyricdoc"

// ChannelSummary deliberately contains neither lyrics nor per-line inference
// associations. It is safe to include in progress events for unselected items.
type ChannelSummary struct {
	Status      string              `json:"status"`
	Origin      string              `json:"origin"`
	SyncStatus  string              `json:"syncStatus"`
	Diagnostics []DiagnosticSummary `json:"diagnostics,omitempty"`
}

type DiagnosticSummary struct {
	Code        string `json:"code"`
	RuleVersion string `json:"ruleVersion,omitempty"`
	Layer       int    `json:"layer,omitempty"`
	Count       int    `json:"count,omitempty"`
}

func SummarizeChannel(ch *lyricdoc.Channel) ChannelSummary {
	if ch == nil {
		return ChannelSummary{Status: "unknown", Origin: "unknown", SyncStatus: "unknown"}
	}
	s := ChannelSummary{Status: ch.Status, Origin: ch.Origin, SyncStatus: ch.SyncStatus}
	for _, d := range ch.Diagnostics {
		diagnostic := DiagnosticSummary{Code: d.Code}
		if d.Inference != nil {
			diagnostic.RuleVersion, diagnostic.Layer, diagnostic.Count = d.Inference.RuleVersion, d.Inference.Layer, d.Inference.Count
		}
		s.Diagnostics = append(s.Diagnostics, diagnostic)
	}
	return s
}

type CandidateSummary struct {
	Candidate       Candidate      `json:"candidate"`
	FetchState      string         `json:"fetchState"`
	Prefetch        bool           `json:"prefetch"`
	Format          string         `json:"format,omitempty"`
	Translation     ChannelSummary `json:"translation"`
	Romanization    ChannelSummary `json:"romanization"`
	WordTiming      string         `json:"wordTiming"`
	DocumentVersion uint64         `json:"documentVersion"`
	ErrorKind       string         `json:"errorKind,omitempty"`
	StopReason      string         `json:"stopReason,omitempty"`
}

// CopySummary prevents snapshots and emitted events from aliasing mutable
// session state. The complete source document is never copied into a summary.
func CopySummary(s CandidateSummary) CandidateSummary {
	s.Candidate.MatchNotes = append([]string(nil), s.Candidate.MatchNotes...)
	s.Candidate.MatchTerms = append([]ScoreTerm(nil), s.Candidate.MatchTerms...)
	s.Candidate.ScoreTerms = append([]ScoreTerm(nil), s.Candidate.ScoreTerms...)
	if s.Candidate.TotalScore != nil {
		total := *s.Candidate.TotalScore
		s.Candidate.TotalScore = &total
	}
	s.Translation.Diagnostics = append([]DiagnosticSummary(nil), s.Translation.Diagnostics...)
	s.Romanization.Diagnostics = append([]DiagnosticSummary(nil), s.Romanization.Diagnostics...)
	return s
}
