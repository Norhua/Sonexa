package sources

import (
	"strings"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// 上游搜索对词项做宽松召回（如 iTunes 仅凭「AKB48」召回同团体全部单曲），
// 封面结果必须再按查询的歌曲、艺术家、专辑过滤，否则会把无关专辑当候选。

// looseEqual 在标准化并去括号后按包含关系比较，容忍「(劇場盤)」「- Single」等版本后缀。
func looseEqual(query, candidate string) bool {
	for _, pair := range [][2]string{
		{matching.Normalize(matching.StripBrackets(query)), matching.Normalize(matching.StripBrackets(candidate))},
		{matching.Normalize(query), matching.Normalize(candidate)},
	} {
		a, b := pair[0], pair[1]
		if a == "" || b == "" {
			continue
		}
		// 过短的串（如单字）做包含比较几乎匹配一切，只接受完全相等。
		if a == b || (min(len([]rune(a)), len([]rune(b))) >= 2 && (strings.Contains(a, b) || strings.Contains(b, a))) {
			return true
		}
	}
	return false
}

func artistMatches(query string, candidates ...string) bool {
	for _, c := range candidates {
		if looseEqual(query, c) {
			return true
		}
		for _, part := range strings.FieldsFunc(c, func(r rune) bool { return r == '/' || r == '、' || r == ',' || r == '&' || r == ';' }) {
			if looseEqual(query, strings.TrimSpace(part)) {
				return true
			}
		}
	}
	return false
}

// Relevant 判断一条歌曲级结果是否属于查询所指的专辑。
//
// 有专辑名时，专辑名匹配即可；否则要求歌曲名匹配，且艺术家（若给出）也匹配。
// 专辑名不匹配但歌曲与艺术家都匹配的结果同样保留：同一首歌可能收录在改名的版本里。
func Relevant(q provider.Query, title, artist, album string) bool {
	q.Title, q.Artist, q.Album = strings.TrimSpace(q.Title), strings.TrimSpace(q.Artist), strings.TrimSpace(q.Album)
	if q.Title == "" && q.Album == "" {
		return q.Artist == "" || artistMatches(q.Artist, artist)
	}
	if q.Album != "" && looseEqual(q.Album, album) {
		return true
	}
	if q.Title == "" || !looseEqual(q.Title, title) {
		return false
	}
	return q.Artist == "" || artistMatches(q.Artist, artist)
}
