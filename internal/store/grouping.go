package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/norhua/sonexa/internal/domain"
)

// groupNameEvidenceTx 从当前值及有效绑定快照重建证据，不把历史名称当成永久别名。
func groupNameEvidenceTx(tx *sql.Tx) ([]domain.GroupEvidence, error) {
	rows, err := tx.Query(`SELECT g.id,'group-current',CAST(g.id AS TEXT),c.title,c.credit FROM release_group g JOIN release_group_current c ON c.group_id=g.id
 UNION ALL SELECT r.release_group_id,'release-current',CAST(r.id AS TEXT),c.title,c.credit FROM release r JOIN release_current c ON c.release_id=r.id
 UNION ALL SELECT r.release_group_id,'release-snapshot',CAST(r.id AS TEXT),json_extract(s.body,'$.title.value'),json_extract(s.body,'$.credit.value.text') FROM release r JOIN release_snapshot s ON s.release_id=r.id WHERE r.provider=json_extract(s.body,'$.identity.provider') AND r.provider_ref=json_extract(s.body,'$.identity.ref')
 UNION ALL SELECT gr.group_id,'group-snapshot',gr.provider||':'||gr.group_ref,json_extract(s.body,'$.title.value'),json_extract(s.body,'$.credit.value.text') FROM release_group_ref gr JOIN release_group_snapshot s ON s.provider=gr.provider AND s.group_ref=gr.group_ref AND s.group_id=gr.group_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GroupEvidence{}
	for rows.Next() {
		var evidence domain.GroupEvidence
		var title, credit *string
		if err := rows.Scan(&evidence.GroupID, &evidence.Kind, &evidence.SourceID, &title, &credit); err != nil {
			return nil, err
		}
		if title == nil || credit == nil {
			continue
		}
		evidence.Title, evidence.Credit = domain.NormalizeGroupName(*title), domain.NormalizeGroupName(*credit)
		if evidence.Title != "" && evidence.Credit != "" {
			out = append(out, evidence)
		}
	}
	return out, rows.Err()
}
func resolveGroupTx(tx *sql.Tx, snapshot domain.Snapshot, current ...*domain.ReleaseSnapshot) (domain.GroupResolution, error) {
	out := domain.GroupResolution{Evidence: []domain.GroupEvidence{}, Groups: []domain.GroupMatch{}}
	if snapshot.Release == nil {
		out.Insufficient = true
		return out, nil
	}
	r := snapshot.Release
	title, credit := domain.NameEvidence(r.Title, r.Credit)
	native := r.Group
	if native == nil && snapshot.Group != nil && snapshot.Group.Identity.Ref != "" {
		native = &snapshot.Group.Identity
	}
	out.Insufficient = (native == nil || native.Ref == "" || native.Provider == "") && title == ""
	groups := map[int64]bool{}
	// anchors 是由强证据（原生组映射、发行已在库）命中的组；名称命中的组要
	// 先过两道否决才进入结果。
	anchors := map[int64]bool{}
	hasNative := native != nil && native.Provider != "" && native.Ref != ""
	if hasNative {
		var id int64
		err := tx.QueryRow("SELECT group_id FROM release_group_ref WHERE provider=? AND group_ref=?", native.Provider, native.Ref).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return out, err
		}
		if err == nil {
			groups[id] = true
			anchors[id] = true
			out.Evidence = append(out.Evidence, domain.GroupEvidence{GroupID: id, Kind: "native", SourceID: string(native.Provider) + ":" + native.Ref, Native: native})
		}
	}
	names := [][2]string{{title, credit}}
	for _, values := range current {
		if values != nil {
			t, c := domain.NameEvidence(values.Title, values.Credit)
			names = append(names, [2]string{t, c})
		}
	}
	evidence, err := groupNameEvidenceTx(tx)
	if err != nil {
		return out, err
	}
	named := []domain.GroupEvidence{}
	for _, e := range evidence {
		for _, name := range names {
			if name[0] != "" && name[1] != "" && e.Title == name[0] && e.Credit == name[1] {
				named = append(named, e)
				break
			}
		}
	}
	for _, name := range names {
		if name[0] != "" && name[1] != "" {
			out.Insufficient = false
		}
	}
	if r.Identity.Provider != "" && r.Identity.Ref != "" {
		var groupID, releaseID int64
		err := tx.QueryRow("SELECT id,release_group_id FROM release WHERE provider=? AND provider_ref=?", r.Identity.Provider, r.Identity.Ref).Scan(&releaseID, &groupID)
		if err != nil && err != sql.ErrNoRows {
			return out, err
		}
		if err == nil {
			groups[groupID] = true
			anchors[groupID] = true
			out.Evidence = append(out.Evidence, domain.GroupEvidence{GroupID: groupID, Kind: "existing-release", SourceID: strconv.FormatInt(releaseID, 10)})
		}
	}
	// 名称命中的两道否决。结果按组缓存：同一组可能有多条名称证据。
	verdict := map[int64]string{}
	for _, e := range named {
		reason, seen := verdict[e.GroupID]
		if !seen && !anchors[e.GroupID] {
			// 1) 同来源原生组否决：上游已经声明这是另一个发行组。候选的原生组没有
			//    映射到该组，而该组已映射了同一来源的原生组——同名的专辑与单曲
			//    就是这种情形，名称不构成同组依据。
			if hasNative {
				var count int
				if err := tx.QueryRow("SELECT count(*) FROM release_group_ref WHERE group_id=? AND provider=?", e.GroupID, native.Provider).Scan(&count); err != nil {
					return out, err
				}
				if count > 0 {
					reason = "native_mismatch"
				}
			}
			// 2) 否定决定：用户判定过该组与某个锚定组不是同一组。只在有锚定组
			//    时过滤；全新的发行没有锚，仍会被询问一次。
			if reason == "" {
				for anchor := range anchors {
					a, b := anchor, e.GroupID
					if a > b {
						a, b = b, a
					}
					var count int
					if err := tx.QueryRow("SELECT count(*) FROM release_group_separation WHERE group_a=? AND group_b=?", a, b).Scan(&count); err != nil {
						return out, err
					}
					if count > 0 {
						reason = "separated"
						break
					}
				}
			}
			verdict[e.GroupID] = reason
		}
		if reason != "" {
			e.Reason = reason
			out.Rejected = append(out.Rejected, e)
			continue
		}
		groups[e.GroupID] = true
		out.Evidence = append(out.Evidence, e)
	}
	// 名称近似只作提示：没有任何命中时，列出去掉版本后缀与署名附注后同名的组。
	if len(groups) == 0 && r.Title.State == domain.Value && r.Credit.State == domain.Value {
		looseTitle, looseCredit := domain.LooseGroupName(r.Title.Value), domain.LooseGroupName(r.Credit.Value.Text)
		seen := map[int64]bool{}
		for _, e := range evidence {
			if seen[e.GroupID] || looseTitle == "" || looseCredit == "" {
				continue
			}
			if domain.LooseGroupName(e.Title) == looseTitle && domain.LooseGroupName(e.Credit) == looseCredit {
				seen[e.GroupID] = true
				out.Similar = append(out.Similar, e)
			}
		}
	}
	ids := make([]int64, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		group, err := groupMembersTx(tx, id)
		if err != nil {
			return out, err
		}
		out.Groups = append(out.Groups, group)
	}
	for _, list := range [][]domain.GroupEvidence{out.Evidence, out.Rejected, out.Similar} {
		sort.Slice(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.GroupID != b.GroupID {
				return a.GroupID < b.GroupID
			}
			if a.Kind != b.Kind {
				return a.Kind < b.Kind
			}
			return a.SourceID < b.SourceID
		})
	}
	return out, nil
}
func groupMembersTx(tx *sql.Tx, id int64) (domain.GroupMatch, error) {
	group := domain.GroupMatch{ID: id, Members: []domain.ReleaseMember{}}
	if err := tx.QueryRow("SELECT c.title,c.credit,g.revision FROM release_group g JOIN release_group_current c ON c.group_id=g.id WHERE g.id=?", id).Scan(&group.Title, &group.Credit, &group.Revision); err != nil {
		return group, err
	}
	rows, err := tx.Query("SELECT r.id,r.provider,r.provider_ref,c.title,c.credit,r.revision FROM release r JOIN release_current c ON c.release_id=r.id WHERE r.release_group_id=? ORDER BY r.id", id)
	if err != nil {
		return group, err
	}
	for rows.Next() {
		member := domain.ReleaseMember{GroupID: id, TrackIDs: []int64{}}
		var provider, ref *string
		if err := rows.Scan(&member.ID, &provider, &ref, &member.Title, &member.Credit, &member.Revision); err != nil {
			rows.Close()
			return group, err
		}
		if provider != nil && ref != nil {
			member.Identity = &domain.Identity{Provider: domain.ProviderID(*provider), Ref: *ref}
		}
		group.Members = append(group.Members, member)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return group, err
	}
	for i := range group.Members {
		rows, err := tx.Query("SELECT id FROM track WHERE release_id=? ORDER BY id", group.Members[i].ID)
		if err != nil {
			return group, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return group, err
			}
			group.Members[i].TrackIDs = append(group.Members[i].TrackIDs, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return group, err
		}
	}
	return group, nil
}
func (s *Store) ResolveGroup(snapshot domain.Snapshot) (domain.GroupResolution, error) {
	tx, err := s.beginRead()
	if err != nil {
		return domain.GroupResolution{}, err
	}
	defer tx.Rollback()
	return resolveGroupTx(tx, snapshot)
}
func admissionBaselineTx(tx *sql.Tx, seed domain.AdmissionSeed) (domain.AdmissionBaseline, error) {
	var baseline domain.AdmissionBaseline
	if seed.Snapshot.Track == nil {
		return baseline, fmt.Errorf("缺少曲目草稿")
	}
	identity := seed.Snapshot.Track.Identity
	if (identity.Provider == "") != (identity.Ref == "") {
		return baseline, ErrSnapshotTarget
	}
	if identity.Ref != "" {
		err := tx.QueryRow("SELECT id FROM track WHERE match_source=? AND match_ref=?", identity.Provider, identity.Ref).Scan(&baseline.ExistingTrackID)
		if err != nil && err != sql.ErrNoRows {
			return baseline, err
		}
	}
	if seed.ImportItemID != 0 {
		if err := tx.QueryRow("SELECT status,CAST(revision AS TEXT)||':'||CAST(search_revision AS TEXT)||':'||updated_at FROM import_item WHERE id=?", seed.ImportItemID).Scan(&baseline.ItemState, &baseline.ItemRevision); err != nil {
			return baseline, err
		}
		if ItemStatus(baseline.ItemState).IsTerminal() {
			return baseline, ErrPlanStale
		}
		var err error
		baseline.Destinations, err = importTargetsTx(tx, seed.ImportItemID)
		if err != nil {
			return baseline, err
		}
		if len(baseline.Destinations) == 0 {
			return baseline, ErrPlanStale
		}
	}
	if r := seed.Snapshot.Release; r != nil {
		if (r.Identity.Provider == "") != (r.Identity.Ref == "") {
			return baseline, ErrSnapshotTarget
		}
		if identity.Ref != "" && r.Identity.Ref != "" && identity.Provider != r.Identity.Provider {
			return baseline, ErrSnapshotTarget
		}
		if seed.Snapshot.Track.Release != nil && *seed.Snapshot.Track.Release != r.Identity {
			return baseline, ErrSnapshotTarget
		}
		if r.Identity.Ref != "" {
			err := tx.QueryRow("SELECT id FROM release WHERE provider=? AND provider_ref=?", r.Identity.Provider, r.Identity.Ref).Scan(&baseline.ExistingReleaseID)
			if err != nil && err != sql.ErrNoRows {
				return baseline, err
			}
		}
	}
	if seed.MoveReleaseID != 0 {
		if baseline.ExistingReleaseID != 0 && baseline.ExistingReleaseID != seed.MoveReleaseID {
			return baseline, ErrPlanStale
		}
		var nativeProvider, nativeRef *string
		if err := tx.QueryRow("SELECT provider,provider_ref FROM release WHERE id=?", seed.MoveReleaseID).Scan(&nativeProvider, &nativeRef); err != nil {
			return baseline, err
		}
		if seed.Snapshot.Release == nil || deref(nativeProvider) != string(seed.Snapshot.Release.Identity.Provider) || deref(nativeRef) != seed.Snapshot.Release.Identity.Ref {
			return baseline, ErrSnapshotTarget
		}
		baseline.ExistingReleaseID = seed.MoveReleaseID
	}
	resolution, err := resolveGroupTx(tx, seed.Snapshot, seed.CurrentRelease)
	if err != nil {
		return baseline, err
	}
	baseline.Resolution = resolution
	actions := 0
	for _, chosen := range []bool{seed.MergeTargetID != 0, seed.JoinGroupID != 0, seed.KeepSeparate} {
		if chosen {
			actions++
		}
	}
	if actions > 1 {
		return baseline, fmt.Errorf("判组动作互斥")
	}
	baseline.Conflict = len(resolution.Groups) > 1 && actions == 0
	if len(resolution.Groups) == 1 {
		baseline.TargetGroupID = resolution.Groups[0].ID
	}
	inResolution := func(id int64) bool {
		for _, group := range resolution.Groups {
			if group.ID == id {
				return true
			}
		}
		return false
	}
	if seed.JoinGroupID != 0 {
		if !inResolution(seed.JoinGroupID) {
			return baseline, fmt.Errorf("加入的组不在当前证据命中范围")
		}
		baseline.TargetGroupID = seed.JoinGroupID
	}
	if seed.KeepSeparate {
		// 发行已经在某个组里，或这是整发行换组，就谈不上「独立成组」。
		if baseline.ExistingReleaseID != 0 || seed.MoveReleaseID != 0 || len(resolution.Groups) == 0 {
			return baseline, fmt.Errorf("当前发行不能独立成组")
		}
		baseline.TargetGroupID = 0
	}
	if seed.MergeTargetID != 0 {
		// 合组目标必须是真实存在的组：命中组之外也允许（用户明确要把两者并到一起，
		// 例如把先前判为「不是同一组」的两组合并回来）。命中组仍全部并入它。
		var exists int
		if err := tx.QueryRow("SELECT count(*) FROM release_group WHERE id=?", seed.MergeTargetID).Scan(&exists); err != nil {
			return baseline, err
		}
		if exists == 0 {
			return baseline, fmt.Errorf("合组目标不在当前证据命中范围")
		}
		baseline.TargetGroupID = seed.MergeTargetID
	}
	if baseline.TargetGroupID != 0 && seed.Snapshot.Release != nil {
		native := seed.Snapshot.Release.Group
		if native == nil && seed.Snapshot.Group != nil && seed.Snapshot.Group.Identity.Ref != "" {
			native = &seed.Snapshot.Group.Identity
		}
		if native != nil && native.Ref != "" {
			var count int
			if err := tx.QueryRow("SELECT count(*) FROM release_group_ref WHERE provider=? AND group_ref=?", native.Provider, native.Ref).Scan(&count); err != nil {
				return baseline, err
			}
			baseline.NewNativeMapping = count == 0
		}
	}
	// 「保持独立成组」本身就是用户对这次判组的明确决定，不再追加成员确认。
	groupChange := len(resolution.Groups) > 1 || baseline.NewNativeMapping || (baseline.TargetGroupID != 0 && baseline.ExistingReleaseID == 0)
	baseline.RequiresConfirmation = baseline.ExistingTrackID == 0 && groupChange && !seed.KeepSeparate
	return baseline, nil
}
func (s *Store) PreviewAdmission(seed domain.AdmissionSeed) (domain.AdmissionBaseline, error) {
	tx, err := s.beginRead()
	if err != nil {
		return domain.AdmissionBaseline{}, err
	}
	defer tx.Rollback()
	return admissionBaselineTx(tx, seed)
}
func sameAdmissionBaseline(a, b domain.AdmissionBaseline) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
