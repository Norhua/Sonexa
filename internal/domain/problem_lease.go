package domain

// ProblemLease protects an active review interaction, not a historical choice.
// The opaque token never replaces the input/problem revisions of an operation.
type ProblemLease struct {
	Token     string `json:"token"`
	ProblemID int64  `json:"problemId"`
	ExpiresAt int64  `json:"expiresAt"` // Unix milliseconds
}
