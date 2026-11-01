package matching

import (
	"context"

	"github.com/norhua/sonexa/internal/domain"
)

// LocalRecall is a fact source, not an ownership or automatic-selection decision.
// Results correspond to inputs in order. Truncated must block any uniqueness
// inference; even an untruncated local result does not enumerate upstream releases.
type LocalRecall interface {
	RecallLocal(context.Context, []InputProfile, int) ([]LocalRecallResult, error)
}

type LocalRecallResult struct {
	Records   []LocalRecord
	Truncated bool
}

// LocalRecord keeps current values and independently validated bound snapshots
// separate. Consumers must compare each provenance, never splice conflicting
// facts into a fabricated upstream candidate. IDs here do not confer ownership.
type LocalRecord struct {
	TrackID, ReleaseID, GroupID                    int64
	TrackRevision, ReleaseRevision, GroupRevision  int64
	TrackSnapshotRevision, ReleaseSnapshotRevision int64
	TrackIdentity, ReleaseIdentity                 *domain.Identity
	GroupIdentities                                []domain.Identity
	Current                                        LocalCurrentFacts
	TrackSnapshot                                  *domain.TrackSnapshot
	ReleaseSnapshot                                *domain.ReleaseSnapshot
	Issues                                         []DecisionReason
}

type LocalCurrentFacts struct {
	Profile       InputProfile
	TrackCredit   domain.Credit
	ReleaseCredit domain.Credit
	Roles         []domain.Role
	ArtistLinks   []LocalArtistLink
	Origins       map[string]LocalFieldOrigin
}

type LocalFieldOrigin struct {
	Mode   domain.ValueMode `json:"mode"`
	Source string           `json:"source"`
}

type LocalArtistLink struct {
	From, To domain.Artist
	Kind     string
}

// RecallTextForms deliberately includes uncertain alternatives for recall only.
// The full profile, including qualifiers and unknown annotations, remains the
// authority for later evidence checks. No stripped key is an equivalence proof.
func RecallTextForms(p TextProfile) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(text string) {
		key := Normalize(text)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	add(p.Raw)
	add(p.Body)
	for _, alias := range p.Aliases {
		add(alias.Text)
	}
	return out
}
