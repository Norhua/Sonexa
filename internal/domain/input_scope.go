package domain

const InputScopeVersion = 1

type InputScopeKind string

const (
	InputScopeSingle  InputScopeKind = "single"
	InputScopeProblem InputScopeKind = "problem"
)

// InputScope lists exactly the rows an operation may finish. A single-row
// operation can retain its enclosing problem as a guard without adopting its
// other members. Problem operations must list the complete active membership.
type InputScope struct {
	Version int                `json:"version"`
	Kind    InputScopeKind     `json:"kind"`
	Problem *InputProblemScope `json:"problem,omitempty"`
	Members []InputScopeMember `json:"members"`
}

type InputProblemScope struct {
	ID                int64 `json:"id"`
	Revision          int64 `json:"revision"`
	QueryRevision     int64 `json:"queryRevision"`
	CandidateRevision int64 `json:"candidateRevision"`
	RuleVersion       int   `json:"ruleVersion"`
	ProofVersion      int   `json:"proofVersion"`
	// Proof changes share the problem/member revisions. This digest also
	// binds the full membership and persisted AND currently rechecked facts.
	EvidenceHash string `json:"evidenceHash"`
}

type InputScopeMember struct {
	ItemID         int64  `json:"itemId"`
	BatchID        int64  `json:"batchId"`
	RowIndex       int    `json:"rowIndex"`
	InputRevision  int64  `json:"inputRevision"`
	MemberRevision int64  `json:"memberRevision"`
	State          string `json:"state"`
	ProfileVersion int    `json:"profileVersion"`
	EvidenceHash   string `json:"evidenceHash"`
}
