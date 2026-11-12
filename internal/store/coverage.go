package store

import (
	"database/sql"
	"sort"
	"strconv"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
)

// 提供者条目的证据级别。
const (
	CoverageEvidenceRecording = "recording"
	CoverageEvidenceText      = "text"
)

// GroupTracklistGap 是一个清单还没取全的组内发行，供调用方按需补取一次。
type GroupTracklistGap struct {
	ReleaseID int64
	Identity  domain.Identity
	Revision  int64
}

// GroupTracklistGaps 返回组内指定来源、仍有曲目引用但清单不是 complete 的发行。
//
// 只包含同来源且确实被引用过的发行：空发行不参与组内歌曲解析，也不该为它发请求；
// except 是这次准入的目标发行，它不参与覆盖结论，同样不需要为此补取。
func (s *Store) GroupTracklistGaps(groupID int64, provider domain.ProviderID, except int64) ([]GroupTracklistGap, error) {
	rows, err := s.db.Query(`SELECT r.id,r.provider_ref,r.revision FROM release r
 WHERE r.release_group_id=? AND r.provider=? AND r.provider_ref IS NOT NULL AND r.id<>?
 AND r.tracklist_state<>? AND EXISTS(SELECT 1 FROM track t WHERE t.release_id=r.id)
 ORDER BY r.id`, groupID, string(provider), except, string(TracklistComplete))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupTracklistGap
	for rows.Next() {
		gap := GroupTracklistGap{Identity: domain.Identity{Provider: provider}}
		if err := rows.Scan(&gap.ReleaseID, &gap.Identity.Ref, &gap.Revision); err != nil {
			return nil, err
		}
		out = append(out, gap)
	}
	return out, rows.Err()
}

// coverageEntry 是参与比较的一条清单条目（本地发行则是一条曲目行）。
type coverageEntry struct {
	discNumber   int
	trackNumber  int
	title        string
	credit       string
	durationMS   *int
	trackRef     string
	recordingRef string
	trackID      int64
	inLibrary    bool
}

// coverageRelease 是参与范围内的一个发行。
type coverageRelease struct {
	id       int64
	identity *domain.Identity
	title    *string
	credit   *string
	complete bool
	refs     int
	total    int
	entries  []coverageEntry
}

// releaseCoverageTx 计算「组内已有发行是否包含这首歌」的结论与输入确认闸门。
//
// 参与范围是目标组内除目标发行以外、至少有曲目引用的发行（与「已有发行」同一口径，
// 含同步专属曲目）。空发行行既不参与比较也不补取清单：否则预览或迁移留下的空发行
// 会让结论永远停在「不可判定」，把整组入库挡在人工确认后面。
//
// 只读已保存的清单与计划内的歌曲事实，事务内无网络；提交时重算并与计划比较。
func releaseCoverageTx(tx *sql.Tx, seed domain.AdmissionSeed, baseline *domain.AdmissionBaseline) error {
	baseline.Coverage = domain.CoverageNotApplicable
	baseline.Providers, baseline.Undecidable, baseline.NeedsTypedConfirm = nil, nil, false
	// 闸门只针对「一首歌进入某个发行」：整发行换组没有单首歌的归属变化；
	// 精确曲目已在库时这次提交不会让任何歌进入发行。
	if seed.MoveReleaseID != 0 || baseline.TargetGroupID == 0 || baseline.ExistingTrackID != 0 {
		return nil
	}
	song, moving, ok := admissionSongFacts(seed)
	if !ok {
		return nil
	}
	releases, err := coverageReleasesTx(tx, baseline.TargetGroupID, baseline.ExistingReleaseID, moving)
	if err != nil {
		return err
	}
	allRecordingDecidable := true
	recordingProvided := false
	for _, release := range releases {
		if release.identity != nil && !release.complete {
			allRecordingDecidable = false
			baseline.Undecidable = append(baseline.Undecidable, domain.ReleaseCoverage{ReleaseID: release.id, Identity: release.identity, Title: release.title, Credit: release.credit, Reason: "tracklist_incomplete"})
			continue
		}
		// 录音级可判定：同来源、这首歌有录音标识、清单每一条也都有录音标识。
		// 本地发行没有上游身份，只能按文本级参与。
		source := domain.ProviderID("")
		decidable := release.identity != nil && song.RecordingRef != "" && release.identity.Provider == song.Provider
		if release.identity != nil {
			source = release.identity.Provider
		}
		for _, entry := range release.entries {
			if entry.recordingRef == "" {
				decidable = false
			}
		}
		if !decidable {
			allRecordingDecidable = false
		}
		for _, entry := range release.entries {
			facts := matching.SongFacts{Provider: source, RecordingRef: entry.recordingRef, Title: entry.title, Credit: entry.credit, DurationMS: entry.durationMS}
			if release.identity == nil {
				facts.RecordingRef = ""
			}
			evidence := ""
			switch matching.CompareSong(song, facts) {
			case matching.SongSameRecording:
				evidence, recordingProvided = CoverageEvidenceRecording, true
			case matching.SongSameText:
				evidence = CoverageEvidenceText
			default:
				continue
			}
			title := entry.title
			baseline.Providers = append(baseline.Providers, domain.SongProvider{
				ReleaseID: release.id, Identity: release.identity, Title: release.title, Credit: release.credit,
				DiscNumber: entry.discNumber, TrackNumber: entry.trackNumber, TrackTitle: &title,
				DurationMS: entry.durationMS, TrackRef: entry.trackRef, RecordingRef: entry.recordingRef,
				Evidence: evidence, TrackID: entry.trackID, InLibrary: entry.inLibrary,
				TrackRefs: release.refs, TrackTotal: release.total,
			})
		}
	}
	// 主发行排在最前：曲目引用多者优先 → 清单总曲数多者 → 先建立者。
	// 基线参与计划哈希与提交时的相等比较，顺序必须稳定。
	sort.SliceStable(baseline.Providers, func(i, j int) bool {
		a, b := baseline.Providers[i], baseline.Providers[j]
		if a.ReleaseID != b.ReleaseID {
			return primaryBefore(a.TrackRefs, a.TrackTotal, a.ReleaseID, b.TrackRefs, b.TrackTotal, b.ReleaseID)
		}
		if a.DiscNumber != b.DiscNumber {
			return a.DiscNumber < b.DiscNumber
		}
		if a.TrackNumber != b.TrackNumber {
			return a.TrackNumber < b.TrackNumber
		}
		return a.TrackID < b.TrackID
	})
	switch {
	case recordingProvided:
		baseline.Coverage = domain.CoverageProvided
	case len(baseline.Providers) > 0:
		baseline.Coverage = domain.CoveragePossible
	case len(baseline.Undecidable) > 0:
		baseline.Coverage = domain.CoverageUnknown
	case allRecordingDecidable:
		baseline.Coverage = domain.CoverageAbsentProven
	default:
		baseline.Coverage = domain.CoverageAbsentText
	}
	switch baseline.Coverage {
	case domain.CoverageUnknown:
		baseline.NeedsTypedConfirm = true
	case domain.CoverageProvided, domain.CoveragePossible:
		// 目标发行本身参与主发行排序；尚未建立的发行没有曲目引用，必然排在已有
		// 提供者之后。这样把歌曲替换到主发行之后，不会再被组内其他提供者挡住。
		primary := baseline.Providers[0]
		baseline.NeedsTypedConfirm = true
		if baseline.ExistingReleaseID != 0 {
			refs, total, err := releaseWeightTx(tx, baseline.ExistingReleaseID)
			if err != nil {
				return err
			}
			baseline.NeedsTypedConfirm = !primaryBefore(refs, total, baseline.ExistingReleaseID, primary.TrackRefs, primary.TrackTotal, primary.ReleaseID)
		}
	}
	return nil
}

// primaryBefore 报告发行 a 是否排在发行 b 之前（更适合作为主发行）。
func primaryBefore(aRefs, aTotal int, aID int64, bRefs, bTotal int, bID int64) bool {
	if aRefs != bRefs {
		return aRefs > bRefs
	}
	if aTotal != bTotal {
		return aTotal > bTotal
	}
	return aID < bID
}

func releaseWeightTx(tx *sql.Tx, releaseID int64) (refs, total int, err error) {
	err = tx.QueryRow(`SELECT (SELECT count(*) FROM track WHERE release_id=?),(SELECT count(*) FROM release_track WHERE release_id=?)`, releaseID, releaseID).Scan(&refs, &total)
	return refs, total, err
}

// admissionSongFacts 取正在准入的这首歌：普通准入用计划里的曲目快照，编辑器迁移
// 用种子携带的歌曲事实（编辑计划的曲目快照是空的）。两处都没有歌时不适用。
//
// 第二个返回值是即将离开原发行的曲目（SQL 片段，形如 "1,2"；没有则为 "0"）。
func admissionSongFacts(seed domain.AdmissionSeed) (matching.SongFacts, string, bool) {
	if s := seed.CoverageSong; s != nil {
		facts := matching.SongFacts{Provider: s.Provider, RecordingRef: s.RecordingRef, Title: s.Title, Credit: s.Credit, DurationMS: s.DurationMS}
		ids := strconv.FormatInt(s.TrackID, 10)
		for _, id := range s.MovingTrackIDs {
			ids += "," + strconv.FormatInt(id, 10)
		}
		return facts, ids, facts.Title != "" || facts.RecordingRef != ""
	}
	track := seed.Snapshot.Track
	if track == nil || seed.EditJSON != "" {
		return matching.SongFacts{}, "0", false
	}
	facts := matching.SongFacts{Provider: track.Identity.Provider}
	if track.Title.State == domain.Value {
		facts.Title = track.Title.Value
	}
	if track.Credit.State == domain.Value {
		facts.Credit = track.Credit.Value.Text
	}
	if track.DurationMS.State == domain.Value {
		duration := track.DurationMS.Value
		facts.DurationMS = &duration
	}
	if track.Recording.State == domain.Value && track.Recording.Value.Provider == track.Identity.Provider {
		facts.RecordingRef = track.Recording.Value.Ref
	}
	return facts, "0", facts.Title != "" || facts.RecordingRef != ""
}

// coverageReleasesTx 读取参与范围内的发行。moving 是整数标识拼成的 SQL 片段。
func coverageReleasesTx(tx *sql.Tx, groupID, targetReleaseID int64, moving string) ([]coverageRelease, error) {
	rows, err := tx.Query(`SELECT r.id,r.provider,r.provider_ref,r.tracklist_state,c.title,c.credit,
 (SELECT count(*) FROM track t WHERE t.release_id=r.id AND t.id NOT IN (`+moving+`)),(SELECT count(*) FROM release_track rt WHERE rt.release_id=r.id)
 FROM release r JOIN release_current c ON c.release_id=r.id
 WHERE r.release_group_id=? AND r.id<>?
 AND EXISTS(SELECT 1 FROM track t WHERE t.release_id=r.id AND t.id NOT IN (`+moving+`))
 ORDER BY r.id`, groupID, targetReleaseID)
	if err != nil {
		return nil, err
	}
	var releases []coverageRelease
	for rows.Next() {
		var (
			release  coverageRelease
			provider *string
			ref      *string
			state    string
		)
		if err := rows.Scan(&release.id, &provider, &ref, &state, &release.title, &release.credit, &release.refs, &release.total); err != nil {
			rows.Close()
			return nil, err
		}
		release.complete = TracklistState(state) == TracklistComplete
		if provider != nil && ref != nil && *ref != "" {
			release.identity = &domain.Identity{Provider: domain.ProviderID(*provider), Ref: *ref}
		}
		releases = append(releases, release)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range releases {
		release := &releases[i]
		switch {
		case release.identity == nil:
			// 没有上游身份的本地发行用它的曲目行当清单，按文本级参与判定。
			release.entries, err = localCoverageEntriesTx(tx, release.id, moving)
		case release.complete:
			release.entries, err = tracklistCoverageEntriesTx(tx, release.id, release.identity.Provider)
		}
		if err != nil {
			return nil, err
		}
	}
	return releases, nil
}

func tracklistCoverageEntriesTx(tx *sql.Tx, releaseID int64, provider domain.ProviderID) ([]coverageEntry, error) {
	rows, err := tx.Query(`SELECT rt.disc_number,rt.track_number,COALESCE(rt.track_ref,''),COALESCE(rt.recording_ref,''),
 COALESCE(rt.title,''),COALESCE(rt.track_credit,''),rt.duration_ms,COALESCE(t.id,0),COALESCE(t.in_library,0)
 FROM release_track rt LEFT JOIN track t ON rt.track_ref IS NOT NULL AND t.match_source=? AND t.match_ref=rt.track_ref
 WHERE rt.release_id=? ORDER BY rt.disc_number,rt.track_number`, string(provider), releaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []coverageEntry
	for rows.Next() {
		var e coverageEntry
		if err := rows.Scan(&e.discNumber, &e.trackNumber, &e.trackRef, &e.recordingRef, &e.title, &e.credit, &e.durationMS, &e.trackID, &e.inLibrary); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func localCoverageEntriesTx(tx *sql.Tx, releaseID int64, moving string) ([]coverageEntry, error) {
	rows, err := tx.Query(`SELECT COALESCE(t.disc_number,0),COALESCE(t.track_number,0),COALESCE(t.title,''),COALESCE(t.credit,''),
 t.duration_ms,t.id,t.in_library FROM track_read t WHERE t.release_id=? AND t.id NOT IN (`+moving+`) ORDER BY t.id`, releaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []coverageEntry
	for rows.Next() {
		var e coverageEntry
		if err := rows.Scan(&e.discNumber, &e.trackNumber, &e.title, &e.credit, &e.durationMS, &e.trackID, &e.inLibrary); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// recordCoexistenceTx 记下用户输入确认词后允许并存的发行对：这次落入的发行，
// 与每个能提供（或无法排除能提供）这首歌的发行。归并提议据此不再提议这一对。
func recordCoexistenceTx(tx *sql.Tx, releaseID int64, baseline domain.AdmissionBaseline, now any) error {
	others := map[int64]bool{}
	for _, p := range baseline.Providers {
		others[p.ReleaseID] = true
	}
	for _, u := range baseline.Undecidable {
		others[u.ReleaseID] = true
	}
	for other := range others {
		if other == releaseID || other == 0 {
			continue
		}
		a, b := releaseID, other
		if a > b {
			a, b = b, a
		}
		if _, err := tx.Exec("INSERT INTO release_coexistence(release_a,release_b,created_at) VALUES(?,?,?) ON CONFLICT DO NOTHING", a, b, now); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE release_merge_proposal SET status='dismissed',updated_at=? WHERE release_a=? AND release_b=? AND status='pending'", now, a, b); err != nil {
			return err
		}
	}
	return nil
}
