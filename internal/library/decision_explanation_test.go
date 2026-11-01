package library

import "github.com/norhua/sonexa/internal/domain"

func decisionHasReason(evidence *domain.DecisionEvidence, code string) bool {
	if evidence == nil {
		return false
	}
	for _, reason := range evidence.Reasons {
		if reason.Code == code && reason.Message != "" {
			return true
		}
	}
	return false
}
