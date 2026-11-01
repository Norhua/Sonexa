package matching

import (
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

// ExistingCandidateFacts projects already verified source facts without I/O.
// The adapter alone validates its ID codec and track/release relationship;
// this helper only implements the common capability/position result shape.
func ExistingCandidateFacts(c provider.Candidate, identity CandidateIdentity, request DecisionRequest, snapshot *domain.Snapshot) TargetResolution {
	out := TargetResolution{Completeness: domain.Unloaded}
	switch identity.State {
	case IdentityInvalid:
		out.State, out.Reason = TargetInvalid, identity.Reason
		return out
	case IdentityUnresolved, IdentityUnsupported:
		out.State, out.Reason = TargetUnsupported, "exact_identity_resolution_unavailable"
		return out
	case IdentityExact:
	default:
		out.State, out.Reason = TargetInvalid, "unknown_identity_state"
		return out
	}
	out.Candidates = []provider.Candidate{c}
	if snapshot != nil {
		out.Release = snapshot.Release
	}
	for _, axis := range []struct {
		input  NumberInput
		actual *int
	}{
		{request.Position.Disc, c.DiscNumber}, {request.Position.Track, c.TrackNumber},
	} {
		if axis.input.State == "" || axis.input.State == ValueMissing {
			continue
		}
		if axis.input.State != ValueKnown || axis.input.Value <= 0 {
			out.State, out.Reason = TargetInsufficient, "invalid_input_position"
			return out
		}
		if axis.actual == nil || *axis.actual <= 0 {
			out.State, out.Reason = TargetInsufficient, "position_unavailable"
			return out
		}
		if int64(*axis.actual) != axis.input.Value {
			out.State, out.Reason = TargetInvalid, "position_conflict"
			return out
		}
	}
	if request.NeedTracklist {
		out.State, out.Reason = TargetUnsupported, "decision_tracklist_unavailable"
		return out
	}
	if request.NeedSummary && (c.ReleaseTrackTotal == nil || *c.ReleaseTrackTotal <= 0 || c.AlbumTitle == nil || *c.AlbumTitle == "") {
		out.State, out.Reason = TargetInsufficient, "summary_unavailable"
		return out
	}
	out.State = TargetExact
	return out
}
