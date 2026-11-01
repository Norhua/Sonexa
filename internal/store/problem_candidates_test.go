package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
)

func problemRecallFixture(t *testing.T, s *Store, input matching.ProblemInput) int64 {
	t.Helper()
	anchor, proof := matching.NewProblemAnchor(input)
	body, _ := json.Marshal(anchor)
	proofJSON, _ := json.Marshal(proof)
	result, err := s.db.Exec(`INSERT INTO review_problem(rule_version,anchor_json,proof_json,created_at,updated_at) VALUES(?,?,?,datetime('now'),datetime('now'))`, matching.MetadataRuleVersion, string(body), string(proofJSON))
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProblemCandidateRecallIsNotEquivalence(t *testing.T) {
	s := openTestStore(t)
	input := matching.ProblemInput{Profile: matching.NewInputProfile("Song", "Artist", "Album")}
	alias := matching.ProblemInput{Profile: matching.NewInputProfile("歌曲（原名：Song）", "Artist", "Album")}
	id := problemRecallFixture(t, s, alias)
	wrong := input
	wrong.Profile.Album = matching.ParseMetadataText("Different Album", matching.MetadataAlbum)
	wrongID := problemRecallFixture(t, s, wrong)
	closed := problemRecallFixture(t, s, input)
	if _, err := s.db.Exec("UPDATE review_problem SET status='closed',closed_at=datetime('now') WHERE id=?", closed); err != nil {
		t.Fatal(err)
	}
	old := problemRecallFixture(t, s, input)
	if _, err := s.db.Exec("UPDATE review_problem SET rule_version=999 WHERE id=?", old); err != nil {
		t.Fatal(err)
	}
	rows, err := s.FindProblemCandidates(context.Background(), input, 0, 100)
	if err != nil || rows.Truncated || len(rows.Problems) != 2 || rows.Problems[0].ID != id || rows.Problems[1].ID != wrongID {
		t.Fatal(rows, err)
	}
	for i, row := range rows.Problems {
		_, _, equivalent := matching.ExtendProblemAnchor(row.Anchor, input)
		if equivalent != (i == 0) {
			t.Fatal("blocking match was treated as proof", row, equivalent)
		}
	}
	empty := matching.ProblemInput{Profile: matching.NewInputProfile("", "", "")}
	rows, err = s.FindProblemCandidates(context.Background(), empty, 0, 100)
	if err != nil || len(rows.Problems) != 0 {
		t.Fatal("missing fields recalled arbitrary problems", rows, err)
	}
}

func TestProblemCandidateRecallPagesBeyondUIAndReportsErrors(t *testing.T) {
	s := openTestStore(t)
	input := matching.ProblemInput{Profile: matching.NewInputProfile("Song", "Artist", "Album")}
	// Many aliases exercise JSON parameters, not OR/variable depth limits.
	for i := range 1100 {
		input.Profile.Title.Aliases = append(input.Profile.Title.Aliases, matching.NameForm{Text: fmt.Sprintf("Alias %d", i), Basis: "fixture", Verified: true})
	}
	for range 103 {
		problemRecallFixture(t, s, input)
	}
	var ids []int64
	var after int64
	for {
		page, err := s.FindProblemCandidates(context.Background(), input, after, 17)
		if err != nil || len(page.Problems) == 0 {
			t.Fatal(page, err)
		}
		for _, row := range page.Problems {
			if row.ID <= after {
				t.Fatal("page repeated or skipped cursor", row.ID, after)
			}
			after = row.ID
			ids = append(ids, row.ID)
		}
		if !page.Truncated {
			break
		}
	}
	if len(ids) != 103 {
		t.Fatal("recall lost candidate problems", len(ids))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.FindProblemCandidates(ctx, input, 0, 10); err == nil {
		t.Fatal("cancelled read returned no match")
	}
	if _, err := s.db.Exec("ALTER TABLE review_problem RENAME TO unavailable_problems"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindProblemCandidates(context.Background(), input, 0, 10); err == nil {
		t.Fatal("failed read returned no match")
	}
}
