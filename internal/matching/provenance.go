package matching

import (
	"errors"
	"fmt"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

type OwnershipState string

const (
	OwnershipUnowned  OwnershipState = "unowned"
	OwnershipOwned    OwnershipState = "owned"
	OwnershipConflict OwnershipState = "identity_conflict"
	OwnershipError    OwnershipState = "lookup_error"
)

var ErrIdentityConflict = errors.New("候选精确身份或发行关系冲突")
var ErrIdentityUnavailable = errors.New("候选身份检查能力不可用")

// LocalBinding is one current binding read atomically, never a related ISRC,
// recording, text hit or historical import decision. Revisions bind skip plans.
type LocalBinding struct {
	TrackID         int64
	Track           domain.Identity
	Release         domain.Identity
	Revision        int64
	ReleaseRevision int64
}

// LocalMatch separates exact ownership from retrieval relevance. Only owned
// exposes a local target ID. Unresolved candidates may still be displayed and
// prepared, but do not have an exact ownership conclusion until resolved.
type LocalMatch struct {
	State           OwnershipState    `json:"state"`
	Identity        CandidateIdentity `json:"identity"`
	TrackID         int64             `json:"trackId"`
	Revision        int64             `json:"revision"`
	ReleaseRevision int64             `json:"releaseRevision"`
	Reason          string            `json:"reason,omitempty"`
}

func (m LocalMatch) Badges(upstream provider.ProviderID) []string {
	if m.State == OwnershipOwned {
		return []string{"本地", string(upstream)}
	}
	return []string{string(upstream)}
}

func (m LocalMatch) Note() string {
	switch m.State {
	case OwnershipConflict:
		return "身份或发行关系冲突，请刷新候选"
	case OwnershipError:
		return "本地身份查询失败，请重试"
	default:
		return ""
	}
}

// Action is a transitional display label; ownership is the business result.
func (m LocalMatch) Action() string {
	switch m.State {
	case OwnershipOwned:
		return "跳过"
	case OwnershipConflict, OwnershipError:
		return "不可操作"
	default:
		return "接受"
	}
}

// LocalLookup is intentionally unable to query ISRC or text. Source-specific
// identity syntax is validated by the injected CandidateSources, not the store.
type LocalLookup interface {
	FindExactBinding(track domain.Identity) (*LocalBinding, error)
}

// ResolveLocal never performs network requests. Only a provider-validated exact
// track with the same current release relationship can be owned. Errors retain
// their structured state instead of silently becoming unowned.
func ResolveLocal(lookup LocalLookup, sources *CandidateSources, c provider.Candidate) (LocalMatch, error) {
	identity := sources.Inspect(c)
	out := LocalMatch{State: OwnershipUnowned, Identity: identity}
	switch identity.State {
	case IdentityUnresolved:
		out.Reason = identity.Reason
		return out, nil
	case IdentityInvalid:
		out.State, out.Reason = OwnershipConflict, identity.Reason
		return out, fmt.Errorf("%w: %s", ErrIdentityConflict, identity.Reason)
	case IdentityUnsupported:
		out.State, out.Reason = OwnershipError, identity.Reason
		return out, ErrIdentityUnavailable
	}
	binding, err := lookup.FindExactBinding(identity.Track)
	if err != nil {
		out.State, out.Reason = OwnershipError, err.Error()
		return out, err
	}
	if binding == nil {
		return out, nil
	}
	if binding.TrackID <= 0 || binding.Revision <= 0 || binding.ReleaseRevision <= 0 ||
		binding.Track != identity.Track || binding.Release != identity.Release {
		out.State, out.Reason = OwnershipConflict, "current_binding_conflict"
		return out, ErrIdentityConflict
	}
	out.State, out.TrackID = OwnershipOwned, binding.TrackID
	out.Revision, out.ReleaseRevision = binding.Revision, binding.ReleaseRevision
	return out, nil
}

// DurationWithinTolerance is retained for the legacy local-recall caller, not
// ownership. A duration or text match never grants an exact local binding.
func DurationWithinTolerance(a, b int) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= 3000
}
