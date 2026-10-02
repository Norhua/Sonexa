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
	if native != nil && native.Provider != "" && native.Ref != "" {
		var id int64
		err := tx.QueryRow("SELECT group_id FROM release_group_ref WHERE provider=? AND group_ref=?", native.Provider, native.Ref).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return out, err
		}
		if err == nil {
			groups[id] = true
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
	for _, e := range evidence {
		for _, name := range names {
			if name[0] != "" && name[1] != "" && e.Title == name[0] && e.Credit == name[1] {
				groups[e.GroupID] = true
				out.Evidence = append(out.Evidence, e)
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
			out.Evidence = append(out.Evidence, domain.GroupEvidence{GroupID: groupID, Kind: "existing-release", SourceID: strconv.FormatInt(releaseID, 10)})
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
	sort.Slice(out.Evidence, func(i, j int) bool {
		a, b := out.Evidence[i], out.Evidence[j]
		if a.GroupID != b.GroupID {
			return a.GroupID < b.GroupID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.SourceID < b.SourceID
	})
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
	tx, err := s.db.Begin()
	if err != nil {
		return domain.GroupResolution{}, err
	}
	defer tx.Rollback()
	return resolveGroupTx(tx, snapshot)
}
func rebuildGroupKeysTx(tx *sql.Tx) error {
	evidence, err := groupNameEvidenceTx(tx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM release_group_match_key"); err != nil {
		return err
	}
	for _, e := range evidence {
		if _, err := tx.Exec("INSERT INTO release_group_match_key(group_id,source_kind,source_id,normalizer_version,title_key,credit_key) VALUES(?,?,?,?,?,?)", e.GroupID, e.Kind, e.SourceID, domain.GroupNormalizerVersion, e.Title, e.Credit); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) RebuildGroupEvidence() error { return s.withTx(rebuildGroupKeysTx) }

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
		if err := tx.QueryRow("SELECT status,updated_at FROM import_item WHERE id=?", seed.ImportItemID).Scan(&baseline.ItemState, &baseline.ItemRevision); err != nil {
			return baseline, err
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
	baseline.Conflict = len(resolution.Groups) > 1 && seed.MergeTargetID == 0
	if len(resolution.Groups) == 1 {
		baseline.TargetGroupID = resolution.Groups[0].ID
	}
	if seed.MergeTargetID != 0 {
		found := false
		for _, group := range resolution.Groups {
			if group.ID == seed.MergeTargetID {
				found = true
			}
		}
		if !found {
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
	baseline.RequiresConfirmation = baseline.ExistingTrackID == 0 && (len(resolution.Groups) > 1 || baseline.NewNativeMapping || (baseline.TargetGroupID != 0 && baseline.ExistingReleaseID == 0))
	return baseline, nil
}
func (s *Store) PreviewAdmission(seed domain.AdmissionSeed) (domain.AdmissionBaseline, error) {
	tx, err := s.db.Begin()
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
