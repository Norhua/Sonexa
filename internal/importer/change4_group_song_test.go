package importer

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// 修改 4 阶段 D：组内歌曲解析（自动路径）。
//
// 约定：组 rg-1 里已有发行 rel-A（专辑「Album」），候选在发行 rel-B 上。

func c4Run(t *testing.T, st *store.Store, searcher *recordingSearcher, rows ...[3]string) (*Runner, int64, store.BatchStats) {
	t.Helper()
	batch := c4Enqueue(t, st, rows)
	runner := newRunner(st, searcher)
	runner.retry.MaxAttempts = 1
	t.Cleanup(runner.Close)
	runToCompletion(t, runner, batch)
	stats, err := st.Stats(batch)
	if err != nil {
		t.Fatal(err)
	}
	return runner, batch, stats
}

func c4Review(t *testing.T, st *store.Store) (string, domain.AdmissionBaseline) {
	t.Helper()
	items, err := st.AllReviewItems()
	if err != nil || len(items) != 1 {
		t.Fatalf("review items: %d %v", len(items), err)
	}
	var baseline domain.AdmissionBaseline
	if items[0].ReviewEvidence != nil {
		_ = json.Unmarshal([]byte(*items[0].ReviewEvidence), &baseline)
	}
	if items[0].ReviewReason == nil {
		return "", baseline
	}
	return *items[0].ReviewReason, baseline
}

func c4Count(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestGroupSongResolvesToExistingRelease(t *testing.T) {
	st := openStore(t)
	c4Owned(t, st, "a-1", "Existing", "Artist", "Album", "rel-A", "rg-1")
	searcher := &recordingSearcher{
		byTitle: map[string][]provider.Candidate{
			"Shared Song":  {c4Candidate("b-5", "Shared Song", "Artist", "Album", "rel-B", "rg-1", "rec-x", 12)},
			"Another Tune": {c4Candidate("b-6", "Another Tune", "Artist", "Album", "rel-B", "rg-1", "rec-y", 12)},
		},
		details: map[string]*domain.Snapshot{"rel-A": c4Detail("rel-A", "rg-1", "Album", "Artist",
			[3]string{"a-1", "rec-a1", "Existing"}, [3]string{"a-2", "rec-x", "Shared Song"}, [3]string{"a-3", "rec-y", "Another Tune"})},
	}
	_, _, stats := c4Run(t, st, searcher, [3]string{"Shared Song", "Artist", "Album"}, [3]string{"Another Tune", "Artist", "Album"})
	if stats.Imported != 2 || stats.Review != 0 {
		t.Fatalf("%+v", stats)
	}
	// 绑定到已有发行的清单条目，没有新建候选所在的发行。
	for _, ref := range []string{"a-2", "a-3"} {
		if n := c4Count(t, st, `SELECT count(*) FROM track t JOIN release r ON r.id=t.release_id WHERE t.match_ref=? AND r.provider_ref='rel-A'`, ref); n != 1 {
			t.Fatalf("%s not bound to the existing release", ref)
		}
	}
	if rel, _ := st.GetReleaseByRef("musicbrainz", "rel-B"); rel != nil {
		t.Fatal("candidate release was created")
	}
	if id, _ := st.FindByMatchRef("musicbrainz", "b-5"); id != 0 {
		t.Fatal("candidate track was created")
	}
	// 清单只取一次：第二首同专辑歌曲不再发请求。
	if n := searcher.detailCount(); n != 1 {
		t.Fatalf("detail requests = %d", n)
	}
	if n := c4Count(t, st, `SELECT count(*) FROM release_membership_decision`); n != 0 {
		t.Fatal("automatic resolution recorded a human decision")
	}
}

func TestGroupSongOwnedOnExistingReleaseIsSkipped(t *testing.T) {
	for _, mode := range []string{"input-describes-candidate", "input-cannot-prove-candidate"} {
		t.Run(mode, func(t *testing.T) {
			st := openStore(t)
			owned := c4Owned(t, st, "a-2", "Shared Song", "Artist", "Album", "rel-A", "rg-1")
			candidate := c4Candidate("b-5", "Shared Song", "Artist", "Album (Deluxe)", "rel-B", "rg-1", "rec-x", 20)
			disc, position := 2, 3
			candidate.DiscNumber, candidate.TrackNumber = &disc, &position
			searcher := &recordingSearcher{
				byTitle: map[string][]provider.Candidate{"Shared Song": {candidate}},
				details: map[string]*domain.Snapshot{"rel-A": c4Detail("rel-A", "rg-1", "Album", "Artist", [3]string{"a-1", "rec-a1", "Existing"}, [3]string{"a-2", "rec-x", "Shared Song"})},
			}
			// 输入描述的是候选所在的发行（豪华版），不是已拥有曲目所在的普通版。
			row := [3]string{"Shared Song", "Artist", "Album (Deluxe)"}
			if mode == "input-cannot-prove-candidate" {
				row[2] = ""
			}
			_, batch, stats := c4Run(t, st, searcher, row)
			if mode == "input-cannot-prove-candidate" {
				// 输入不能证明与候选一致：不以已有身份绕过复核。
				if reason, _ := c4Review(t, st); stats.Review != 1 || stats.Skipped != 0 || reason != "ambiguous" {
					t.Fatalf("%+v %s", stats, reason)
				}
				return
			}
			if stats.Skipped != 1 || stats.Review != 0 || stats.Imported != 0 {
				t.Fatalf("%+v", stats)
			}
			var trackID int64
			var evidence string
			if err := st.DB().QueryRow(`SELECT track_id,review_evidence FROM import_item WHERE batch_id=?`, batch).Scan(&trackID, &evidence); err != nil {
				t.Fatal(err)
			}
			if trackID != owned || !strings.Contains(evidence, "group_recording_provider") || !strings.Contains(evidence, "candidate:musicbrainz:b-5") {
				t.Fatalf("track %d evidence %s", trackID, evidence)
			}
			if n := c4Count(t, st, `SELECT count(*) FROM track`); n != 1 {
				t.Fatal("skip created a track")
			}
		})
	}
}

func TestGroupSongTracklistUnavailable(t *testing.T) {
	candidate := c4Candidate("b-5", "Shared Song", "Artist", "Album", "rel-B", "rg-1", "rec-x", 12)
	t.Run("still-incomplete", func(t *testing.T) {
		st := openStore(t)
		c4Owned(t, st, "a-1", "Existing", "Artist", "Album", "rel-A", "rg-1")
		partial := c4Detail("rel-A", "rg-1", "Album", "Artist", [3]string{"a-1", "rec-a1", "Existing"})
		partial.Release.TracklistCompleteness = domain.Partial
		searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Shared Song": {candidate}}, details: map[string]*domain.Snapshot{"rel-A": partial}}
		_, _, stats := c4Run(t, st, searcher, [3]string{"Shared Song", "Artist", "Album"})
		reason, baseline := c4Review(t, st)
		if stats.Review != 1 || reason != "song_unresolved" || baseline.Coverage != domain.CoverageUnknown || len(baseline.Undecidable) != 1 || !baseline.NeedsTypedConfirm {
			t.Fatalf("%+v %s %+v", stats, reason, baseline)
		}
		if rel, _ := st.GetReleaseByRef("musicbrainz", "rel-B"); rel != nil {
			t.Fatal("release created without proof")
		}
	})
	t.Run("request-failed", func(t *testing.T) {
		st := openStore(t)
		c4Owned(t, st, "a-1", "Existing", "Artist", "Album", "rel-A", "rg-1")
		searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Shared Song": {candidate}}, detailErr: errors.New("offline")}
		_, _, stats := c4Run(t, st, searcher, [3]string{"Shared Song", "Artist", "Album"})
		// 请求失败按请求失败处理（退避、计次），不进复核。
		if stats.Failed != 1 || stats.Review != 0 || stats.Imported != 0 {
			t.Fatalf("%+v", stats)
		}
	})
}

func TestGroupSongDuplicatePositions(t *testing.T) {
	for _, position := range []int{2, 9} {
		st := openStore(t)
		c4Owned(t, st, "a-9", "Existing", "Artist", "Album", "rel-A", "rg-1")
		candidate := c4Candidate("b-5", "Shared Song", "Artist", "Album", "rel-B", "rg-1", "rec-x", 12)
		disc, pos := 1, position
		candidate.DiscNumber, candidate.TrackNumber = &disc, &pos
		searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Shared Song": {candidate}},
			details: map[string]*domain.Snapshot{"rel-A": c4Detail("rel-A", "rg-1", "Album", "Artist",
				[3]string{"a-1", "rec-x", "Shared Song"}, [3]string{"a-2", "rec-x", "Shared Song"}, [3]string{"a-9", "rec-9", "Existing"})}}
		_, _, stats := c4Run(t, st, searcher, [3]string{"Shared Song", "Artist", "Album"})
		if position == 2 {
			// 同一录音出现在两个位置：取碟号、音轨号与候选相同的那条。
			if id, _ := st.FindByMatchRef("musicbrainz", "a-2"); stats.Imported != 1 || id == 0 {
				t.Fatalf("position %d: %+v", position, stats)
			}
			continue
		}
		if reason, _ := c4Review(t, st); stats.Review != 1 || reason != "song_unresolved" {
			t.Fatalf("position %d: %+v %s", position, stats, reason)
		}
	}
}

func TestGroupSongUniqueToCandidateRelease(t *testing.T) {
	st := openStore(t)
	c4Owned(t, st, "a-1", "Existing", "Artist", "Album", "rel-A", "rg-1")
	searcher := &recordingSearcher{
		byTitle: map[string][]provider.Candidate{"Bonus Track": {c4Candidate("b-9", "Bonus Track", "Artist", "Album", "rel-B", "rg-1", "rec-bonus", 13)}},
		details: map[string]*domain.Snapshot{"rel-A": c4Detail("rel-A", "rg-1", "Album", "Artist", [3]string{"a-1", "rec-a1", "Existing"})},
	}
	_, _, stats := c4Run(t, st, searcher, [3]string{"Bonus Track", "Artist", "Album"})
	// 组内每个已有发行都按录音证明不含这首歌，且上游原生组已映射到该组：自动新建发行。
	if stats.Imported != 1 || stats.Review != 0 {
		t.Fatalf("%+v", stats)
	}
	if n := c4Count(t, st, `SELECT count(*) FROM track t JOIN release r ON r.id=t.release_id WHERE t.match_ref='b-9' AND r.provider_ref='rel-B'`); n != 1 {
		t.Fatal("unique song not admitted on its own release")
	}
	if n := c4Count(t, st, `SELECT count(DISTINCT release_group_id) FROM release`); n != 1 {
		t.Fatal("releases split across groups")
	}
}

func TestGroupSongPrefersReleaseWithMoreReferences(t *testing.T) {
	st := openStore(t)
	c4Owned(t, st, "a-1", "Existing", "Artist", "Album", "rel-A", "rg-1")
	c4Owned(t, st, "a-3", "Existing Too", "Artist", "Album", "rel-A", "rg-1")
	c4Owned(t, st, "b-1", "Bonus", "Artist", "Album", "rel-B", "rg-1")
	var enriched []string
	searcher := &recordingSearcher{
		byTitle: map[string][]provider.Candidate{"Shared Song": {c4Candidate("b-5", "Shared Song", "Artist", "Album", "rel-B", "rg-1", "rec-x", 3)}},
		details: map[string]*domain.Snapshot{
			"rel-A": c4Detail("rel-A", "rg-1", "Album", "Artist", [3]string{"a-1", "rec-a1", "Existing"}, [3]string{"a-2", "rec-x", "Shared Song"}, [3]string{"a-3", "rec-a3", "Existing Too"}),
			"rel-B": c4Detail("rel-B", "rg-1", "Album", "Artist", [3]string{"b-1", "rec-b1", "Bonus"}, [3]string{"b-5", "rec-x", "Shared Song"}),
		},
	}
	batch := c4Enqueue(t, st, [][3]string{{"Shared Song", "Artist", "Album"}})
	runner := newRunner(st, searcher)
	runner.fetchOnMatch = true
	runner.SetEnricher(func(_ int64, providerID, releaseRef string) { enriched = append(enriched, providerID+":"+releaseRef) })
	t.Cleanup(runner.Close)
	runToCompletion(t, runner, batch)
	stats, _ := st.Stats(batch)
	// 候选所在发行已在库，但组内另一发行的曲目引用更多且清单含同一录音：改用它。
	if id, _ := st.FindByMatchRef("musicbrainz", "a-2"); stats.Imported != 1 || id == 0 {
		t.Fatalf("%+v", stats)
	}
	if id, _ := st.FindByMatchRef("musicbrainz", "b-5"); id != 0 {
		t.Fatal("song bound to the smaller release")
	}
	// 补全用的是曲目实际所挂发行的身份。
	if len(enriched) != 1 || enriched[0] != "musicbrainz:rel-A" {
		t.Fatalf("enriched %v", enriched)
	}
}

func TestExistingCandidateReleaseFetchesSiblingTracklist(t *testing.T) {
	st := openStore(t)
	c4Owned(t, st, "a-1", "Existing", "Artist", "Album", "rel-A", "rg-1")
	c4Owned(t, st, "b-1", "Bonus", "Artist", "Album", "rel-B", "rg-1")
	c4Owned(t, st, "b-2", "Bonus Two", "Artist", "Album", "rel-B", "rg-1")
	searcher := &recordingSearcher{
		byTitle: map[string][]provider.Candidate{"Bonus Three": {c4Candidate("b-3", "Bonus Three", "Artist", "Album", "rel-B", "rg-1", "rec-b3", 3)}},
		details: map[string]*domain.Snapshot{"rel-A": c4Detail("rel-A", "rg-1", "Album", "Artist", [3]string{"a-1", "rec-a1", "Existing"})},
	}
	_, _, stats := c4Run(t, st, searcher, [3]string{"Bonus Three", "Artist", "Album"})
	// 候选所在发行已在库、组内另一发行清单未加载：补取一次后照常入库，不因不可判定进复核。
	if id, _ := st.FindByMatchRef("musicbrainz", "b-3"); stats.Imported != 1 || stats.Review != 0 || id == 0 || searcher.detailCount() != 1 {
		t.Fatalf("%+v details %d", stats, searcher.detailCount())
	}
}

func TestEmptyReleaseDoesNotBlockGroup(t *testing.T) {
	st := openStore(t)
	c4Owned(t, st, "a-1", "Existing", "Artist", "Album", "rel-A", "rg-1")
	// 一个没有曲目引用的空发行（清单缺失）：不参与覆盖，也不为它补取。
	// 预览或迁移会留下这样的发行行；这里把它唯一的曲目摘下来构造同样的状态。
	empty := c4Owned(t, st, "e-1", "Gone", "Artist", "Album", "rel-E", "rg-1")
	if _, err := st.DB().Exec(`UPDATE track SET release_id=NULL WHERE id=?`, empty); err != nil {
		t.Fatal(err)
	}
	if rel, _ := st.GetReleaseByRef("musicbrainz", "rel-E"); rel == nil || c4Count(t, st, `SELECT count(*) FROM track WHERE release_id=?`, rel.ID) != 0 {
		t.Fatal("precondition: empty release row missing")
	}
	searcher := &recordingSearcher{
		byTitle: map[string][]provider.Candidate{"Bonus Track": {c4Candidate("b-9", "Bonus Track", "Artist", "Album", "rel-B", "rg-1", "rec-bonus", 13)}},
		details: map[string]*domain.Snapshot{"rel-A": c4Detail("rel-A", "rg-1", "Album", "Artist", [3]string{"a-1", "rec-a1", "Existing"})},
	}
	_, _, stats := c4Run(t, st, searcher, [3]string{"Bonus Track", "Artist", "Album"})
	if stats.Imported != 1 || stats.Review != 0 {
		t.Fatalf("%+v", stats)
	}
	for _, ref := range searcher.detailCalls {
		if ref == "rel-E" {
			t.Fatal("fetched the tracklist of an empty release")
		}
	}
}
