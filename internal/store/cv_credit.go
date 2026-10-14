package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
)

// CVCreditSource 是当前署名的派生来源，不是上游提供的歌曲演唱事实。
const CVCreditSource = "credit_cv"

// cvCreditActor 保留完整 CV 成员，另外识别唯一的声优。组合名字、不可靠或
// 多个 CV 注记不派生。身份只接受该区间内的上游声优部件或本曲唯一同名角色，
// 绝不把角色的标识赋给声优，也不以全库同名查找来合并人物。
func cvCreditActor(credit domain.Credit, member domain.CreditMember) *domain.Artist {
	rules := matching.DefaultRules()
	parsed := rules.ParseCreditName(member.CreditedName)
	if parsed.Uncertain || len(parsed.Artists) != 1 || parsed.Artists[0].VoiceActor == "" {
		return nil
	}
	segments := matching.FindBracketSegments(member.CreditedName)
	if len(segments) != 1 || segments[0].End != len(member.CreditedName) {
		return nil
	}
	name := parsed.Artists[0].VoiceActor
	if strings.IndexFunc(name, unicode.IsLetter) < 0 || strings.ContainsAny(name, ",，/／") {
		return nil
	}
	actor := rules.ParseCredit(name)
	if actor.Uncertain || len(actor.Artists) != 1 || actor.Artists[0].VoiceActor != "" || len(matching.FindBracketSegments(name)) > 0 {
		return nil
	}
	result := domain.Artist{Name: name, Type: "person"}
	for _, part := range credit.Members {
		if part.Start < member.Start || part.End > member.End || part.Start >= part.End ||
			part.End > len(credit.Text) || credit.Text[part.Start:part.End] != part.CreditedName || part.Artist.Type == "character" {
			continue
		}
		if part.CreditedName == name || part.Artist.Name == name {
			if result.Identity != nil && part.Artist.Identity != nil && *result.Identity != *part.Artist.Identity {
				return nil // 同名但上游身份冲突，不猜测。
			}
			result = part.Artist
		}
	}
	return &result
}

func resolveCVActorTx(tx *sql.Tx, trackID int64, actor domain.Artist) (domain.Artist, error) {
	if actor.Identity != nil {
		return actor, nil
	}
	rows, err := tx.Query(`SELECT DISTINCT a.provider,a.provider_ref FROM track_artist ta
		JOIN artist a ON a.id=ta.artist_id WHERE ta.track_id=? AND a.name=?
		AND a.provider IS NOT NULL AND (ta.source IS NULL OR ta.source<>?)`, trackID, actor.Name, CVCreditSource)
	if err != nil {
		return actor, err
	}
	defer rows.Close()
	var identities []domain.Identity
	for rows.Next() {
		var identity domain.Identity
		if err := rows.Scan(&identity.Provider, &identity.Ref); err != nil {
			return actor, err
		}
		identities = append(identities, identity)
	}
	if len(identities) == 1 {
		actor.Identity = &identities[0]
	}
	return actor, rows.Err()
}

// refreshCVVocalsTx 只在没有明确演唱集合时兜底。显式 empty/用户清空也通过
// 字段来源保护。每次重建先移除旧派生关系，改署名不会留下旧声优的演唱关系。
// 上游快照始终保持原样，编辑器能独立补录/清空演唱者。
func refreshCVVocalsTx(tx *sql.Tx, trackID int64, actors []int64) error {
	if _, err := tx.Exec("DELETE FROM track_artist WHERE track_id=? AND role='vocal' AND source=?", trackID, CVCreditSource); err != nil {
		return err
	}
	origins, err := readOriginsTx(tx, "track", trackID)
	if err != nil {
		return err
	}
	origin := origins["roles.vocal"]
	if origin.Mode != "" && origin.Source != CVCreditSource {
		return nil
	}
	var explicit int
	if err := tx.QueryRow("SELECT COUNT(*) FROM track_artist WHERE track_id=? AND role='vocal'", trackID).Scan(&explicit); err != nil {
		return err
	}
	delete(origins, "roles.vocal")
	if explicit == 0 && len(actors) > 0 {
		seen := map[int64]bool{}
		for _, artistID := range actors {
			if seen[artistID] {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO track_artist(track_id,artist_id,role,position,source)
				VALUES(?,?,'vocal',?,?)`, trackID, artistID, len(seen), CVCreditSource); err != nil {
				return err
			}
			seen[artistID] = true
		}
		origins["roles.vocal"] = FieldOrigin{Mode: domain.Follow, Source: CVCreditSource}
	}
	body, err := json.Marshal(origins)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE track_current SET states_json=? WHERE track_id=?", string(body), trackID)
	return err
}
