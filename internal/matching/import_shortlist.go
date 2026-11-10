package matching

// ImportTitleKeys is a conservative shortlist for exact import evidence, NOT a
// matching decision. It covers every exact branch of CompareTitle; callers must
// still check versions, full credits, album, position, ambiguity and confirmation
// validity. In particular shared alternates alone do not establish identity.
func ImportTitleKeys(text string) []string {
	values := append([]string{text, StripNonVersionAnnotations(text)}, Alternates(text)...)
	seen := map[string]bool{}
	var keys []string
	for _, value := range values {
		key := Normalize(value)
		if key != "" && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}
