package matching

import (
	"regexp"
	"strings"
)

// BracketPair 是一种括号形态的左右字符。
//
// 需覆盖的形态：（） () ［］ [] 【】
type BracketPair struct {
	Open  rune
	Close rune
}

// bracketPairs 是全部被识别的括号形态。
var bracketPairs = []BracketPair{
	{'（', '）'}, // 全角圆括号
	{'(', ')'}, // 半角圆括号
	{'［', '］'}, // 全角方括号
	{'[', ']'}, // 半角方括号
	{'【', '】'}, // 黑括号
}

// isBracketRune 报告 r 是否为任一被识别形态的括号字符。
func isBracketRune(r rune) bool {
	for _, p := range bracketPairs {
		if r == p.Open || r == p.Close {
			return true
		}
	}
	return false
}

// Segment 是一段被括号包裹的内容及其在原串中的位置。
type Segment struct {
	Outer     string // 括号之外的部分（去掉本段括号后的剩余串）
	Inner     string // 括号内的内容，不含括号本身
	Start     int    // 左括号在原串中的字节偏移
	End       int    // 右括号之后的字节偏移
	OpenRune  rune
	CloseRune rune
}

// FindBracketSegments 找出串中全部最外层的括号段。
//
// 只取最外层：嵌套括号的内层留在 Inner 里由调用方按需再解析，
// 这样「取内层」变体不会因为嵌套而丢内容。
func FindBracketSegments(s string) []Segment {
	var segments []Segment
	runes := []rune(s)
	// 记录每个 rune 的字节偏移，便于返回字节位置（credit_spans_json 用字节偏移）
	offsets := make([]int, len(runes)+1)
	{
		off := 0
		for i, r := range runes {
			offsets[i] = off
			off += len(string(r))
		}
		offsets[len(runes)] = off
	}

	for i := 0; i < len(runes); i++ {
		pair, ok := openingPair(runes[i])
		if !ok {
			continue
		}
		depth := 0
		for j := i; j < len(runes); j++ {
			switch runes[j] {
			case pair.Open:
				depth++
			case pair.Close:
				depth--
				if depth == 0 {
					inner := string(runes[i+1 : j])
					outer := strings.TrimSpace(string(runes[:i]) + string(runes[j+1:]))
					segments = append(segments, Segment{
						Outer:     outer,
						Inner:     inner,
						Start:     offsets[i],
						End:       offsets[j+1],
						OpenRune:  pair.Open,
						CloseRune: pair.Close,
					})
					i = j // 跳过本段，继续找下一个最外层段
				}
			}
			if depth == 0 {
				break
			}
		}
	}
	return segments
}

// openingPair 返回以 r 为左括号的形态。
func openingPair(r rune) (BracketPair, bool) {
	for _, p := range bracketPairs {
		if r == p.Open {
			return p, true
		}
	}
	return BracketPair{}, false
}

// separatorPattern 是括号内前缀与内容之间的分隔符：半角冒号、全角冒号、
// 半角句点、全角句号。
const separatorPattern = `[:：.。]`

// StripBrackets 去掉串中全部最外层括号段，返回剩余部分。
func StripBrackets(s string) string {
	segments := FindBracketSegments(s)
	if len(segments) == 0 {
		return s
	}
	// 从后往前删，避免前面的删除改变后面段的偏移
	out := s
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		out = out[:seg.Start] + out[seg.End:]
	}
	return strings.TrimSpace(collapseSpaces(out))
}

var multiSpace = regexp.MustCompile(`\s+`)

func collapseSpaces(s string) string {
	return strings.TrimSpace(multiSpace.ReplaceAllString(s, " "))
}
