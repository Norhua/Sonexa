package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	"github.com/norhua/sonexa/internal/matching"
)

// ItemProfile is shared by the Runner and transactional problem lifecycle.
// Internal callers lacking a parsed profile still have explicit canonical units;
// file imports retain their original parsing/precision/failure evidence.
func ItemProfile(item *ImportItem) matching.InputProfile {
	if item.Profile != nil {
		return *item.Profile
	}
	text := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	p := matching.NewInputProfile(text(item.InTitle), text(item.InArtist), text(item.InAlbum))
	for _, field := range []struct {
		value  *int
		target *matching.NumberInput
	}{
		{item.InYear, &p.Year}, {item.InDiscNo, &p.Disc}, {item.InTrackNo, &p.Track}, {item.InDurationMS, &p.Duration},
	} {
		if field.value == nil {
			continue
		}
		n := field.target
		n.Raw, n.Value, n.Precision, n.State = strconv.Itoa(*field.value), int64(*field.value), 1, matching.ValueKnown
		if n.Value <= 0 {
			n.State, n.Error = matching.ValueInvalid, "nonpositive_value"
		}
	}
	return p
}

// MarkReviewWithProblem preserves source facts only for this current input.
// It has the same row CAS as MarkReviewAtRevision; membership is in that tx.
func (s *Store) MarkReviewWithProblem(itemID, revision int64, reason ReviewReason, candidatesJSON, variantsJSON, evidence string, input matching.ProblemInput) error {
	return s.markReviewAtRevision(itemID, revision, reason, candidatesJSON, variantsJSON, []string{evidence}, &input)
}

func syncProblemItemTx(tx *sql.Tx, itemID int64, supplied *matching.ProblemInput) error {
	item, err := scanItem(tx.QueryRow("SELECT "+itemColumns+" FROM import_item WHERE id=?", itemID))
	if err != nil {
		return err
	}
	if item.Status.IsTerminal() {
		return ErrPlanStale
	}
	input := matching.ProblemInput{Profile: ItemProfile(item)}
	if supplied != nil {
		if !reflect.DeepEqual(input.Profile, supplied.Profile) {
			return ErrPlanStale
		}
		input = *supplied
	}
	input, err = refreshProblemLocalFactsTx(tx, input)
	if err != nil {
		return err
	}
	var problemID int64
	err = tx.QueryRow("SELECT problem_id FROM review_problem_item WHERE item_id=? AND active=1", itemID).Scan(&problemID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if problemID != 0 {
		// State changes/retries keep their current work scope. Rebuild from
		// live members, not from an anchor retaining a finished source row.
		if supplied == nil {
			var body string
			if err := tx.QueryRow("SELECT proof_json FROM review_problem_item WHERE problem_id=? AND item_id=?", problemID, itemID).Scan(&body); err != nil {
				return err
			}
			var previous matching.ProblemInput
			if err := json.Unmarshal([]byte(body), &previous); err != nil {
				return err
			}
			if reflect.DeepEqual(previous.Profile, input.Profile) {
				input = previous
			}
		}
		body, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE review_problem_item SET input_revision=?,proof_json=?,revision=revision+1,updated_at=? WHERE problem_id=? AND item_id=? AND active=1", item.Revision, string(body), time.Now().UTC(), problemID, itemID); err != nil {
			return err
		}
		return rebuildProblemTx(tx, problemID)
	}
	// Queued/matching rows acquire a problem only after review or exhausted
	// failure. Existing members remain associated across retry/pause/restart.
	if item.Status != StatusReview && item.Status != StatusFailed {
		return nil
	}
	var after int64
	for {
		page, err := findProblemCandidatesTx(context.Background(), tx, input, after, 100)
		if err != nil {
			return err
		}
		for _, candidate := range page.Problems {
			after = candidate.ID
			anchor, err := currentProblemAnchorTx(tx, candidate.ID)
			if err != nil {
				return err
			}
			if anchor == nil {
				continue
			}
			if _, _, equivalent := matching.ExtendProblemAnchor(*anchor, input); equivalent {
				problemID = candidate.ID
				break
			}
		}
		if problemID != 0 || !page.Truncated {
			break
		}
	}
	if problemID == 0 {
		problemID, err = createProblemTx(tx, input, false)
		if err != nil {
			return err
		}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec("INSERT INTO review_problem_item(problem_id,item_id,input_revision,proof_json,created_at,updated_at) VALUES(?,?,?,?,?,?)", problemID, item.ID, item.Revision, string(body), now, now); err != nil {
		return err
	}
	return rebuildProblemTx(tx, problemID)
}

func createProblemTx(tx *sql.Tx, input matching.ProblemInput, isolated bool) (int64, error) {
	anchor, proof := matching.NewProblemAnchor(input)
	anchor.Isolated = isolated
	anchorJSON, err := json.Marshal(anchor)
	if err != nil {
		return 0, err
	}
	proofJSON, err := json.Marshal(proof)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	result, err := tx.Exec("INSERT INTO review_problem(rule_version,anchor_json,proof_json,created_at,updated_at) VALUES(?,?,?,?,?)", matching.MetadataRuleVersion, string(anchorJSON), string(proofJSON), now, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func currentProblemAnchorTx(tx *sql.Tx, problemID int64) (*matching.ProblemAnchor, error) {
	var isolated bool
	if err := tx.QueryRow("SELECT COALESCE(json_extract(anchor_json,'$.isolated'),0) FROM review_problem WHERE id=? AND status='active'", problemID).Scan(&isolated); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT m.proof_json,m.input_revision,i.revision,i.status FROM review_problem_item m
 JOIN import_item i ON i.id=m.item_id WHERE m.problem_id=? AND m.active=1 ORDER BY m.item_id`, problemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	inputs := []matching.ProblemInput{}
	for rows.Next() {
		var body string
		var expected, current int64
		var status ItemStatus
		if err := rows.Scan(&body, &expected, &current, &status); err != nil {
			return nil, err
		}
		if expected != current || !activeItemStatus(status) {
			return nil, ErrPlanStale
		}
		var proofInput matching.ProblemInput
		if err := json.Unmarshal([]byte(body), &proofInput); err != nil {
			return nil, err
		}
		input, err := refreshProblemLocalFactsTx(tx, proofInput)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return nil, nil
	}
	anchor, _ := matching.NewProblemAnchor(inputs...)
	anchor.Isolated = isolated
	return &anchor, nil
}

// Persisted source facts are diagnostic views, not permission to reuse a
// removed/rebound local identity. Refresh validity before extending a proof.
func refreshProblemLocalFactsTx(tx *sql.Tx, input matching.ProblemInput) (matching.ProblemInput, error) {
	if input.Pool == nil {
		return input, nil
	}
	for i := range input.Pool.Targets {
		target := &input.Pool.Targets[i]
		for _, fact := range target.Facts {
			if fact.Kind != "current" {
				continue
			}
			var trackRevision, releaseRevision int64
			var source, ref, releaseSource, releaseRef *string
			err := tx.QueryRow(`SELECT t.revision,r.revision,t.match_source,t.match_ref,r.provider,r.provider_ref
 FROM track t JOIN release r ON r.id=t.release_id WHERE t.id=?`, fact.LocalTrackID).Scan(&trackRevision, &releaseRevision, &source, &ref, &releaseSource, &releaseRef)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return input, err
			}
			if errors.Is(err, sql.ErrNoRows) || source == nil || ref == nil || releaseSource == nil || releaseRef == nil ||
				*source != string(target.Identity.Track.Provider) || *ref != target.Identity.Track.Ref || *releaseSource != string(target.Identity.Release.Provider) || *releaseRef != target.Identity.Release.Ref || trackRevision != fact.TrackRevision || releaseRevision != fact.ReleaseRevision {
				target.Issues = append(target.Issues, matching.DecisionReason{Code: "stale_problem_local_facts"})
			}
		}
	}
	return input, nil
}

func rebuildProblemTx(tx *sql.Tx, problemID int64) error {
	anchor, err := currentProblemAnchorTx(tx, problemID)
	if err != nil {
		return err
	}
	if anchor == nil {
		_, err = tx.Exec("UPDATE review_problem SET status='closed',revision=revision+1,closed_at=?,updated_at=? WHERE id=? AND status='active'", time.Now().UTC(), time.Now().UTC(), problemID)
		return err
	}
	body, err := json.Marshal(anchor)
	if err != nil {
		return err
	}
	proof, err := json.Marshal(matching.ProveProblem(anchor.Inputs))
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE review_problem SET anchor_json=?,proof_json=?,revision=revision+1,updated_at=? WHERE id=? AND status='active'", string(body), string(proof), time.Now().UTC(), problemID)
	return err
}

func finishProblemItemTx(tx *sql.Tx, itemID int64) error {
	var problemID, revision int64
	err := tx.QueryRow(`SELECT p.id,p.revision FROM review_problem_item m JOIN review_problem p ON p.id=m.problem_id
 WHERE m.item_id=? AND m.active=1`, itemID).Scan(&problemID, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE review_problem_item SET active=0,revision=revision+1,updated_at=? WHERE problem_id=? AND item_id=?", time.Now().UTC(), problemID, itemID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE import_item SET resolution_problem_id=?,resolution_problem_revision=? WHERE id=?", problemID, revision, itemID); err != nil {
		return err
	}
	return rebuildProblemTx(tx, problemID)
}
