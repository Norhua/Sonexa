package store

import (
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// matchingCandidate 是传给优选阶梯的候选类型别名。
type matchingCandidate = provider.Candidate

// toLadderCandidate 把一条 release 记录转为优选阶梯的输入。
//
// 这样专辑视图选「代表 release」与匹配阶段的多候选优选走同一套阶梯，
// 不另写一份排序代码。
func toLadderCandidate(r Release) matchingCandidate {
	ref := ""
	if r.ProviderRef != nil {
		ref = *r.ProviderRef
	}
	return provider.Candidate{
		MatchRef:         ref,
		ReleaseRef:       r.ProviderRef,
		ReleaseGroupRef:  r.ReleaseGroupRef,
		ReleaseStatus:    r.ReleaseStatus,
		MediumFormat:     r.MediumFormat,
		Country:          r.Country,
		FirstReleaseDate: r.FirstReleaseDate,
		HasCover:         r.CoverPath != nil && *r.CoverPath != "",
	}
}

// ladderConfig 是选代表 release 用的阶梯配置。
//
// 由 SetLadderConfig 注入用户设置；未注入时用默认值。匹配阶段与专辑视图
// 必须共用同一份配置——函数共用了而配置各走一套，漂移照样发生。
func (s *Store) ladderConfig() matching.LadderConfig {
	if s.ladder != nil {
		return *s.ladder
	}
	return matching.DefaultLadderConfig()
}

// SetLadderConfig 注入发行优选阶梯的配置。
func (s *Store) SetLadderConfig(cfg matching.LadderConfig) { s.ladder = &cfg }

// RepresentativeRelease 在一个发行组下选出代表 release。
//
// 专辑视图选「代表版本」与匹配阶段的多候选优选走同一套阶梯。
func (s *Store) RepresentativeRelease(groupID int64) (*Release, error) {
	rows, err := s.db.Query(
		"SELECT "+releaseColumns+" FROM release_read r WHERE r.release_group_id = ?", groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var releases []Release
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		releases = append(releases, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, nil
	}
	// 「有封面」指显式采用：旧版记在 cover_path，新版记在采用关系表。
	adopted := map[int64]bool{}
	selected, err := s.db.Query(`SELECT s.release_id FROM release_cover_selection s
		JOIN release_read r ON r.id = s.release_id WHERE r.release_group_id = ? AND s.image_id IS NOT NULL`, groupID)
	if err != nil {
		return nil, err
	}
	defer selected.Close()
	for selected.Next() {
		var id int64
		if err := selected.Scan(&id); err != nil {
			return nil, err
		}
		adopted[id] = true
	}
	if err := selected.Err(); err != nil {
		return nil, err
	}

	candidates := make([]matchingCandidate, 0, len(releases))
	for _, r := range releases {
		c := toLadderCandidate(r)
		c.HasCover = c.HasCover || adopted[r.ID]
		candidates = append(candidates, c)
	}
	best := s.ladderConfig().SelectRelease(candidates)
	if best == nil {
		return &releases[0], nil
	}
	for i := range releases {
		if releases[i].ProviderRef != nil && *releases[i].ProviderRef == best.MatchRef {
			return &releases[i], nil
		}
	}
	return &releases[0], nil
}
