package matching

import "github.com/norhua/sonexa/internal/provider"

// LocalMatchKind describes ownership of the exact upstream track, not recording.
type LocalMatchKind string

const (
	LocalNone  LocalMatchKind = "none"
	LocalByRef LocalMatchKind = "by_ref"
)

type LocalMatch struct {
	Kind    LocalMatchKind
	TrackID int64
}

func (m LocalMatch) Badges(upstream provider.ProviderID) []string {
	if m.Kind == LocalByRef {
		return []string{"本地", string(upstream)}
	}
	return []string{string(upstream)}
}
func (m LocalMatch) Note() string { return "" }
func (m LocalMatch) Action() string {
	if m.Kind == LocalByRef {
		return "跳过"
	}
	return "加入曲库"
}

// LocalLookup is the consumer-owned exact track binding lookup. ISRC can be
// used for recording search, but never proves ownership of a release track.
type LocalLookup interface {
	FindByMatchRef(source, ref string) (int64, error)
}

func ResolveLocal(lookup LocalLookup, c provider.Candidate) (LocalMatch, error) {
	if lookup != nil && c.Provider != "" && c.MatchRef != "" {
		id, err := lookup.FindByMatchRef(string(c.Provider), c.MatchRef)
		if err != nil {
			return LocalMatch{}, err
		}
		if id != 0 {
			return LocalMatch{Kind: LocalByRef, TrackID: id}, nil
		}
	}
	return LocalMatch{Kind: LocalNone}, nil
}

func DurationWithinTolerance(a, b int) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= 3000
}
