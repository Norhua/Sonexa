package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

// ReleaseMergeSide 是归并提议里的一个发行。
type ReleaseMergeSide struct {
	ReleaseID int64            `json:"releaseId"`
	Identity  *domain.Identity `json:"identity,omitempty"`
	Title     *string          `json:"title"`
	Credit    *string          `json:"credit"`
	TrackRefs int              `json:"trackRefs"`
}

// ReleaseMergeProposal 是「同组两个发行曲目相同」的提议。只供查看与忽略：
// 执行归并要把一个发行的曲目改绑到另一个发行的对应条目，会改写曲目标识，本期不做。
type ReleaseMergeProposal struct {
	ID         int64            `json:"id"`
	GroupID    int64            `json:"groupId"`
	A          ReleaseMergeSide `json:"a"`
	B          ReleaseMergeSide `json:"b"`
	TrackTotal int              `json:"trackTotal"`
	Status     string           `json:"status"`
}

// completeRecordingsTx 返回一个发行按录音排序的清单；不满足比较条件时 ok 为假：
// 需有上游身份、清单 complete、至少一条曲目引用，且每条清单都有录音标识。
func completeRecordingsTx(tx *sql.Tx, releaseID int64) (groupID int64, provider string, recordings []string, ok bool, err error) {
	var source, ref *string
	var state string
	var refs int
	err = tx.QueryRow(`SELECT r.release_group_id,r.provider,r.provider_ref,r.tracklist_state,(SELECT count(*) FROM track t WHERE t.release_id=r.id)
 FROM release r WHERE r.id=?`, releaseID).Scan(&groupID, &source, &ref, &state, &refs)
	if err == sql.ErrNoRows {
		return 0, "", nil, false, nil
	}
	if err != nil || source == nil || ref == nil || TracklistState(state) != TracklistComplete || refs == 0 {
		return groupID, "", nil, false, err
	}
	rows, err := tx.Query("SELECT COALESCE(recording_ref,'') FROM release_track WHERE release_id=?", releaseID)
	if err != nil {
		return groupID, "", nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var recording string
		if err := rows.Scan(&recording); err != nil {
			return groupID, "", nil, false, err
		}
		if recording == "" {
			return groupID, "", nil, false, nil
		}
		recordings = append(recordings, recording)
	}
	if err := rows.Err(); err != nil {
		return groupID, "", nil, false, err
	}
	sort.Strings(recordings)
	return groupID, *source, recordings, len(recordings) > 0, nil
}

// proposeReleaseMergeTx 在一个发行的清单完整后，与同组、同来源、清单同为完整的
// 其他发行比较录音多重集（忽略碟序、曲序）；相同则生成一条提议。
//
// 用户输入确认词允许并存的一对不提议；证据未变的已有提议（含已忽略的）不重复生成。
// 不改任何关系。「集合相同」只用在这里，不参与任何闸门。
func proposeReleaseMergeTx(tx *sql.Tx, releaseID int64) error {
	groupID, provider, recordings, ok, err := completeRecordingsTx(tx, releaseID)
	if err != nil || !ok {
		return err
	}
	rows, err := tx.Query("SELECT id FROM release WHERE release_group_id=? AND provider=? AND id<>? AND tracklist_state=? ORDER BY id", groupID, provider, releaseID, string(TracklistComplete))
	if err != nil {
		return err
	}
	var others []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		others = append(others, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	signature := strings.Join(recordings, "\x00")
	sum := sha256.Sum256([]byte(provider + "\x00" + signature))
	hash := hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	for _, other := range others {
		_, _, theirs, ok, err := completeRecordingsTx(tx, other)
		if err != nil {
			return err
		}
		if !ok || strings.Join(theirs, "\x00") != signature {
			continue
		}
		a, b := releaseID, other
		if a > b {
			a, b = b, a
		}
		var coexist int
		if err := tx.QueryRow("SELECT count(*) FROM release_coexistence WHERE release_a=? AND release_b=?", a, b).Scan(&coexist); err != nil {
			return err
		}
		if coexist > 0 {
			continue
		}
		evidence, _ := json.Marshal(struct {
			Provider   string `json:"provider"`
			TrackTotal int    `json:"trackTotal"`
		}{provider, len(recordings)})
		if _, err := tx.Exec(`INSERT INTO release_merge_proposal(release_a,release_b,evidence_json,evidence_hash,created_at,updated_at)
 VALUES(?,?,?,?,?,?) ON CONFLICT(release_a,release_b,evidence_hash) DO NOTHING`, a, b, string(evidence), hash, now, now); err != nil {
			return err
		}
	}
	return nil
}

// ListReleaseMergeProposals 列出待处理的归并提议。
func (s *Store) ListReleaseMergeProposals() ([]ReleaseMergeProposal, error) {
	rows, err := s.db.Query(`SELECT p.id,p.status,json_extract(p.evidence_json,'$.trackTotal'),ra.release_group_id,
 ra.id,ra.provider,ra.provider_ref,ca.title,ca.credit,(SELECT count(*) FROM track t WHERE t.release_id=ra.id),
 rb.id,rb.provider,rb.provider_ref,cb.title,cb.credit,(SELECT count(*) FROM track t WHERE t.release_id=rb.id)
 FROM release_merge_proposal p
 JOIN release ra ON ra.id=p.release_a JOIN release_current ca ON ca.release_id=ra.id
 JOIN release rb ON rb.id=p.release_b JOIN release_current cb ON cb.release_id=rb.id
 WHERE p.status='pending' AND ra.release_group_id=rb.release_group_id ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReleaseMergeProposal{}
	for rows.Next() {
		var p ReleaseMergeProposal
		var ap, ar, bp, br *string
		if err := rows.Scan(&p.ID, &p.Status, &p.TrackTotal, &p.GroupID,
			&p.A.ReleaseID, &ap, &ar, &p.A.Title, &p.A.Credit, &p.A.TrackRefs,
			&p.B.ReleaseID, &bp, &br, &p.B.Title, &p.B.Credit, &p.B.TrackRefs); err != nil {
			return nil, err
		}
		if ap != nil && ar != nil {
			p.A.Identity = &domain.Identity{Provider: domain.ProviderID(*ap), Ref: *ar}
		}
		if bp != nil && br != nil {
			p.B.Identity = &domain.Identity{Provider: domain.ProviderID(*bp), Ref: *br}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DismissReleaseMergeProposal 忽略一条提议；证据不变时不会再次生成。
func (s *Store) DismissReleaseMergeProposal(id int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		result, err := tx.Exec("UPDATE release_merge_proposal SET status='dismissed',updated_at=? WHERE id=? AND status='pending'", time.Now().UTC(), id)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return ErrPlanStale
		}
		return nil
	})
}
