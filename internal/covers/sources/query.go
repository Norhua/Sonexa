package sources

import "strings"

// SearchTerm is the cover business query: song first, album as fallback, artist
// as an anchor. It is intentionally different from metadata and lyric queries.
func SearchTerm(q Query) string {
	head := strings.TrimSpace(q.Title)
	if head == "" {
		head = strings.TrimSpace(q.Album)
	}
	return strings.TrimSpace(strings.Join([]string{head, strings.TrimSpace(q.Artist)}, " "))
}
