package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
)

func execQueueSQL(t *testing.T, s *Store, query string, args ...any) sql.Result {
	t.Helper()
	result, err := s.db.Exec(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func insertProblemFixture(t *testing.T, s *Store) int64 {
	t.Helper()
	result := execQueueSQL(t, s, `INSERT INTO review_problem(rule_version,anchor_json,proof_json,created_at,updated_at)
	 VALUES(?, ?, ?, datetime('now'),datetime('now'))`, matching.MetadataRuleVersion,
		`{"title":"曲目","artist":"艺术家","album":"专辑"}`, `{"version":1,"kind":"text"}`)
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

const insertMemberFixture = `INSERT INTO review_problem_item(problem_id,item_id,input_revision,proof_json,created_at,updated_at)
 VALUES(?,?,?, '{"version":1,"kind":"text"}',datetime('now'),datetime('now'))`

func TestQueueIntegerRevisionAndOriginalInput(t *testing.T) {
	s := openTestStore(t)
	batch, err := s.CreateBatch(&ImportBatch{Filename: "原始.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	profile := matching.NewInputProfile("曲目（译名：歌曲）", "角色(CV.声优)", "专辑 (Deluxe Edition)")
	profile.Duration = matching.NumberInput{Raw: "10:20", State: matching.ValueKnown, Value: 620000, Unit: "ms", Precision: 1000, Semantic: "duration"}
	original := ImportItem{RowIndex: 7, RawRow: `["曲目（译名：歌曲）","角色(CV.声优)","专辑 (Deluxe Edition)","10:20"]`,
		Profile: &profile, InTitle: ptr(profile.Title.Raw), InArtist: ptr(profile.Artist.Text.Raw), InAlbum: ptr(profile.Album.Raw), InDurationMS: ptr(620000)}
	if err := s.EnqueueItems(batch, []ImportItem{original}); err != nil {
		t.Fatal(err)
	}
	queued, err := s.itemsByStatus(batch, StatusQueued)
	if err != nil || len(queued) != 1 || queued[0].Revision != 1 {
		t.Fatalf("initial revision: %+v %v", queued, err)
	}
	id := queued[0].ID
	wantRevision := int64(1)
	check := func() {
		t.Helper()
		item, err := s.GetItem(id)
		if err != nil {
			t.Fatal(err)
		}
		if item.Revision != wantRevision || item.RawRow != original.RawRow || item.RowIndex != original.RowIndex ||
			!reflect.DeepEqual(item.Profile, original.Profile) || !reflect.DeepEqual(item.InTitle, original.InTitle) ||
			!reflect.DeepEqual(item.InArtist, original.InArtist) || !reflect.DeepEqual(item.InAlbum, original.InAlbum) ||
			!reflect.DeepEqual(item.InDurationMS, original.InDurationMS) {
			t.Fatalf("revision/original mismatch: %+v", item)
		}
		// 时间戳保持相同也不影响下一次整数修订。
		execQueueSQL(t, s, "UPDATE import_item SET updated_at='2026-01-01' WHERE id=?", id)
	}
	check()
	item, err := s.ClaimNextItem(batch)
	if err != nil || item == nil || item.Revision != 2 {
		t.Fatalf("claim revision: %+v %v", item, err)
	}
	wantRevision++
	check()
	operations := []func() error{
		func() error {
			return s.MarkReview(id, ReviewAmbiguous, `[{"matchRef":"track"}]`, `[]`, `{"reason":"ambiguous"}`)
		},
		func() error { return s.MarkReview(id, ReviewReasonUnchanged, `[]`, `["改搜"]`) },
		func() error { _, err := s.RecordFailure(id, "来源失败", time.Second, 1); return err },
		func() error { return s.RetryItem(id) },
		func() error { _, err := s.ClaimNextItem(batch); return err },
		func() error { return s.ResetStaleMatching() },
		func() error { return s.MarkDiscarded(id) },
	}
	for _, operation := range operations {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
		wantRevision++
		check()
	}
	after, err := s.GetItem(id)
	if err != nil || after.ResolutionReason == nil || *after.ResolutionReason != ResolutionUserDiscarded || after.TrackID != nil || after.ResolutionTarget != nil {
		t.Fatalf("discard reason/target: %+v %v", after, err)
	}
	for _, column := range []string{"raw_row", "in_title", "in_artist", "in_album", "row_index", "in_year", "in_track_no", "in_disc_no", "in_duration_ms", "batch_id"} {
		if _, err := s.db.Exec("UPDATE import_item SET "+column+"=? WHERE id=?", "changed", id); err == nil {
			t.Fatalf("original column %s was mutable", column)
		}
	}
}

func TestReviewMembershipSchemaAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	b1, b2 := seedBatch(t, s, 1), seedBatch(t, s, 1)
	one, err := s.ClaimNextItem(b1)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.ClaimNextItem(b2)
	if err != nil {
		t.Fatal(err)
	}
	// This test exercises raw schema constraints; lifecycle behavior is tested
	// separately. Avoid MarkReview/RecordFailure, which now attach membership.
	execQueueSQL(t, s, "UPDATE import_item SET status='review',review_reason='no_match',revision=revision+1 WHERE id=?", one.ID)
	execQueueSQL(t, s, "UPDATE import_item SET status='failed',last_error='网络失败',revision=revision+1 WHERE id=?", two.ID)
	one, err = s.GetItem(one.ID)
	if err != nil {
		t.Fatal(err)
	}
	two, err = s.GetItem(two.ID)
	if err != nil {
		t.Fatal(err)
	}
	problem := insertProblemFixture(t, s)
	execQueueSQL(t, s, insertMemberFixture, problem, one.ID, one.Revision)
	execQueueSQL(t, s, insertMemberFixture, problem, two.ID, two.Revision)
	other := insertProblemFixture(t, s)
	if _, err := s.db.Exec(insertMemberFixture, other, one.ID, one.Revision); err == nil {
		t.Fatal("one item joined two active problems")
	}
	if _, err := s.db.Exec("UPDATE review_problem SET status='closed',closed_at=datetime('now') WHERE id=?", problem); err == nil {
		t.Fatal("closed a problem with active members")
	}
	if _, err := s.db.Exec("UPDATE import_item SET status='skipped' WHERE id=?", one.ID); err == nil {
		t.Fatal("finished an item without ending its active membership")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.AllReviewItems()
	if err != nil || len(items) != 2 || items[0].Status != StatusReview || items[1].Status != StatusFailed ||
		items[0].RawRow != one.RawRow || items[1].RawRow != two.RawRow || items[1].LastError == nil || *items[1].LastError != "网络失败" {
		t.Fatalf("restart lost source rows: %+v %v", items, err)
	}
	var members int
	if err := s.db.QueryRow("SELECT count(*) FROM review_problem_item WHERE problem_id=? AND active=1", problem).Scan(&members); err != nil || members != 2 {
		t.Fatalf("restart lost members: %d %v", members, err)
	}
	// 历史成员可保留，但不占用当前归属；新归属不能引用旧输入修订。
	execQueueSQL(t, s, "UPDATE review_problem_item SET active=0,revision=revision+1 WHERE problem_id=?", problem)
	execQueueSQL(t, s, "UPDATE review_problem SET status='closed',closed_at=datetime('now'),revision=revision+1 WHERE id=?", problem)
	if _, err := s.db.Exec("UPDATE review_problem_item SET active=1 WHERE problem_id=?", problem); err == nil {
		t.Fatal("reactivated a member of a closed problem")
	}
	if _, err := s.db.Exec(insertMemberFixture, other, one.ID, one.Revision-1); err == nil {
		t.Fatal("accepted stale input evidence")
	}
	execQueueSQL(t, s, insertMemberFixture, other, one.ID, one.Revision)
	if err := s.MarkDiscarded(two.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(insertMemberFixture, other, two.ID, two.Revision+1); err == nil {
		t.Fatal("terminal input joined a new problem")
	}
	// 删除历史批次仅移除其成员，不删除其他批次的原文或问题。
	execQueueSQL(t, s, "DELETE FROM import_batch WHERE id=?", b2)
	if err := s.db.QueryRow("SELECT count(*) FROM review_problem_item WHERE item_id=?", two.ID).Scan(&members); err != nil || members != 0 {
		t.Fatalf("orphan members: %d %v", members, err)
	}
	remaining, err := s.GetItem(one.ID)
	if err != nil || remaining == nil || remaining.RawRow != one.RawRow {
		t.Fatalf("other batch changed: %+v %v", remaining, err)
	}
}

func TestReviewMembershipConcurrentUnique(t *testing.T) {
	s := openTestStore(t)
	item, err := s.ClaimNextItem(seedBatch(t, s, 1))
	if err != nil {
		t.Fatal(err)
	}
	problems := []int64{insertProblemFixture(t, s), insertProblemFixture(t, s)}
	results := make(chan error, len(problems))
	var wg sync.WaitGroup
	for _, problem := range problems {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_, err := s.db.Exec(insertMemberFixture, id, item.ID, item.Revision)
			results <- err
		}(problem)
	}
	wg.Wait()
	close(results)
	var succeeded int
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one active membership, got %d", succeeded)
	}
}

func TestQueueRevisionAndJSONConstraints(t *testing.T) {
	s := openTestStore(t)
	item, err := s.ClaimNextItem(seedBatch(t, s, 1))
	if err != nil {
		t.Fatal(err)
	}
	problem := insertProblemFixture(t, s)
	execQueueSQL(t, s, insertMemberFixture, problem, item.ID, item.Revision)
	for _, column := range []struct{ table, name string }{
		{"import_item", "revision"}, {"review_problem", "revision"}, {"review_problem", "query_revision"},
		{"review_problem", "candidate_revision"}, {"review_problem", "rule_version"},
		{"review_problem_item", "revision"}, {"review_problem_item", "input_revision"},
	} {
		for _, value := range []any{nil, 0, -1, 1.5, "invalid"} {
			if _, err := s.db.Exec("UPDATE "+column.table+" SET "+column.name+"=?", value); err == nil {
				t.Errorf("accepted %s.%s=%v", column.table, column.name, value)
			}
		}
	}
	for _, column := range []struct{ table, name string }{
		{"import_item", "input_profile"}, {"import_item", "resolution_target"},
		{"review_problem", "anchor_json"}, {"review_problem", "proof_json"},
		{"review_problem", "query_json"}, {"review_problem", "candidate_facts"}, {"review_problem_item", "proof_json"},
	} {
		if _, err := s.db.Exec("UPDATE " + column.table + " SET " + column.name + "='not-json'"); err == nil {
			t.Errorf("accepted invalid JSON: %s.%s", column.table, column.name)
		}
	}
}

func TestQueueResolutionTargetRetainedWithoutLibraryForeignKey(t *testing.T) {
	s := openTestStore(t)
	item, err := s.ClaimNextItem(seedBatch(t, s, 1))
	if err != nil {
		t.Fatal(err)
	}
	target := ResolutionTarget{Track: domain.Identity{Provider: "musicbrainz", Ref: "track-a"}, Release: domain.Identity{Provider: "musicbrainz", Ref: "release-a"}}
	raw, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	execQueueSQL(t, s, `UPDATE import_item SET status='skipped',revision=revision+1,resolution_reason=?,track_id=123,
	 resolution_target=?,resolution_problem_id=456,resolution_problem_revision=7 WHERE id=?`, ResolutionExistingTrack, string(raw), item.ID)
	got, err := s.GetItem(item.ID)
	if err != nil || got.ResolutionReason == nil || *got.ResolutionReason != ResolutionExistingTrack ||
		got.ResolutionTarget == nil || *got.ResolutionTarget != target || got.ResolutionProblemRevision == nil || *got.ResolutionProblemRevision != 7 {
		t.Fatalf("resolution diagnostic lost: %+v %v", got, err)
	}
}

func TestTrackIdentifiersDoNotImposeRecordingOrISRCUniqueness(t *testing.T) {
	s := openTestStore(t)
	for _, values := range []struct {
		id            int
		source, track string
	}{{1, "musicbrainz", "track-a"}, {2, "musicbrainz", "track-b"}, {3, "itunes", "track-a"}} {
		execQueueSQL(t, s, `INSERT INTO track(id,match_source,match_ref,created_at,updated_at) VALUES(?,?,?,datetime('now'),datetime('now'))`, values.id, values.source, values.track)
		execQueueSQL(t, s, `INSERT INTO track_current(track_id) VALUES(?)`, values.id)
		execQueueSQL(t, s, `INSERT INTO track_snapshot(track_id,body) VALUES(?, '{"recording":{"state":"value","value":{"provider":"musicbrainz","ref":"same-recording"}}}')`, values.id)
		execQueueSQL(t, s, `INSERT INTO track_identifier(track_id,kind,value,position) VALUES(?,'ISRC','JPE301500361',0)`, values.id)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM track_read WHERE isrc='JPE301500361' AND recording_ref='same-recording'").Scan(&count); err != nil || count != 3 {
		t.Fatalf("different tracks did not coexist: %d %v", count, err)
	}
	if _, err := s.db.Exec(`INSERT INTO track(match_source,match_ref,created_at,updated_at) VALUES('musicbrainz','track-a',datetime('now'),datetime('now'))`); err == nil {
		t.Fatal("exact source+track duplicate accepted")
	}
}

func TestPriorGenerationsRejectedWithoutChanges(t *testing.T) {
	for _, generation := range []string{"upstream-snapshot-v1", "track-identity-v2", "track-identity-v3", "track-identity-v4"} {
		t.Run(generation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "old.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`CREATE TABLE model_generation(id INTEGER PRIMARY KEY,generation TEXT);
			 CREATE TABLE schema_version(version INTEGER,name TEXT,applied_at TEXT);
			 CREATE TABLE import_item(id INTEGER PRIMARY KEY,raw_row TEXT,input_profile TEXT);
			 INSERT INTO import_item VALUES(1,'["旧原文"]','{"ruleVersion":1}');`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("INSERT INTO model_generation VALUES(1,?); INSERT INTO schema_version VALUES(?,'old','2026-01-01')", generation, LatestVersion()); err != nil {
				t.Fatal(err)
			}
			opened, err := Open(path)
			if opened != nil {
				opened.Close()
			}
			if !errors.Is(err, ErrIncompatibleModel) {
				t.Fatalf("old model accepted: %v", err)
			}
			var raw string
			if err := db.QueryRow("SELECT raw_row FROM import_item WHERE id=1").Scan(&raw); err != nil || raw != `["旧原文"]` {
				t.Fatalf("old input changed: %q %v", raw, err)
			}
			var count int
			if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('review_problem','local_match_key','local_match_dirty','local_match_state')").Scan(&count); err != nil || count != 0 {
				t.Fatalf("old model migrated: %d %v", count, err)
			}
		})
	}
}
