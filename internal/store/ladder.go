package store

import "github.com/norhua/sonexa/internal/provider"

// matchingCandidate 是传给优选阶梯的候选类型别名。
type matchingCandidate = provider.Candidate

// toLadderCandidate 把一条 release 记录转为优选阶梯的输入。
//
// 这样专辑视图选「代表 release」与匹配阶段的多候选优选走同一套阶梯，
// 不另写一份排序代码。
func toLadderCandidate(r Release) matchingCandidate {
	return provider.Candidate{
		MatchRef:         r.ProviderRef,
		ReleaseRef:       &r.ProviderRef,
		ReleaseGroupRef:  r.ReleaseGroupRef,
		ReleaseStatus:    r.ReleaseStatus,
		MediumFormat:     r.MediumFormat,
		Country:          r.Country,
		FirstReleaseDate: r.FirstReleaseDate,
		HasCover:         r.CoverPath != nil && *r.CoverPath != "",
	}
}
