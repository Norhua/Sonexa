package importer

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestRunnerSuccessfulDecisionEvidenceAndLogs(t *testing.T) {
	for _, mode := range []string{"new", "local_existing", "remote_existing"} {
		t.Run(mode, func(t *testing.T) {
			st := openStore(t)
			c := mbCandidate("explanation-track", "Song", "Artist", "Album")
			if mode != "new" {
				_, c = seedLocalRunnerTrack(t, st, mode == "local_existing", false)
			}
			searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Song": {c}}}
			batch := enqueueOne(t, st, "Song（译名：歌曲）", "Artist", "Album")
			events := runToCompletion(t, newRunner(st, searcher), batch)
			var body string
			var status store.ItemStatus
			if err := st.DB().QueryRow("SELECT status,review_evidence FROM import_item WHERE batch_id=?", batch).Scan(&status, &body); err != nil {
				t.Fatal(err)
			}
			var evidence domain.DecisionEvidence
			if err := json.Unmarshal([]byte(body), &evidence); err != nil || evidence.RuleVersion != matching.MetadataRuleVersion {
				t.Fatal(body, err)
			}
			codes := map[string]bool{}
			for _, reason := range evidence.Reasons {
				codes[reason.Code] = true
				if reason.Message == "" {
					t.Fatal(reason)
				}
			}
			if !codes["shared_complete_name"] || !codes["automatic_target"] {
				t.Fatal(evidence)
			}
			wantStatus := store.StatusImported
			if mode != "new" {
				wantStatus = store.StatusSkipped
				if !codes["prefer_owned_compatible_track"] || !codes["exact_existing_track"] {
					t.Fatal(evidence)
				}
			}
			if status != wantStatus {
				t.Fatal(status)
			}
			summary := matching.DecisionSummary(&evidence)
			found := false
			for _, event := range events {
				found = found || strings.Contains(event.Line, summary)
			}
			if !found || !strings.Contains(summary, "可靠异写") {
				t.Fatal(events, summary)
			}
			var resultBody string
			if err := st.DB().QueryRow("SELECT result_json FROM operation_result ORDER BY committed_at DESC LIMIT 1").Scan(&resultBody); err != nil {
				t.Fatal(err)
			}
			var result domain.AdmissionResult
			if err := json.Unmarshal([]byte(resultBody), &result); err != nil || !reflect.DeepEqual(result.Decision, &evidence) {
				t.Fatal(resultBody, body, err)
			}
		})
	}
}

func TestAutomaticDecisionEvidenceRollbackAndPlanSeal(t *testing.T) {
	st := openStore(t)
	seedLocalRunnerTrack(t, st, true, false)
	batch := enqueueOne(t, st, "Song（译名：歌曲）", "Artist", "Album")
	item, err := st.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	runner := newRunner(st, &recordingSearcher{})
	selection, err := runner.library.PrepareLocalExisting(context.Background(), *inputFromItem(item).Profile, item.ID, item.Revision)
	if err != nil || selection == nil || selection.Plan.Seed.Decision == nil {
		t.Fatal(selection, err)
	}
	tampered := *selection.Plan
	seed := tampered.Seed
	decision := *seed.Decision
	decision.Reasons = []domain.DecisionReason{{Code: "forged"}}
	seed.Decision = &decision
	tampered.Seed = seed
	if _, err := st.CommitAdmission(tampered, domain.AdmissionApproval{}); err == nil {
		t.Fatal("unsealed explanation accepted")
	}
	if _, err := st.DB().Exec(`CREATE TRIGGER reject_decision BEFORE UPDATE OF review_evidence ON import_item BEGIN SELECT RAISE(ABORT,'evidence write failed'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.library.CommitAdmission(context.Background(), selection.Plan.ID, selection.Plan.Hash, domain.AdmissionApproval{}); err == nil {
		t.Fatal("partial commit")
	}
	got, err := st.GetItem(item.ID)
	if err != nil || got.Status != store.StatusMatching || got.ReviewEvidence != nil {
		t.Fatal(got, err)
	}
	if prior, err := st.AdmissionResult(selection.Plan.ID, selection.Plan.Hash); err != nil || prior != nil {
		t.Fatal(prior, err)
	}
	if _, err := st.DB().Exec("DROP TRIGGER reject_decision"); err != nil {
		t.Fatal(err)
	}
	first, err := runner.library.CommitAdmission(context.Background(), selection.Plan.ID, selection.Plan.Hash, domain.AdmissionApproval{})
	if err != nil {
		t.Fatal(err)
	}
	again, err := runner.library.CommitAdmission(context.Background(), selection.Plan.ID, selection.Plan.Hash, domain.AdmissionApproval{})
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal(first, again, err)
	}
}
