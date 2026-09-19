package store

import (
	"fmt"
	"sync"
	"testing"
)

// TestDeleteTrackKeepsTracklist 覆盖 2.12 与验收 F4：
// 删除曲目清理署名分解/歌单标签/艺术家关联，但不动 release_track——
// 该位置在专辑视图中自然回落为灰色行。
func TestDeleteTrackKeepsTracklist(t *testing.T) {
	s := openTestStore(t)
	db := s.DB()

	// 溯源存在 track 上——曲目是单表，没有第二张表要连带清理
	ref := "track-mbid-4"
	trackID, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title:       "さようなら",
		MatchSource: ptr("musicbrainz"), MatchRef: &ref,
	})
	if err != nil {
		t.Fatalf("创建曲目失败: %v", err)
	}

	// 归属：一个歌单、一个标签、一位艺术家
	var playlistID, tagID int64
	for _, c := range []struct {
		kind, name string
		out        *int64
	}{{"playlist", "我的收藏", &playlistID}, {"tag", "ACG", &tagID}} {
		res, err := db.Exec(
			`INSERT INTO collection (name, kind, created_at) VALUES (?, ?, datetime('now'))`,
			c.name, c.kind)
		if err != nil {
			t.Fatalf("建 %s 失败: %v", c.kind, err)
		}
		*c.out, _ = res.LastInsertId()
		if _, err := db.Exec(
			`INSERT INTO collection_item (collection_id, track_id, added_at) VALUES (?, ?, datetime('now'))`,
			*c.out, trackID); err != nil {
			t.Fatalf("加入 %s 失败: %v", c.kind, err)
		}
	}
	artistID, _ := s.UpsertArtist(&Artist{Name: "後藤邑子"})
	if err := s.AddTrackArtist(trackID, TrackArtist{ArtistID: artistID, Role: RoleVocal}); err != nil {
		t.Fatalf("关联艺术家失败: %v", err)
	}
	if err := s.SetTrackCredits(trackID, []CreditEntry{
		{ArtistID: artistID, CreditedName: ptr("後藤邑子")},
	}); err != nil {
		t.Fatalf("写入署名分解失败: %v", err)
	}

	// 该曲目所属专辑的清单（上游事实，与 track 无外键）
	releaseID, err := s.UpsertRelease(&Release{
		Provider: ptr("musicbrainz"), ProviderRef: ptr("rel-1"), Title: "涼宮ハルヒの詰合",
		TracklistState: TracklistComplete,
	})
	if err != nil {
		t.Fatalf("写入发行失败: %v", err)
	}
	if err := s.ReplaceTracklist(releaseID, []ReleaseTrack{
		{DiscNumber: 1, TrackNumber: 4, TrackRef: &ref, Title: "さようなら"},
	}, TracklistComplete); err != nil {
		t.Fatalf("写入清单失败: %v", err)
	}

	if err := s.DeleteTrack(trackID); err != nil {
		t.Fatalf("删除曲目失败: %v", err)
	}

	// 用户数据被清理
	for _, c := range []struct {
		table, where string
	}{
		{"collection_item", "track_id"},
		{"track_artist", "track_id"},
		{"track_credit", "track_id"},
	} {
		var n int
		if err := db.QueryRow(
			fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = ?", c.table, c.where), trackID).Scan(&n); err != nil {
			t.Fatalf("统计 %s 失败: %v", c.table, err)
		}
		if n != 0 {
			t.Errorf("%s 中仍有 %d 行，应被级联清理", c.table, n)
		}
	}

	// 清单数据不受影响——该位置回落为灰色行
	var tracklistRows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM release_track WHERE release_id = ?`, releaseID).Scan(&tracklistRows); err != nil {
		t.Fatalf("统计清单失败: %v", err)
	}
	if tracklistRows != 1 {
		t.Errorf("清单行数 = %d, 期望 1（删除曲目不应影响清单）", tracklistRows)
	}
	var releases int
	db.QueryRow(`SELECT COUNT(*) FROM release WHERE id = ?`, releaseID).Scan(&releases)
	if releases != 1 {
		t.Error("发行记录应保留作缓存")
	}
}

// TestConcurrentWriteAndRead 覆盖 2.11：
// 开启 WAL 后，后台写循环与前台读并发时不出现 database is locked。
func TestConcurrentWriteAndRead(t *testing.T) {
	s := openTestStore(t)

	var journalMode string
	if err := s.DB().QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("读取 journal_mode 失败: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, 期望 wal", journalMode)
	}

	const writes, readers = 60, 4
	var wg sync.WaitGroup
	errCh := make(chan error, writes+readers*writes)

	// 后台写循环：模拟匹配队列按条目提交短事务
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < writes; i++ {
			if _, err := s.CreateTrack(&Track{Title: fmt.Sprintf("track-%d", i)}); err != nil {
				errCh <- fmt.Errorf("写入 %d: %w", i, err)
				return
			}
		}
	}()

	// 前台并发读
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < writes; i++ {
				var n int
				if err := s.DB().QueryRow(`SELECT COUNT(*) FROM track`).Scan(&n); err != nil {
					errCh <- fmt.Errorf("读取: %w", err)
					return
				}
				if _, err := s.SearchTrackIDs("track", 5); err != nil {
					errCh <- fmt.Errorf("检索: %w", err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("并发读写出错: %v", err)
	}

	var total int
	s.DB().QueryRow(`SELECT COUNT(*) FROM track`).Scan(&total)
	if total != writes {
		t.Errorf("曲目总数 = %d, 期望 %d", total, writes)
	}
}
