package domain

// DecisionReason is diagnostic evidence for this operation, never permission
// to reuse a prior selection. Message is user-facing; Detail retains diagnostics.
type DecisionReason struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Detail  string `json:"detail,omitempty"`
	Message string `json:"message,omitempty"`
}

// DecisionEvidence travels with the sealed plan and its atomic result. It is
// not a candidate pool, equivalence proof or historical identity lookup key.
type DecisionEvidence struct {
	RuleVersion int              `json:"ruleVersion"`
	Reasons     []DecisionReason `json:"reasons"`
}
