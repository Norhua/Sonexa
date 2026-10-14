package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
)

func ensureDomainArtistTx(tx *sql.Tx, a domain.Artist) (int64, error) {
	if strings.TrimSpace(a.Name) == "" {
		return 0, fmt.Errorf("艺术家名称不能为空")
	}
	if a.Identity == nil {
		return upsertArtistTx(tx, &Artist{Name: a.Name, SortName: nonblank(a.SortName), Type: nonblank(a.Type)})
	}
	var id int64
	err := tx.QueryRow("SELECT id FROM artist WHERE provider=? AND provider_ref=?", a.Identity.Provider, a.Identity.Ref).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	now := time.Now().UTC()
	res, err := tx.Exec("INSERT INTO artist(name,sort_name,type,provider,provider_ref,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", a.Name, nonblank(a.SortName), nonblank(a.Type), a.Identity.Provider, a.Identity.Ref, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func creditMembers(credit domain.Credit) ([]domain.CreditMember, bool) {
	rules := matching.DefaultRules()
	if len(credit.Members) == 0 {
		parsed := rules.ParseCredit(credit.Text)
		if parsed.Uncertain {
			return nil, true
		}
		var out []domain.CreditMember
		for _, a := range parsed.Artists {
			out = append(out, domain.CreditMember{Artist: domain.Artist{Name: a.Name, Type: a.Type}, CreditedName: a.Name, Start: a.Start, End: a.End})
		}
		return out, false
	}
	var out []domain.CreditMember
	segments := matching.FindBracketSegments(credit.Text)
	for i := 0; i < len(credit.Members); {
		m := credit.Members[i]
		if m.Start < 0 || m.End > len(credit.Text) || m.Start >= m.End || credit.Text[m.Start:m.End] != m.CreditedName {
			return nil, true
		}
		end := m.End
		for _, seg := range segments {
			if seg.Start >= m.Start && seg.End > end && (seg.Start <= end || strings.TrimSpace(credit.Text[end:seg.Start]) == "") && rules.Classify(seg).Kind == matching.KindCV {
				end = seg.End
			}
		}
		j := i + 1
		for j < len(credit.Members) && credit.Members[j].Start < end {
			if credit.Members[j].End > end {
				return nil, true
			}
			j++
		}
		if end != m.End {
			parsed := rules.ParseCreditName(credit.Text[m.Start:end])
			if parsed.Uncertain || len(parsed.Artists) != 1 {
				return nil, true
			}
			m.End = end
			m.CreditedName = credit.Text[m.Start:end]
			m.Artist = domain.Artist{Name: parsed.Artists[0].Name, Type: parsed.Artists[0].Type}
		}
		// 上游可能把完整 CV 写法放在一个成员里，但实体身份仍指向声优。
		// 当前署名保留 CV 整体；独立的演唱关系继续保留原来源身份。
		for _, seg := range matching.FindBracketSegments(m.CreditedName) {
			if rules.Classify(seg).Kind == matching.KindCV {
				parsed := rules.ParseCreditName(m.CreditedName)
				if parsed.Uncertain || len(parsed.Artists) != 1 {
					return nil, true
				}
				m.Artist = domain.Artist{Name: parsed.Artists[0].Name, Type: parsed.Artists[0].Type}
				break
			}
		}
		out = append(out, m)
		i = j
	}
	return out, false
}

func refreshCreditsTx(tx *sql.Tx, kind string, id int64) error {
	if kind == "release_group" {
		return nil
	}
	key := entityKey(kind)
	var text *string
	if err := tx.QueryRow("SELECT credit FROM "+kind+"_current WHERE "+key+"=?", id).Scan(&text); err != nil {
		return err
	}
	credit := domain.Credit{Text: deref(text)}
	origins, err := readOriginsTx(tx, kind, id)
	if err != nil {
		return err
	}
	if origins["credit"].Mode == domain.Follow {
		var body string
		if err := tx.QueryRow("SELECT body FROM "+kind+"_snapshot WHERE "+key+"=?", id).Scan(&body); err != nil && err != sql.ErrNoRows {
			return err
		}
		var snapshot struct {
			Credit domain.Field[domain.Credit] `json:"credit"`
		}
		if body != "" {
			if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
				return err
			}
			if snapshot.Credit.State == domain.Value && snapshot.Credit.Value.Text == credit.Text {
				credit = snapshot.Credit.Value
			}
		}
	}
	members, uncertain := creditMembers(credit)
	if _, err := tx.Exec("DELETE FROM "+kind+"_credit WHERE "+key+"=?", id); err != nil {
		return err
	}
	if kind == "track" {
		var reason *string
		if uncertain {
			reason = nonblank("署名拆分不确定，需人工确认")
		}
		if _, err := tx.Exec("UPDATE track SET credit_review=COALESCE(?,credit_review) WHERE id=?", reason, id); err != nil {
			return err
		}
	}
	if uncertain {
		if kind == "track" {
			return refreshCVVocalsTx(tx, id, nil)
		}
		return nil
	}
	var actors []int64
	position := 0
	for _, m := range members {
		artist, err := ensureDomainArtistTx(tx, m.Artist)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO "+kind+"_credit("+key+",artist_id,position,credited_name,span_start,span_end) VALUES(?,?,?,?,?,?)", id, artist, position, m.CreditedName, m.Start, m.End); err != nil {
			return err
		}
		position++
		actor := cvCreditActor(credit, m)
		if actor == nil {
			continue
		}
		if kind == "track" {
			resolved, err := resolveCVActorTx(tx, id, *actor)
			if err != nil {
				return err
			}
			actor = &resolved
		}
		actorID, err := ensureDomainArtistTx(tx, *actor)
		if err != nil {
			return err
		}
		// 声优也是署名成员，但不产生与完整 CV 链接重叠的可点击区间。
		if _, err := tx.Exec("INSERT INTO "+kind+"_credit("+key+",artist_id,position,credited_name) VALUES(?,?,?,?)", id, actorID, position, actor.Name); err != nil {
			return err
		}
		position++
		actors = append(actors, actorID)
	}
	if kind == "track" {
		return refreshCVVocalsTx(tx, id, actors)
	}
	return nil
}

func syncRolesTx(tx *sql.Tx, id int64, roles map[string]domain.Field[[]domain.Role], source string, editing bool) error {
	origins, err := readOriginsTx(tx, "track", id)
	if err != nil {
		return err
	}
	var snapshot domain.TrackSnapshot
	if editing {
		_, boundSource, err := boundSnapshotTx(tx, "track", id)
		if err != nil {
			return err
		}
		source = boundSource
		if source != "" {
			var body string
			if err := tx.QueryRow("SELECT body FROM track_snapshot WHERE track_id=?", id).Scan(&body); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
				return err
			}
		}
	}
	changed := false
	for role, field := range roles {
		if !field.IsKnown() || !IsValidRole(ArtistRole(role)) || role == "album_artist" {
			continue
		}
		key := "roles." + role
		if !editing && origins[key].Mode == domain.User {
			continue
		}
		mode, fieldSource := domain.Follow, source
		if source == "" || (editing && !roleFieldEqual(field, snapshot.Roles[role])) {
			mode, fieldSource = domain.User, "user"
		}
		changed = true
		if _, err := tx.Exec("DELETE FROM track_artist WHERE track_id=? AND role=?", id, role); err != nil {
			return err
		}
		for i, r := range field.Value {
			artist, err := ensureDomainArtistTx(tx, r.Artist)
			if err != nil {
				return err
			}
			attrs, err := json.Marshal(r.Attributes)
			if err != nil {
				return err
			}
			if _, err := tx.Exec("INSERT INTO track_artist(track_id,artist_id,role,attribute,attributes_json,position,source) VALUES(?,?,?,?,?,?,?)", id, artist, role, nonblank(strings.Join(r.Attributes, ", ")), string(attrs), i, fieldSource); err != nil {
				return err
			}
		}
		origins[key] = FieldOrigin{Mode: mode, Source: fieldSource}
	}
	b, err := json.Marshal(origins)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE track_current SET states_json=? WHERE track_id=?", string(b), id)
	if err == nil && changed {
		if err := refreshCreditsTx(tx, "track", id); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE track SET revision=revision+1,updated_at=? WHERE id=?", time.Now().UTC(), id)
	}
	return err
}

func roleFieldEqual(a, b domain.Field[[]domain.Role]) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	var left, right rawField
	_ = json.Unmarshal(x, &left)
	_ = json.Unmarshal(y, &right)
	return rawEqual(left, right)
}
