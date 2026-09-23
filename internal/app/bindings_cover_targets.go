package app

import "github.com/norhua/sonexa/internal/covers"

type CoverTargetQuery struct {
	ReleaseID int64  `json:"releaseId"`
	Provider  string `json:"provider"`
	Ref       string `json:"ref"`
	GroupRef  string `json:"groupRef"`
	Usage     string `json:"usage"`
	Revision  int64  `json:"revision"`
}

func (a *App) CoverTargetOutcomes(queries []CoverTargetQuery) []string {
	a.mu.RLock()
	svc := a.coverSvc
	a.mu.RUnlock()
	out := make([]string, len(queries))
	for i, q := range queries {
		out[i] = "error"
		if svc == nil {
			continue
		}
		target := covers.Target{ReleaseID: q.ReleaseID, Provider: q.Provider, Ref: q.Ref, GroupRef: q.GroupRef}
		results := svc.Outcomes([]string{target.Key(covers.Usage(q.Usage), q.Revision)})
		if len(results) > 0 && results[0] != covers.OutcomeUnknown {
			out[i] = string(results[0])
		}
	}
	return out
}
