package matching

// jaroWinkler 返回两个字符串的 Jaro-Winkler 相似度（0..1）。
//
// 自实现而非取库：基准测试样例已固定期望行为，两种做法都要通过同一组样例。
func jaroWinkler(s1, s2 string) float64 {
	r1, r2 := []rune(s1), []rune(s2)
	j := jaro(r1, r2)
	if j == 0 {
		return 0
	}
	// Winkler 修正：共同前缀（最多 4）给予加权。
	prefix := 0
	for prefix < len(r1) && prefix < len(r2) && prefix < 4 && r1[prefix] == r2[prefix] {
		prefix++
	}
	const p = 0.1
	return j + float64(prefix)*p*(1-j)
}

// jaro 返回 Jaro 相似度。
func jaro(r1, r2 []rune) float64 {
	l1, l2 := len(r1), len(r2)
	if l1 == 0 && l2 == 0 {
		return 1
	}
	if l1 == 0 || l2 == 0 {
		return 0
	}

	matchDist := max(l1, l2)/2 - 1
	if matchDist < 0 {
		matchDist = 0
	}

	m1 := make([]bool, l1)
	m2 := make([]bool, l2)
	matches := 0
	for i := 0; i < l1; i++ {
		lo := max(0, i-matchDist)
		hi := min(l2-1, i+matchDist)
		for j := lo; j <= hi; j++ {
			if m2[j] || r1[i] != r2[j] {
				continue
			}
			m1[i], m2[j] = true, true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}

	// 换位数
	transpositions := 0
	k := 0
	for i := 0; i < l1; i++ {
		if !m1[i] {
			continue
		}
		for !m2[k] {
			k++
		}
		if r1[i] != r2[k] {
			transpositions++
		}
		k++
	}
	t := float64(transpositions) / 2

	mf := float64(matches)
	return (mf/float64(l1) + mf/float64(l2) + (mf-t)/mf) / 3
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
