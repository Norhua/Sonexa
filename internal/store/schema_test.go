package store

import (
	"strings"
	"testing"
)

// TestSchemaTablesExist 覆盖 2.4-2.9：迁移可在空库上应用并建出全部表。
func TestSchemaTablesExist(t *testing.T) {
	s := openTestStore(t)

	want := []string{
		"track",
		"release_group", "release", "release_track",
		"artist", "artist_link", "track_artist", "track_credit", "release_credit",
		"collection", "collection_item",
		"import_batch", "import_item",
		"provider_cache", "track_fts", "track_fts_shadow",
		"schema_version",
	}
	for _, table := range want {
		var name string
		err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("表 %s 不存在: %v", table, err)
		}
	}

	// track_metadata 已并入 track：曲目是单表，不再有第二份基本信息
	var gone string
	if err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE name = 'track_metadata'`,
	).Scan(&gone); err == nil {
		t.Error("track_metadata 应已并入 track，不应再存在")
	}
}

// TestMatchRefPartialUnique 覆盖 2.4：idx_match_ref 是部分唯一索引——
// 已匹配的 (来源, 标识) 唯一，而未匹配的曲目（match_source IS NULL）不受约束。
//
// 溯源已从 track_metadata 挪到 track 上，判定本身不变。
func TestMatchRefPartialUnique(t *testing.T) {
	s := openTestStore(t)
	db := s.DB()

	newTrack := func(title string, source, ref any) error {
		_, err := db.Exec(
			`INSERT INTO track (title, match_source, match_ref, created_at, updated_at)
			 VALUES (?, ?, ?, datetime('now'), datetime('now'))`, title, source, ref)
		return err
	}

	if err := newTrack("a", "musicbrainz", "mbid-1"); err != nil {
		t.Fatalf("首次写入溯源失败: %v", err)
	}
	// 同一 (来源, 标识) 不可重复——「同一上游结果不被重复加入」
	if err := newTrack("b", "musicbrainz", "mbid-1"); err == nil {
		t.Error("重复的 (match_source, match_ref) 应被唯一约束拒绝")
	}
	// 不同来源的同一标识互不冲突
	if err := newTrack("c", "itunes", "mbid-1"); err != nil {
		t.Errorf("不同来源的同一标识不应冲突: %v", err)
	}
	// 未匹配的曲目不受唯一约束：多条 NULL 溯源可以并存
	for i := 0; i < 3; i++ {
		if err := newTrack("none", nil, nil); err != nil {
			t.Fatalf("未匹配曲目第 %d 条写入失败: %v", i+1, err)
		}
	}
}

// TestArtistUniqueWithDisambiguation 覆盖 2.5：
// 同名 + 歧义消解的组合唯一——两位同名艺术家靠 disambiguation 区分。
func TestArtistUniqueWithDisambiguation(t *testing.T) {
	s := openTestStore(t)
	db := s.DB()

	add := func(name string, disambiguation any) error {
		_, err := db.Exec(
			`INSERT INTO artist (name, disambiguation, created_at, updated_at)
			 VALUES (?, ?, datetime('now'), datetime('now'))`, name, disambiguation)
		return err
	}

	if err := add("平野綾", nil); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	if err := add("平野綾", nil); err == nil {
		t.Error("同名且都无歧义消解的艺术家应被拒绝")
	}
	// 带歧义消解的同名者是另一条独立记录
	if err := add("平野綾", "US band"); err != nil {
		t.Errorf("同名但有歧义消解应可插入: %v", err)
	}
	if err := add("平野綾", "US band"); err == nil {
		t.Error("同名且歧义消解相同的艺术家应被拒绝")
	}
}

// TestCollectionNameUniquePerKind 覆盖 2.6：
// (kind, name) 唯一——同名的歌单与标签可以并存。
func TestCollectionNameUniquePerKind(t *testing.T) {
	s := openTestStore(t)
	db := s.DB()

	add := func(kind, name string) error {
		_, err := db.Exec(
			`INSERT INTO collection (name, kind, created_at) VALUES (?, ?, datetime('now'))`,
			name, kind)
		return err
	}

	if err := add("playlist", "ACG"); err != nil {
		t.Fatalf("建歌单失败: %v", err)
	}
	if err := add("playlist", "ACG"); err == nil {
		t.Error("同名歌单应被拒绝")
	}
	// 同名的标签与歌单并存
	if err := add("tag", "ACG"); err != nil {
		t.Errorf("与歌单同名的标签应被允许: %v", err)
	}
}

// TestReleaseRefUnique 覆盖 2.7：(provider, provider_ref) 唯一。
//
// 唯一性只对有上游标识的歌集成立：本地歌集（provider_ref IS NULL）
// 靠 (标题, 署名) 归拢，不受这个索引约束。
func TestReleaseRefUnique(t *testing.T) {
	s := openTestStore(t)
	db := s.DB()

	add := func(provider, ref, title string) error {
		_, err := db.Exec(
			`INSERT INTO release (provider, provider_ref, title, tracklist_state, created_at, updated_at)
			 VALUES (?, ?, ?, 'none', datetime('now'), datetime('now'))`, provider, ref, title)
		return err
	}

	if err := add("musicbrainz", "rel-1", "专辑"); err != nil {
		t.Fatalf("插入发行失败: %v", err)
	}
	if err := add("musicbrainz", "rel-1", "专辑（重复）"); err == nil {
		t.Error("重复的 (provider, provider_ref) 应被拒绝")
	}
	if err := add("itunes", "rel-1", "同标识不同来源"); err != nil {
		t.Errorf("不同来源的同一标识不应冲突: %v", err)
	}
	// 本地歌集没有上游标识，索引不约束它们
	if _, err := db.Exec(
		`INSERT INTO release (title, tracklist_state, created_at, updated_at)
		 VALUES ('本地歌集', 'none', datetime('now'), datetime('now'))`); err != nil {
		t.Fatalf("插入本地歌集失败: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO release (title, tracklist_state, created_at, updated_at)
		 VALUES ('另一张本地歌集', 'none', datetime('now'), datetime('now'))`); err != nil {
		t.Errorf("多条无上游标识的歌集应可并存: %v", err)
	}
}

// TestReleaseGroupRefUnique 覆盖 2.7 的发行组一侧：
// (provider, provider_ref) 唯一——同一发行组不被重复登记。
func TestReleaseGroupRefUnique(t *testing.T) {
	s := openTestStore(t)
	db := s.DB()

	add := func(ref, title string) error {
		_, err := db.Exec(
			`INSERT INTO release_group (provider, provider_ref, title, created_at, updated_at)
			 VALUES ('musicbrainz', ?, ?, datetime('now'), datetime('now'))`, ref, title)
		return err
	}

	if err := add("rg-1", "涼宮ハルヒの詰合"); err != nil {
		t.Fatalf("插入发行组失败: %v", err)
	}
	if err := add("rg-1", "重复的发行组"); err == nil {
		t.Error("重复的 (provider, provider_ref) 应被拒绝")
	}
}

// TestTrackCreditAllowsSameArtistTwice 覆盖署名分解的主键：
// 同一位艺术家可能在一条署名里出现两次（形如 A feat. B & A），
// position 进主键使各自有各自的区间。
func TestTrackCreditAllowsSameArtistTwice(t *testing.T) {
	s := openTestStore(t)
	db := s.DB()

	trackID, _ := s.CreateTrack(&Track{Title: "曲目"})
	artistID, _ := s.UpsertArtist(&Artist{Name: "A"})

	add := func(position int, span any) error {
		_, err := db.Exec(
			`INSERT INTO track_credit (track_id, artist_id, position, span_start, span_end)
			 VALUES (?, ?, ?, ?, ?)`, trackID, artistID, position, span, span)
		return err
	}

	if err := add(0, 0); err != nil {
		t.Fatalf("首次写入署名失败: %v", err)
	}
	if err := add(1, 20); err != nil {
		t.Errorf("同一位艺术家的第二个位置应可写入: %v", err)
	}
	if err := add(0, 0); err == nil {
		t.Error("同一 (曲目, 艺术家, 位置) 应被拒绝")
	}
}

// TestImportQueueIndexUsed 覆盖 2.8：队列索引 (status, next_retry_at) 被查询计划命中。
func TestImportQueueIndexUsed(t *testing.T) {
	s := openTestStore(t)

	rows, err := s.DB().Query(
		`EXPLAIN QUERY PLAN
		 SELECT id FROM import_item
		 WHERE status = 'queued' AND (next_retry_at IS NULL OR next_retry_at <= datetime('now'))
		 ORDER BY next_retry_at`)
	if err != nil {
		t.Fatalf("EXPLAIN 失败: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("扫描查询计划失败: %v", err)
		}
		plan.WriteString(detail + "\n")
	}
	if !strings.Contains(plan.String(), "idx_import_item_queue") {
		t.Errorf("队列查询未命中 idx_import_item_queue，计划为:\n%s", plan.String())
	}
}
