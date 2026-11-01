package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

type problemImpactKey struct {
	Kind   string `json:"kind"`
	Value  string `json:"value"`
	Prefix bool   `json:"prefix"`
}

func impactIdentity(kind string, id domain.Identity) []problemImpactKey {
	if id.Provider == "" || id.Ref == "" {
		return nil
	}
	body, _ := json.Marshal(id)
	return []problemImpactKey{{Kind: kind, Value: string(body)}}
}
func impactProfile(p matching.InputProfile) []problemImpactKey {
	var out []problemImpactKey
	for _, field := range []struct {
		kind string
		text matching.TextProfile
	}{{"title", p.Title}, {"artist", p.Artist.Text}, {"album", p.Album}} {
		for _, key := range matching.RecallTextForms(field.text) {
			out = append(out, problemImpactKey{field.kind, key, field.text.Truncated})
		}
	}
	return out
}
func impactCandidate(c provider.Candidate) []problemImpactKey {
	out := impactProfile(matching.NewInputProfile(deref(c.Title), deref(c.ArtistName), deref(c.AlbumTitle)))
	out = append(out, impactIdentity("track", domain.Identity{Provider: c.Provider, Ref: c.MatchRef})...)
	out = append(out, impactIdentity("release", domain.Identity{Provider: c.Provider, Ref: deref(c.ReleaseRef)})...)
	return out
}
func uniqueImpactKeys(keys []problemImpactKey) []problemImpactKey {
	seen := map[problemImpactKey]bool{}
	out := []problemImpactKey{}
	for _, key := range keys {
		if key.Value != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Value != b.Value {
			return a.Value < b.Value
		}
		return !a.Prefix && b.Prefix
	})
	return out
}
func rebuildProblemKeysTx(tx *sql.Tx, id int64) error {
	rows, err := tx.Query(`SELECT i.id,m.proof_json FROM review_problem_item m JOIN import_item i ON i.id=m.item_id WHERE m.problem_id=? AND m.active=1 ORDER BY i.id`, id)
	if err != nil {
		return err
	}
	var keys []problemImpactKey
	for rows.Next() {
		var itemID int64
		var body string
		if err := rows.Scan(&itemID, &body); err != nil {
			rows.Close()
			return err
		}
		var input matching.ProblemInput
		if err := json.Unmarshal([]byte(body), &input); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, impactProfile(input.Profile)...)
		if input.Pool != nil {
			for _, target := range input.Pool.Targets {
				for _, fact := range target.Facts {
					keys = append(keys, impactCandidate(fact.Metadata.Candidate)...)
				}
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var body *string
	if err := tx.QueryRow("SELECT candidate_facts FROM review_problem WHERE id=?", id).Scan(&body); err != nil {
		return err
	}
	if body != nil {
		var facts ProblemCandidateFacts
		if err := json.Unmarshal([]byte(*body), &facts); err != nil {
			return err
		}
		for _, m := range facts.Members {
			for _, c := range m.Facts.Candidates {
				keys = append(keys, impactCandidate(c.Candidate)...)
			}
		}
	}
	if _, err := tx.Exec("DELETE FROM review_problem_key WHERE problem_id=?", id); err != nil {
		return err
	}
	for _, key := range uniqueImpactKeys(keys) {
		if _, err := tx.Exec("INSERT INTO review_problem_key(problem_id,kind,value,prefix) VALUES(?,?,?,?)", id, key.Kind, key.Value, key.Prefix); err != nil {
			return err
		}
	}
	return nil
}

type libraryBefore struct {
	RelationNames                          []string
	Title, Artist, Album, AlbumArtist      string
	Source, Ref, ReleaseSource, ReleaseRef string
	TrackSnapshot, ReleaseSnapshot         string
}

func (b libraryBefore) keys() ([]problemImpactKey, error) {
	keys := impactProfile(matching.NewInputProfile(b.Title, b.Artist, b.Album))
	for _, name := range b.RelationNames {
		keys = append(keys, impactProfile(matching.NewInputProfile("", name, ""))...)
	}
	keys = append(keys, impactProfile(matching.NewInputProfile("", b.AlbumArtist, ""))...)
	keys = append(keys, impactIdentity("track", domain.Identity{Provider: domain.ProviderID(b.Source), Ref: b.Ref})...)
	keys = append(keys, impactIdentity("release", domain.Identity{Provider: domain.ProviderID(b.ReleaseSource), Ref: b.ReleaseRef})...)
	var t domain.TrackSnapshot
	var r domain.ReleaseSnapshot
	if b.TrackSnapshot != "" {
		if err := json.Unmarshal([]byte(b.TrackSnapshot), &t); err != nil {
			return nil, err
		}
	}
	if b.ReleaseSnapshot != "" {
		if err := json.Unmarshal([]byte(b.ReleaseSnapshot), &r); err != nil {
			return nil, err
		}
	}
	keys = append(keys, impactCandidate(matching.SnapshotCandidate(&t, &r))...)
	return keys, nil
}

// ScheduleLibraryRechecks consumes at most limit changed tracks, preserving old
// identities/names across deletes and rebinds. Reverse keys are only impact
// hints, never evidence that an input can be skipped. Unrelated UI notifications
// with no changed facts cause no queue writes.
func (s *Store) ScheduleLibraryRechecks(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		limit = 32
	}
	count := 0
	err := s.withTx(func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT track_id FROM review_library_dirty ORDER BY track_id LIMIT ?", limit)
		if err != nil {
			return err
		}
		ids, err := readRecallIDs(rows)
		if err != nil {
			return err
		}
		count = len(ids)
		for _, id := range ids {
			var after int64
			if err := tx.QueryRow("SELECT after_problem FROM review_library_dirty WHERE track_id=?", id).Scan(&after); err != nil {
				return err
			}
			var keys []problemImpactKey
			rows, err := tx.QueryContext(ctx, "SELECT body FROM review_library_before WHERE track_id=?", id)
			if err != nil {
				return err
			}
			for rows.Next() {
				var raw string
				if err := rows.Scan(&raw); err != nil {
					rows.Close()
					return err
				}
				var before libraryBefore
				if err := json.Unmarshal([]byte(raw), &before); err != nil {
					rows.Close()
					return err
				}
				next, err := before.keys()
				if err != nil {
					rows.Close()
					return err
				}
				keys = append(keys, next...)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			record, err := readLocalRecordTx(ctx, tx, id)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			if err == nil {
				for _, text := range localRecordTexts(record) {
					for _, name := range matching.RecallTextForms(matching.ParseMetadataText(text.text, matching.MetadataField(text.field))) {
						keys = append(keys, problemImpactKey{Kind: text.field, Value: name})
					}
				}
				if record.TrackIdentity != nil {
					keys = append(keys, impactIdentity("track", *record.TrackIdentity)...)
				}
				if record.ReleaseIdentity != nil {
					keys = append(keys, impactIdentity("release", *record.ReleaseIdentity)...)
				}
			}
			body, err := json.Marshal(uniqueImpactKeys(keys))
			if err != nil {
				return err
			}
			rows, err = tx.QueryContext(ctx, `WITH changed AS (SELECT json_extract(value,'$.kind') kind,json_extract(value,'$.value') value FROM json_each(?))
 SELECT DISTINCT k.problem_id FROM changed c JOIN review_problem_key k ON k.kind=c.kind
 JOIN review_problem p ON p.id=k.problem_id AND p.status='active'
 WHERE k.problem_id>? AND (k.value=c.value OR (k.prefix=1 AND c.value>=k.value AND c.value<k.value||char(1114111)))
 ORDER BY k.problem_id LIMIT 101`, string(body), after)
			if err != nil {
				return err
			}
			problems, err := readRecallIDs(rows)
			if err != nil {
				return err
			}
			more := len(problems) > 100
			if more {
				problems = problems[:100]
			}
			for _, problem := range problems {
				if err := queueProblemRecheckTx(tx, problem); err != nil {
					return err
				}
			}
			if more {
				if _, err := tx.Exec("UPDATE review_library_dirty SET after_problem=? WHERE track_id=?", problems[len(problems)-1], id); err != nil {
					return err
				}
				count = limit // more durable work remains; request another bounded pass
				continue
			}
			if _, err := tx.Exec("DELETE FROM review_library_before WHERE track_id=?", id); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM review_library_dirty WHERE track_id=?", id); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
	return count, err
}
