package musicbrainz

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

var _ matching.CandidateResolver = (*Provider)(nil)

// InspectCandidate checks source-native identity without I/O. A synthetic
// recording@release is a search seed, never a collection track identity.
func (p *Provider) InspectCandidate(c provider.Candidate) matching.CandidateIdentity {
	invalid := matching.CandidateIdentity{State: matching.IdentityInvalid, Reason: "invalid_musicbrainz_identity"}
	if c.Provider != provider.MusicBrainz || c.ReleaseRef == nil || !canonicalUUID(*c.ReleaseRef) {
		return invalid
	}
	releaseID := domain.Identity{Provider: provider.MusicBrainz, Ref: *c.ReleaseRef}
	if recordingID, releaseIDText, synthetic := strings.Cut(c.MatchRef, "@"); synthetic {
		if !canonicalUUID(recordingID) || releaseIDText != releaseID.Ref || c.RecordingRef == nil || *c.RecordingRef != recordingID {
			return invalid
		}
		return matching.CandidateIdentity{State: matching.IdentityUnresolved, Release: releaseID, Reason: "recording_requires_track_position"}
	}
	if !canonicalUUID(c.MatchRef) {
		return invalid
	}
	if (c.DiscNumber != nil && *c.DiscNumber <= 0) || (c.TrackNumber != nil && *c.TrackNumber <= 0) {
		return invalid
	}
	return matching.CandidateIdentity{State: matching.IdentityExact,
		Track: domain.Identity{Provider: provider.MusicBrainz, Ref: c.MatchRef}, Release: releaseID}
}

func canonicalUUID(ref string) bool { return uuidRef.MatchString(ref) && strings.ToLower(ref) == ref }

func (p *Provider) LoadDecisionFacts(ctx context.Context, request matching.DecisionRequest) ([]matching.TargetResolution, error) {
	out := make([]matching.TargetResolution, 0, len(request.Candidates))
	// Scoped to this bounded request: both summary and tracklist consumers use
	// the same parsed release, while later requests reuse the HTTP cache.
	releases := map[string]*releaseDetail{}
	for _, candidate := range request.Candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		identity := p.InspectCandidate(candidate)
		if identity.State == matching.IdentityInvalid {
			out = append(out, matching.TargetResolution{State: matching.TargetInvalid, Completeness: domain.Unloaded, Reason: identity.Reason})
			continue
		}
		if identity.State == matching.IdentityExact && !request.NeedTracklist && !request.NeedSummary &&
			!positionRequested(request.Position) {
			out = append(out, matching.TargetResolution{State: matching.TargetExact, Candidates: []provider.Candidate{candidate}, Completeness: domain.Unloaded})
			continue
		}
		rel := releases[identity.Release.Ref]
		if rel == nil {
			loaded, err := p.fetchRelease(ctx, provider.Ref{Provider: provider.MusicBrainz, ID: identity.Release.Ref})
			if err != nil {
				return nil, err
			}
			if loaded.ID != identity.Release.Ref {
				return nil, fmt.Errorf("MusicBrainz 资料不属于目标发行")
			}
			rel = loaded
			releases[identity.Release.Ref] = rel
		}
		out = append(out, p.resolveCandidate(candidate, request.Position, rel))
	}
	return out, nil
}

func positionRequested(p matching.PositionRequirement) bool {
	return (p.Disc.State != "" && p.Disc.State != matching.ValueMissing) || (p.Track.State != "" && p.Track.State != matching.ValueMissing)
}

// resolveCandidate consumes native facts so missing media positions are never
// replaced by the legacy display fallback to disc 1.
func (p *Provider) resolveCandidate(seed provider.Candidate, position matching.PositionRequirement, rel *releaseDetail) matching.TargetResolution {
	out := matching.TargetResolution{State: matching.TargetInsufficient, Completeness: domain.Partial, Reason: "incomplete_tracklist"}
	identity := p.InspectCandidate(seed)
	if identity.State == matching.IdentityInvalid || rel == nil || identity.Release.Ref != rel.ID {
		out.State, out.Reason = matching.TargetInvalid, "target_release_conflict"
		return out
	}
	complete := completeDecisionTracklist(rel)
	if complete {
		out.Completeness = domain.Complete
	}
	detail := ParseDetail(rel, "")
	detail.TracklistCompleteness = out.Completeness
	// Keep missing positions unknown in the decision snapshot as well.
	index := 0
	for _, med := range rel.Media {
		for _, trk := range med.TrackList() {
			detail.Tracklist[index].DiscNumber = med.Position
			detail.Tracklist[index].TrackNumber = decisionTrackPosition(trk)
			index++
		}
	}
	out.Release = provider.DetailSnapshot(detail).Release
	for _, requirement := range []matching.NumberInput{position.Disc, position.Track} {
		if requirement.State != "" && requirement.State != matching.ValueMissing &&
			(requirement.State != matching.ValueKnown || requirement.Value <= 0) {
			out.Reason = "invalid_input_position"
			return out
		}
	}
	unknownPosition := false
	for _, med := range rel.Media {
		for _, trk := range med.TrackList() {
			if identity.State == matching.IdentityExact {
				if trk.ID != seed.MatchRef {
					continue
				}
			} else if trk.Recording == nil || seed.RecordingRef == nil || trk.Recording.ID != *seed.RecordingRef {
				continue
			}
			pos := decisionTrackPosition(trk)
			if (position.Disc.State == matching.ValueKnown && med.Position > 0 && int64(med.Position) != position.Disc.Value) ||
				(position.Track.State == matching.ValueKnown && pos > 0 && int64(pos) != position.Track.Value) {
				continue
			}
			if med.Position <= 0 || pos <= 0 || !canonicalUUID(trk.ID) {
				unknownPosition = true
				continue
			}
			if seed.RecordingRef != nil && (trk.Recording == nil || trk.Recording.ID != *seed.RecordingRef) {
				out.State, out.Reason = matching.TargetInvalid, "recording_relation_conflict"
				return out
			}
			rec := recording{}
			if trk.Recording != nil {
				rec = *trk.Recording
			}
			// Build from the fetched source facts, never from input/query text.
			resolved := buildCandidate(rec, *rel, med, trk, joinArtistCredit(rec.ArtistCredit))
			out.Candidates = append(out.Candidates, resolved)
		}
	}
	if !complete || unknownPosition {
		return out
	}
	switch len(out.Candidates) {
	case 0:
		out.State, out.Reason = matching.TargetNotFound, "track_not_found"
	case 1:
		out.State, out.Reason = matching.TargetExact, "unique_track_position"
	default:
		out.State, out.Reason = matching.TargetAmbiguous, "multiple_track_positions"
	}
	return out
}

func decisionTrackPosition(t track) int {
	if t.Position > 0 {
		return t.Position
	}
	n, err := strconv.Atoi(t.Number)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func completeDecisionTracklist(rel *releaseDetail) bool {
	if len(rel.Media) == 0 || (rel.MediumCount > 0 && rel.MediumCount != len(rel.Media)) {
		return false
	}
	seenMedia, seenTracks := map[int]bool{}, map[string]bool{}
	total := 0
	for _, med := range rel.Media {
		if med.Position <= 0 || seenMedia[med.Position] || med.TrackCount <= 0 || med.TrackCount != len(med.TrackList()) {
			return false
		}
		seenMedia[med.Position] = true
		positions := map[int]bool{}
		for _, trk := range med.TrackList() {
			pos := decisionTrackPosition(trk)
			if !canonicalUUID(trk.ID) || seenTracks[trk.ID] || pos <= 0 || pos > med.TrackCount || positions[pos] ||
				trk.Recording == nil || !canonicalUUID(trk.Recording.ID) {
				return false
			}
			seenTracks[trk.ID], positions[pos] = true, true
			total++
		}
	}
	for n := 1; n <= len(rel.Media); n++ {
		if !seenMedia[n] {
			return false
		}
	}
	return rel.TrackCount == nil || *rel.TrackCount == total
}
