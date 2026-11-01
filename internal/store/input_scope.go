package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
)

func inputEvidenceHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func readInputMemberTx(tx *sql.Tx, id int64) (domain.InputScopeMember, int64, error) {
	var out domain.InputScopeMember
	item, err := scanItem(tx.QueryRow("SELECT "+itemColumns+" FROM import_item WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return out, 0, ErrPlanStale
	}
	if err != nil {
		return out, 0, err
	}
	profile := ItemProfile(item)
	if !activeItemStatus(item.Status) || profile.RuleVersion != matching.MetadataRuleVersion {
		return out, 0, ErrPlanStale
	}
	out = domain.InputScopeMember{ItemID: id, BatchID: item.BatchID, RowIndex: item.RowIndex,
		InputRevision: item.Revision, State: string(item.Status), ProfileVersion: profile.RuleVersion}
	var problemID, expected int64
	var proofBody string
	err = tx.QueryRow("SELECT problem_id,revision,input_revision,proof_json FROM review_problem_item WHERE item_id=? AND active=1", id).
		Scan(&problemID, &out.MemberRevision, &expected, &proofBody)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, 0, err
	}
	if problemID != 0 {
		var input matching.ProblemInput
		if err := json.Unmarshal([]byte(proofBody), &input); err != nil {
			return out, 0, err
		}
		if expected != item.Revision || !reflect.DeepEqual(input.Profile, profile) {
			return out, 0, ErrPlanStale
		}
	}
	out.EvidenceHash, err = inputEvidenceHash(struct {
		Profile    matching.InputProfile
		Proof      string
		Candidates *string
		Evidence   *string
	}{profile, proofBody, item.Candidates, item.ReviewEvidence})
	return out, problemID, err
}

func readInputProblemTx(tx *sql.Tx, id int64) (*domain.InputProblemScope, []domain.InputScopeMember, matching.ProblemProof, error) {
	var proof matching.ProblemProof
	scope, err := readProblemScopeTx(tx, id)
	if err != nil {
		return nil, nil, proof, err
	}
	out := &domain.InputProblemScope{ID: id, Revision: scope.Revision, QueryRevision: scope.QueryRevision, CandidateRevision: scope.CandidateRevision}
	var anchorBody, proofBody string
	var candidates, query *string
	if err := tx.QueryRow("SELECT rule_version,anchor_json,proof_json,candidate_facts,query_json FROM review_problem WHERE id=?", id).
		Scan(&out.RuleVersion, &anchorBody, &proofBody, &candidates, &query); err != nil {
		return nil, nil, proof, err
	}
	var storedAnchor matching.ProblemAnchor
	var storedProof matching.ProblemProof
	if err := json.Unmarshal([]byte(anchorBody), &storedAnchor); err != nil {
		return nil, nil, proof, err
	}
	if err := json.Unmarshal([]byte(proofBody), &storedProof); err != nil {
		return nil, nil, proof, err
	}
	out.ProofVersion = storedProof.Version
	if out.RuleVersion != matching.MetadataRuleVersion || storedProof.RuleVersion != matching.MetadataRuleVersion ||
		storedProof.Version != matching.ProblemProofVersion || storedAnchor.Version != matching.ProblemProofVersion {
		return nil, nil, proof, ErrPlanStale
	}
	members := make([]domain.InputScopeMember, 0, len(scope.Members))
	for _, member := range scope.Members {
		current, problemID, err := readInputMemberTx(tx, member.ItemID)
		if err != nil {
			return nil, nil, proof, err
		}
		if problemID != id {
			return nil, nil, proof, ErrPlanStale
		}
		members = append(members, current)
	}
	// Refresh local binding/fact validity and re-prove the entire group. A
	// saved exact proof cannot survive deletion/rebinding merely because the
	// queue itself did not change revision.
	anchor, err := currentProblemAnchorTx(tx, id)
	if err != nil {
		return nil, nil, proof, err
	}
	if anchor == nil {
		return nil, nil, proof, ErrPlanStale
	}
	proof = matching.ProveProblem(anchor.Inputs)
	out.EvidenceHash, err = inputEvidenceHash(struct {
		Scope       ProblemScope
		Members     []domain.InputScopeMember
		SavedAnchor string
		SavedProof  string
		Candidates  *string
		Query       *string
		Anchor      *matching.ProblemAnchor
		Proof       matching.ProblemProof
	}{scope, members, anchorBody, proofBody, candidates, query, anchor, proof})
	return out, members, proof, err
}

func readSingleInputScopeTx(tx *sql.Tx, itemID, revision int64) (*domain.InputScope, error) {
	member, problemID, err := readInputMemberTx(tx, itemID)
	if err != nil {
		return nil, err
	}
	if revision > 0 && revision != member.InputRevision {
		return nil, ErrPlanStale
	}
	out := &domain.InputScope{Version: domain.InputScopeVersion, Kind: domain.InputScopeSingle, Members: []domain.InputScopeMember{member}}
	if problemID != 0 {
		out.Problem, _, _, err = readInputProblemTx(tx, problemID)
	}
	return out, err
}

// ReadSingleInputScope captures only the selected row, even inside a multi-row
// problem. Revision zero is allowed only for legacy prepare-at-current APIs.
func (s *Store) ReadSingleInputScope(itemID, revision int64) (*domain.InputScope, error) {
	if itemID <= 0 || revision < 0 {
		return nil, ErrPlanStale
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return readSingleInputScopeTx(tx, itemID, revision)
}

// ReadProblemSingleInputScope captures exactly one selected original row while
// validating the whole displayed problem and the current lease holder.
func (s *Store) ReadProblemSingleInputScope(scope ProblemScope, itemID int64) (*domain.InputScope, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := checkProblemScopeTx(tx, scope); err != nil {
		return nil, err
	}
	if err := s.checkProblemAccessTx(tx, scope.ProblemID, scope.LeaseToken); err != nil {
		return nil, err
	}
	for _, member := range scope.Members {
		if member.ItemID != itemID {
			continue
		}
		out, err := readSingleInputScopeTx(tx, itemID, member.InputRevision)
		if err != nil {
			return nil, err
		}
		out.LeaseToken = scope.LeaseToken
		return out, nil
	}
	return nil, ErrPlanStale
}

// ReadProblemInputScope upgrades an already displayed, complete problem token;
// it never reads the latest membership in place of a stale supplied token.
func (s *Store) ReadProblemInputScope(scope ProblemScope) (*domain.InputScope, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := checkProblemScopeTx(tx, scope); err != nil {
		return nil, err
	}
	if err := s.checkProblemLeaseTxOptional(tx, scope.ProblemID, scope.LeaseToken); err != nil {
		return nil, err
	}
	problem, members, proof, err := readInputProblemTx(tx, scope.ProblemID)
	if err != nil {
		return nil, err
	}
	if len(members) > 1 && proof.Kind == matching.ProblemIndependent {
		return nil, ErrPlanStale
	}
	return &domain.InputScope{Version: domain.InputScopeVersion, Kind: domain.InputScopeProblem, Problem: problem, Members: members, LeaseToken: scope.LeaseToken}, nil
}

func checkInputScopeTx(tx *sql.Tx, scope *domain.InputScope) error {
	if scope == nil || scope.Version != domain.InputScopeVersion || len(scope.Members) == 0 {
		return ErrPlanStale
	}
	var current *domain.InputScope
	var err error
	switch scope.Kind {
	case domain.InputScopeSingle:
		if len(scope.Members) != 1 {
			return ErrPlanStale
		}
		current, err = readSingleInputScopeTx(tx, scope.Members[0].ItemID, scope.Members[0].InputRevision)
	case domain.InputScopeProblem:
		if scope.Problem == nil {
			return ErrPlanStale
		}
		var proof matching.ProblemProof
		current = &domain.InputScope{Version: domain.InputScopeVersion, Kind: domain.InputScopeProblem}
		current.Problem, current.Members, proof, err = readInputProblemTx(tx, scope.Problem.ID)
		if err == nil && len(current.Members) > 1 && proof.Kind == matching.ProblemIndependent {
			return ErrPlanStale
		}
	default:
		return ErrPlanStale
	}
	if err != nil {
		return err
	}
	current.LeaseToken = scope.LeaseToken
	if !reflect.DeepEqual(scope, current) {
		return ErrPlanStale
	}
	return nil
}

func (s *Store) CheckInputScope(scope *domain.InputScope) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return checkInputScopeTx(tx, scope)
}

func admissionInputScopeTx(tx *sql.Tx, seed domain.AdmissionSeed, baseline *domain.AdmissionBaseline) error {
	if seed.InputScope == nil {
		if seed.ImportItemID != 0 || seed.ItemRevision != 0 || seed.Operation == domain.AdmissionSkipExisting {
			return ErrPlanStale
		}
		return nil
	}
	if err := checkInputScopeTx(tx, seed.InputScope); err != nil {
		return err
	}
	if seed.ImportItemID != 0 || seed.ItemRevision != 0 {
		if seed.InputScope.Kind != domain.InputScopeSingle || len(seed.InputScope.Members) != 1 ||
			seed.ImportItemID != seed.InputScope.Members[0].ItemID || seed.ItemRevision != seed.InputScope.Members[0].InputRevision {
			return ErrPlanStale
		}
		baseline.ItemRevision, baseline.ItemState = seed.ItemRevision, seed.InputScope.Members[0].State
	}
	if len(seed.InputScope.Members) > 1 && seed.Snapshot.Track.Identity.Ref == "" {
		return ErrPlanStale // 无需匹配必须明确选择一行，不可推广。
	}
	if seed.MoveReleaseID != 0 || seed.ProposalID != 0 || seed.EditJSON != "" {
		return ErrPlanStale
	}
	baseline.InputScope = seed.InputScope
	return nil
}
