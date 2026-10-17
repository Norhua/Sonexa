package sources

const MaxResults = 20

// Info belongs to the cover source list, not the metadata registry.
type Info struct {
	ID           string
	Name         string
	NeedsCountry bool
}

func BoundedLimit(limit int) int {
	if limit <= 0 || limit > MaxResults {
		return MaxResults
	}
	return limit
}
