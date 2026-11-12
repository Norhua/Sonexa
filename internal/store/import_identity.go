package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

var ErrItemNotActive = errors.New("条目已处理或状态已变化，请刷新复核队列")

func importInput(it ImportItem) matching.Input {
	return matching.Input{Title: stringValue(it.InTitle), Artist: stringValue(it.InArtist), Album: stringValue(it.InAlbum), DurationMS: it.InDurationMS, DiscNo: it.InDiscNo, TrackNo: it.InTrackNo}
}
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// readImportInputsTx closes its cursor before any write, including on the
// single-connection SQLite configuration. Source row numbers are not consulted.
func readImportInputsTx(tx *sql.Tx, where string, args ...any) ([]ImportItem, error) {
	rows, err := tx.Query(`SELECT id,raw_row,in_title,in_artist,in_album,in_duration_ms,in_disc_no,in_track_no,in_year FROM import_item WHERE (`+where+`) AND EXISTS(SELECT 1 FROM import_destination d WHERE d.input_id=import_item.id AND d.state='active') ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ImportItem
	for rows.Next() {
		var it ImportItem
		if err := rows.Scan(&it.ID, &it.RawRow, &it.InTitle, &it.InArtist, &it.InAlbum, &it.InDurationMS, &it.InDiscNo, &it.InTrackNo, &it.InYear); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// exactInput treats missing metadata as identical only when the mapped input
// itself is identical and contains a searchable value. Different column maps
// cannot accidentally equate identical raw arrays.
func exactInput(a, b ImportItem) bool {
	ai, bi := importInput(a), importInput(b)
	if ai.Title == "" && ai.Artist == "" && ai.Album == "" {
		return false
	}
	return a.RawRow == b.RawRow && ai.Title == bi.Title && ai.Artist == bi.Artist && ai.Album == bi.Album && matching.CompatibleImportPosition(ai, bi)
}
func equivalentImport(a, b ImportItem) bool {
	return exactInput(a, b) || matching.SameImportText(importInput(a), importInput(b))
}

func mergeImportTx(tx *sql.Tx, itemID, targetID int64) error {
	if _, err := tx.Exec(`UPDATE import_item SET merged_item_id=? WHERE merged_item_id=?`, targetID, itemID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE import_item SET revision=revision+1,search_revision=search_revision+1,updated_at=? WHERE id=?`, time.Now().UTC(), targetID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE import_item SET status='merged',merged_item_id=?,next_retry_at=NULL,updated_at=? WHERE id=? AND status IN ('queued','matching','review','failed')`, targetID, time.Now().UTC(), itemID)
	return err
}

// mergeTextInputTx only coalesces an unambiguous active target. All retained
// aliases must agree, preventing a missing position from bridging tracks 1/2.
func mergeTextInputTx(tx *sql.Tx, item ImportItem) error {
	rows, err := tx.Query(`SELECT shell.id,i.raw_row,i.in_title,i.in_artist,i.in_album,i.in_duration_ms,i.in_disc_no,i.in_track_no
 FROM import_item shell JOIN import_item i ON (i.id=shell.id OR i.merged_item_id=shell.id)
 JOIN import_destination d ON d.input_id=i.id AND d.state='active'
 WHERE shell.id<>? AND shell.status IN ('queued','matching','review','failed') ORDER BY shell.id,i.id`, item.ID)
	if err != nil {
		return err
	}
	compatible := map[int64]bool{}
	for rows.Next() {
		var shellID int64
		var input ImportItem
		if err := rows.Scan(&shellID, &input.RawRow, &input.InTitle, &input.InArtist, &input.InAlbum, &input.InDurationMS, &input.InDiscNo, &input.InTrackNo); err != nil {
			rows.Close()
			return err
		}
		if _, seen := compatible[shellID]; !seen {
			compatible[shellID] = true
		}
		compatible[shellID] = compatible[shellID] && equivalentImport(item, input)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var targets []int64
	for shell, match := range compatible {
		if match {
			targets = append(targets, shell)
		}
	}
	if len(targets) == 1 {
		return mergeImportTx(tx, item.ID, targets[0])
	}
	return nil
}

// OriginalInputs returns immutable distinct source contents across all absorbed
// batches. The canonical input is first; neither path nor source row is stored.
func (s *Store) OriginalInputs(itemID int64) ([][]string, error) {
	rows, err := s.db.Query(`SELECT raw_row FROM import_item WHERE id=? OR merged_item_id=? ORDER BY id=? DESC,id`, itemID, itemID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]string{}
	seen := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var cells []string
		if json.Unmarshal([]byte(raw), &cells) != nil {
			cells = []string{raw}
		}
		key, _ := json.Marshal(cells)
		if !seen[string(key)] {
			seen[string(key)] = true
			out = append(out, cells)
		}
	}
	return out, rows.Err()
}

// ResolveImport checks current bindings inside the same write transaction as
// the terminal transition, then registers an unambiguous selected track for
// cross-input coalescing. An ISRC/recording or overlapping search pool is never
// a binding. A nil selected candidate still allows safe library duplicate checks.
func (s *Store) ResolveImport(itemID int64, candidates []matching.ScoredCandidate, selected *provider.Candidate) (bool, error) {
	handled := false
	err := s.withTx(func(tx *sql.Tx) error {
		var status ItemStatus
		if err := tx.QueryRow(`SELECT status FROM import_item WHERE id=?`, itemID).Scan(&status); err != nil {
			return err
		}
		if status.IsTerminal() {
			handled = true
			return nil
		}
		decision, err := ownedImportDecisionTx(tx, itemID, candidates)
		if err != nil {
			return err
		}
		if decision.TrackID != 0 {
			handled = true
			return skipOwnedImportTx(tx, itemID, decision)
		}
		inputs, err := readImportInputsTx(tx, `id=? OR merged_item_id=?`, itemID, itemID)
		if err != nil {
			return err
		}
		if len(inputs) == 0 {
			return ErrItemNotActive
		}
		if selected == nil || selected.MatchRef == "" {
			return nil
		}
		// A single search hit can still be the wrong album; only register a
		// resolved identity when every absorbed input agrees with its context.
		selectedInput := matching.Input{Title: stringValue(selected.Title), Album: stringValue(selected.AlbumTitle), DurationMS: selected.DurationMS, DiscNo: selected.DiscNumber, TrackNo: selected.TrackNumber}
		for _, input := range inputs {
			if !matching.SameTrackContext(importInput(input), selectedInput) {
				return nil
			}
		}
		merged, err := registerResolvedImportTx(tx, itemID, inputs, string(selected.Provider), selected.MatchRef)
		handled = merged
		return err
	})
	if err == nil {
		s.notifyImportInvalidation()
	}
	return handled, err
}

// SkipReviewCandidate rechecks the exact binding, not a client-supplied local ID.
func (s *Store) SkipReviewCandidate(itemID int64, source, ref string) error {
	return s.withTx(func(tx *sql.Tx) error {
		var id int64
		if source == "" || ref == "" {
			return fmt.Errorf("缺少精确曲目身份")
		}
		if err := tx.QueryRow(`SELECT id FROM track WHERE match_source=? AND match_ref=?`, source, ref).Scan(&id); err != nil {
			return err
		}
		var status ItemStatus
		if err := tx.QueryRow(`SELECT status FROM import_item WHERE id=?`, itemID).Scan(&status); err != nil {
			return err
		}
		if status != StatusReview && status != StatusFailed {
			return ErrItemNotActive
		}
		_, err := deliverImportTx(tx, itemID, id, StatusSkipped, map[string]string{"kind": "manual_skip"})
		return err
	})
}

// Search tokens prevent both out-of-order writes and resurrection after an action.
func (s *Store) BeginReviewSearch(itemID int64) (int64, error) {
	var revision int64
	err := s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE import_item SET search_revision=search_revision+1,search_manual=1,resolved_source=NULL,resolved_ref=NULL WHERE id=? AND status IN ('review','failed')`, itemID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrItemNotActive
		}
		return tx.QueryRow(`SELECT search_revision FROM import_item WHERE id=?`, itemID).Scan(&revision)
	})
	return revision, err
}
func (s *Store) FinishReviewSearch(itemID, revision int64, source, country, candidates, variants string) error {
	return s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE import_item SET status='review',search_manual=1,search_provider=?,search_country=?,candidates=?,tried_variants=?,updated_at=? WHERE id=? AND search_revision=? AND status IN ('review','failed')`, source, country, candidates, variants, time.Now().UTC(), itemID, revision)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrItemNotActive
		}
		return nil
	})
}
func (s *Store) SetImportSearchSource(itemID int64, source string) error {
	_, err := s.db.Exec(`UPDATE import_item SET search_provider=?,search_country='',search_manual=0 WHERE id=? AND status='matching'`, source, itemID)
	return err
}

func (s *Store) ReviewSearchSource(itemID int64) (string, string, bool, error) {
	var source, country string
	var manual bool
	err := s.db.QueryRow(`SELECT search_provider,search_country,search_manual FROM import_item WHERE id=?`, itemID).Scan(&source, &country, &manual)
	return source, country, manual, err
}

// ProvidedImport 描述「候选 C 所在发行不在库（或不是主发行），改由组内已有发行 A 的
// 清单条目提供」这次替换的两侧身份。
type ProvidedImport struct {
	// Candidate 是匹配选中的候选 C，Scored 是它自己的评分记录。
	Candidate provider.Candidate
	// ReleaseID / TrackRef 是已有发行 A 及其清单条目的曲目标识。
	ReleaseID int64
	TrackRef  string
}

// ResolveProvidedImport 是替换到已有发行之后的已拥有判重与已解析身份登记。
//
// 不能拿 A 的专辑名、版本段与位置去比输入：输入描述的是 C 所在的发行（例如
// 「专辑（Deluxe）」第 2 碟第 3 首），A 上那首歌已拥有时必然判不出来。改为两段证明：
//   - 输入 ↔ 候选 C：用 C 自己的曲名、署名、专辑、位置、时长，与「精确绑定已在库」
//     时同样严格（MatchOwnedInput，exactRef 为真）；
//   - 候选 C ↔ A 的曲目：录音级相同，且 A 的该清单条目的曲目标识就是本地曲目的绑定。
//
// 两段都成立即跳过（证据 group_recording_provider）。A 的曲目已拥有而第一段不成立时
// 返回 owned=true、handled=false，由调用方送人工复核，不以已有身份绕过。
// A 的曲目尚未入库时只登记已解析身份（提供者条目的曲目标识），输入与候选上下文的
// 一致性仍按 C 校验。整个过程在一个事务里完成并重验。
func (s *Store) ResolveProvidedImport(itemID int64, in ProvidedImport) (handled, owned bool, err error) {
	c := in.Candidate
	err = s.withTx(func(tx *sql.Tx) error {
		var status ItemStatus
		if err := tx.QueryRow(`SELECT status FROM import_item WHERE id=?`, itemID).Scan(&status); err != nil {
			return err
		}
		if status.IsTerminal() {
			handled = true
			return nil
		}
		inputs, err := readImportInputsTx(tx, `id=? OR merged_item_id=?`, itemID, itemID)
		if err != nil {
			return err
		}
		if len(inputs) == 0 {
			return ErrItemNotActive
		}
		// 第二段：清单条目仍在，且与候选是同一来源的同一录音。
		var recording *string
		err = tx.QueryRow(`SELECT rt.recording_ref FROM release_track rt JOIN release r ON r.id=rt.release_id
 WHERE rt.release_id=? AND rt.track_ref=? AND r.provider=?`, in.ReleaseID, in.TrackRef, string(c.Provider)).Scan(&recording)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPlanStale
		}
		if err != nil {
			return err
		}
		if recording == nil || c.RecordingRef == nil || *recording == "" || *recording != *c.RecordingRef {
			return ErrPlanStale
		}
		var trackID int64
		err = tx.QueryRow(`SELECT id FROM track WHERE match_source=? AND match_ref=? AND release_id=?`, string(c.Provider), in.TrackRef, in.ReleaseID).Scan(&trackID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		candidate := matching.Input{Title: stringValue(c.Title), Artist: stringValue(c.ArtistName), Album: stringValue(c.AlbumTitle), DurationMS: c.DurationMS, DiscNo: c.DiscNumber, TrackNo: c.TrackNumber}
		if trackID != 0 {
			owned = true
			// 输入若本身就与 A 的曲目一致，现有判定已经足够。
			decision, err := ownedImportDecisionTx(tx, itemID, []matching.ScoredCandidate{{Candidate: provider.Candidate{Provider: c.Provider, MatchRef: in.TrackRef}}})
			if err != nil {
				return err
			}
			if decision.TrackID == trackID {
				handled = true
				return skipOwnedImportTx(tx, itemID, decision)
			}
			// 第一段：每条输入都要独立与候选 C 一致。
			proof := ownedImportDecision{TrackID: trackID, Rule: "group-recording-provider-v1"}
			for _, input := range inputs {
				evidence := matching.MatchOwnedInput(importInput(input), candidate, true)
				if evidence.Rank == 0 {
					return nil
				}
				evidence.Kind = "group_recording_provider"
				evidence.Facts = append(evidence.Facts, "candidate:"+string(c.Provider)+":"+c.MatchRef, "candidate_release:"+stringValue(c.ReleaseRef),
					"same_recording:"+*recording, "provider_track:"+in.TrackRef)
				proof.Inputs = append(proof.Inputs, evidence)
			}
			handled = true
			return skipOwnedImportTx(tx, itemID, proof)
		}
		for _, input := range inputs {
			if !matching.SameTrackContext(importInput(input), candidate) {
				return nil
			}
		}
		merged, err := registerResolvedImportTx(tx, itemID, inputs, string(c.Provider), in.TrackRef)
		handled = merged
		return err
	})
	if err == nil {
		s.notifyImportInvalidation()
	}
	return handled, owned, err
}

// registerResolvedImportTx 登记条目已解析到的曲目身份；另一条活动条目已解析到同一身份
// 且位置不冲突时把本条并入它。返回是否发生了合并。
func registerResolvedImportTx(tx *sql.Tx, itemID int64, inputs []ImportItem, source, ref string) (bool, error) {
	if _, err := tx.Exec(`UPDATE import_item SET resolved_source=?,resolved_ref=? WHERE id=?`, source, ref, itemID); err != nil {
		return false, err
	}
	var target int64
	err := tx.QueryRow(`SELECT id FROM import_item WHERE id<>? AND resolved_source=? AND resolved_ref=? AND status IN ('queued','matching','review','failed') ORDER BY id LIMIT 1`, itemID, source, ref).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	others, err := readImportInputsTx(tx, `id=? OR merged_item_id=?`, target, target)
	if err != nil {
		return false, err
	}
	for _, input := range inputs {
		for _, other := range others {
			if !matching.CompatibleImportPosition(importInput(input), importInput(other)) {
				return false, nil
			}
		}
	}
	return true, mergeImportTx(tx, itemID, target)
}
