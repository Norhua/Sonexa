package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ArtistRole 是参与者角色，取值域固定。
type ArtistRole string

const (
	RoleVocal       ArtistRole = "vocal"
	RoleInstrument  ArtistRole = "instrument"
	RolePerformer   ArtistRole = "performer"
	RoleComposer    ArtistRole = "composer"
	RoleLyricist    ArtistRole = "lyricist"
	RoleWriter      ArtistRole = "writer"
	RoleArranger    ArtistRole = "arranger"
	RoleProducer    ArtistRole = "producer"
	RoleRemixer     ArtistRole = "remixer"
	RoleConductor   ArtistRole = "conductor"
	RoleCharacter   ArtistRole = "character"
	RoleAlbumArtist ArtistRole = "album_artist"
)

// RoleCredited 是「署名」这一伪角色，仅用于艺术家视图的分面筛选。
//
// 它不是 track_artist 里的取值——署名存在 track_credit 表上。
// 放在这里是因为分面的取值域对前端是一个整体。
const RoleCredited = "credited"

// RoleDisplayOrder 是详情抽屉参与者区的分组显示顺序。
// character 不在其中——它不单独成组，只出现在署名原串的可点击片段中。
var RoleDisplayOrder = []ArtistRole{
	RoleVocal, RoleInstrument, RolePerformer, RoleComposer,
	RoleLyricist, RoleArranger, RoleProducer, RoleRemixer, RoleConductor,
}

// validRoles 用于拒绝取值域之外的角色值。
var validRoles = map[ArtistRole]bool{
	RoleVocal: true, RoleInstrument: true, RolePerformer: true,
	RoleComposer: true, RoleLyricist: true, RoleWriter: true,
	RoleArranger: true, RoleProducer: true, RoleRemixer: true,
	RoleConductor: true, RoleCharacter: true, RoleAlbumArtist: true,
}

// IsValidRole 报告 role 是否在取值域内。
func IsValidRole(role ArtistRole) bool { return validRoles[role] }

// ArtistType 是艺术家类型。
const (
	ArtistTypePerson    = "person"
	ArtistTypeGroup     = "group"
	ArtistTypeCharacter = "character"
	ArtistTypeOrchestra = "orchestra"
	ArtistTypeChoir     = "choir"
	ArtistTypeOther     = "other"
)

// LinkType 是艺术家间的关系类型。
const (
	LinkVoiceOf  = "voice_of"
	LinkMemberOf = "member_of"
	LinkAliasOf  = "alias_of"
)

// Artist 是艺术家实体。上游标识可空——手动录入的艺术家没有。
type Artist struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	SortName       *string `json:"sortName"`
	Type           *string `json:"type"`
	Disambiguation *string `json:"disambiguation"`
	MBID           *string `json:"mbid"`
}

// TrackArtist 是一条曲目-艺术家的角色关系。
//
// 它回答「这个人在这首歌上做了什么」。「这个人是否被署名」是另一件事，
// 存在 track_credit 上——未署名的作词作曲与署名艺术家必须可区分，
// 否则艺术家列表里会出现一堆用户从没在署名里见过的名字。
type TrackArtist struct {
	ArtistID  int64      `json:"artistID"`
	Role      ArtistRole `json:"role"`
	Attribute *string    `json:"attribute"` // role = instrument 时存乐器名
	Position  *int       `json:"position"`
	Source    *string    `json:"source"`
}

// CreditEntry 是署名串里的一段：谁、以什么写法、在原串的哪个区间。
type CreditEntry struct {
	ArtistID     int64   `json:"artistId"`
	Position     int     `json:"position"`
	CreditedName *string `json:"creditedName"`
	SpanStart    *int    `json:"spanStart"`
	SpanEnd      *int    `json:"spanEnd"`
}

// UpsertArtist 按 (name, disambiguation) 找到或创建艺术家，返回其 id。
//
// 不同写法就是不同的 artist 行：v1 不做「同一艺术家不同写法合并」，
// 因此这里不对名称做任何标准化比对。
func (s *Store) UpsertArtist(a *Artist) (int64, error) {
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		var err error
		id, err = upsertArtistTx(tx, a)
		return err
	})
	return id, err
}

func upsertArtistTx(tx *sql.Tx, a *Artist) (int64, error) {
	var id int64
	err := tx.QueryRow(
		`SELECT id FROM artist WHERE name = ? AND COALESCE(disambiguation,'') = COALESCE(?,'')`,
		a.Name, a.Disambiguation).Scan(&id)
	if err == nil {
		// 已存在：补齐此前缺失的字段，不覆盖已有值
		if _, err := tx.Exec(`
			UPDATE artist SET
				sort_name = COALESCE(sort_name, ?),
				type      = COALESCE(type, ?),
				mbid      = COALESCE(mbid, ?),
				updated_at = ?
			WHERE id = ?`, a.SortName, a.Type, a.MBID, time.Now().UTC(), id); err != nil {
			return 0, err
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	now := time.Now().UTC()
	res, err := tx.Exec(`
		INSERT INTO artist (name, sort_name, type, disambiguation, mbid, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`,
		a.Name, a.SortName, a.Type, a.Disambiguation, a.MBID, now, now)
	if err != nil {
		return 0, fmt.Errorf("创建艺术家失败: %w", err)
	}
	return res.LastInsertId()
}

// LinkArtists 建立一条艺术家关系（如角色 → 声优的 voice_of）。
func (s *Store) LinkArtists(fromID, toID int64, linkType string) error {
	return s.withTx(func(tx *sql.Tx) error { return linkArtistsTx(tx, fromID, toID, linkType) })
}

func linkArtistsTx(tx *sql.Tx, fromID, toID int64, linkType string) error {
	_, err := tx.Exec(`
		INSERT INTO artist_link (from_artist_id, to_artist_id, type) VALUES (?,?,?)
		ON CONFLICT DO NOTHING`, fromID, toID, linkType)
	if err != nil {
		return fmt.Errorf("建立艺术家关系失败: %w", err)
	}
	return nil
}

// SetTrackArtists 以整组关系替换某曲目的参与者，并在同一事务内刷新 FTS 索引。
//
// 索引刷新与关系写入同事务是硬要求：否则改了关系却没刷索引时，
// 搜声优名搜不到，而这种漂移很难被发现。
func (s *Store) SetTrackArtists(trackID int64, relations []TrackArtist) error {
	for _, r := range relations {
		if !IsValidRole(r.Role) {
			return fmt.Errorf("角色 %q 不在取值域内", r.Role)
		}
	}
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM track_artist WHERE track_id = ?`, trackID); err != nil {
			return err
		}
		for _, r := range relations {
			if _, err := tx.Exec(`
				INSERT INTO track_artist (track_id, artist_id, role, attribute, position, source)
				VALUES (?,?,?,?,?,?)
				ON CONFLICT(track_id, artist_id, role) DO UPDATE SET
					attribute = excluded.attribute,
					position  = excluded.position,
					source    = excluded.source`,
				trackID, r.ArtistID, string(r.Role), r.Attribute, r.Position, r.Source); err != nil {
				return fmt.Errorf("写入参与者关系失败: %w", err)
			}
		}
		return syncFTS(tx, trackID)
	})
}

// AddTrackArtist 追加一条参与者关系并刷新索引。
func (s *Store) AddTrackArtist(trackID int64, r TrackArtist) error {
	if !IsValidRole(r.Role) {
		return fmt.Errorf("角色 %q 不在取值域内", r.Role)
	}
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			INSERT INTO track_artist (track_id, artist_id, role, attribute, position, source)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT(track_id, artist_id, role) DO UPDATE SET
				attribute = excluded.attribute,
				position  = excluded.position,
				source    = excluded.source`,
			trackID, r.ArtistID, string(r.Role), r.Attribute, r.Position, r.Source); err != nil {
			return err
		}
		return syncFTS(tx, trackID)
	})
}

// SetTrackCredits 以整组署名替换某曲目的署名分解。
//
// 这张表同时承担两件事：渲染署名原串里的可点击区间，以及回答
// 「这位艺术家是否被署名」——后者是艺术家列表的成员判定依据，
// 因此它必须可查询，不能像此前那样躺在一个 JSON 列里。
func (s *Store) SetTrackCredits(trackID int64, entries []CreditEntry) error {
	return s.withTx(func(tx *sql.Tx) error { return setTrackCreditsTx(tx, trackID, entries) })
}

func setTrackCreditsTx(tx *sql.Tx, trackID int64, entries []CreditEntry) error {
	if _, err := tx.Exec(`DELETE FROM track_credit WHERE track_id = ?`, trackID); err != nil {
		return err
	}
	for i, e := range entries {
		pos := e.Position
		if pos == 0 {
			pos = i
		}
		if _, err := tx.Exec(`
			INSERT INTO track_credit (track_id, artist_id, position, credited_name, span_start, span_end)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT(track_id, artist_id, position) DO UPDATE SET
				credited_name = excluded.credited_name,
				span_start    = excluded.span_start,
				span_end      = excluded.span_end`,
			trackID, e.ArtistID, pos, e.CreditedName, e.SpanStart, e.SpanEnd); err != nil {
			return fmt.Errorf("写入署名分解失败: %w", err)
		}
	}
	return syncFTS(tx, trackID)
}

// SetReleaseCredits 以整组署名替换某歌集的署名分解。
func (s *Store) SetReleaseCredits(releaseID int64, entries []CreditEntry) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM release_credit WHERE release_id = ?`, releaseID); err != nil {
			return err
		}
		for i, e := range entries {
			pos := e.Position
			if pos == 0 {
				pos = i
			}
			if _, err := tx.Exec(`
				INSERT INTO release_credit (release_id, artist_id, position, credited_name, span_start, span_end)
				VALUES (?,?,?,?,?,?)
				ON CONFLICT(release_id, artist_id, position) DO UPDATE SET
					credited_name = excluded.credited_name,
					span_start    = excluded.span_start,
					span_end      = excluded.span_end`,
				releaseID, e.ArtistID, pos, e.CreditedName, e.SpanStart, e.SpanEnd); err != nil {
				return fmt.Errorf("写入歌集署名分解失败: %w", err)
			}
		}
		return nil
	})
}

// GetTrackCredits 读取某曲目的署名分解，按位置升序。
func (s *Store) GetTrackCredits(trackID int64) ([]CreditEntry, error) {
	rows, err := s.db.Query(`
		SELECT artist_id, position, credited_name, span_start, span_end
		FROM track_credit WHERE track_id = ? ORDER BY position`, trackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CreditEntry{}
	for rows.Next() {
		var e CreditEntry
		if err := rows.Scan(&e.ArtistID, &e.Position, &e.CreditedName, &e.SpanStart, &e.SpanEnd); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetArtist 按 id 读取艺术家。
func (s *Store) GetArtist(id int64) (*Artist, error) {
	var a Artist
	err := s.db.QueryRow(
		`SELECT id, name, sort_name, type, disambiguation, mbid FROM artist WHERE id = ?`, id).Scan(
		&a.ID, &a.Name, &a.SortName, &a.Type, &a.Disambiguation, &a.MBID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// VoiceActorOf 返回某角色艺术家的声优；无此关系时返回 nil。
// 供艺术家视图把标题渲染为「涼宮ハルヒ (CV: 平野綾)」。
func (s *Store) VoiceActorOf(characterID int64) (*Artist, error) {
	var a Artist
	err := s.db.QueryRow(`
		SELECT a.id, a.name, a.sort_name, a.type, a.disambiguation, a.mbid
		FROM artist_link l JOIN artist a ON a.id = l.to_artist_id
		WHERE l.from_artist_id = ? AND l.type = ?`, characterID, LinkVoiceOf).Scan(
		&a.ID, &a.Name, &a.SortName, &a.Type, &a.Disambiguation, &a.MBID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
