package matching

import "github.com/norhua/sonexa/internal/domain"

// PositionUnique inspects every possible position, not only collected tracks.
// It is shared by automatic selection and active-problem identity proofs.
func PositionUnique(input InputProfile, target CandidateTarget, release *domain.ReleaseSnapshot) bool {
	if release.TracklistCompleteness != domain.Complete || release.Tracklist.State != domain.Value || len(release.Tracklist.Value) == 0 {
		return false
	}
	found := false
	positions := map[[2]int]bool{}
	identities := map[domain.Identity]bool{}
	for _, entry := range release.Tracklist.Value {
		if entry.Track.State != domain.Value || entry.Track.Value.Provider != release.Identity.Provider || entry.Track.Value.Ref == "" || entry.DiscNumber <= 0 || entry.TrackNumber <= 0 {
			return false
		}
		position := [2]int{entry.DiscNumber, entry.TrackNumber}
		if positions[position] || identities[entry.Track.Value] {
			return false
		}
		positions[position], identities[entry.Track.Value] = true, true
		if entry.Track.Value == target.Identity.Track {
			if found {
				return false
			}
			found = true
			continue
		}
		track := &domain.TrackSnapshot{Identity: entry.Track.Value, Release: &release.Identity, Title: entry.Title, Credit: entry.Credit,
			DiscNumber: domain.Known(entry.DiscNumber), TrackNumber: domain.Known(entry.TrackNumber), DurationMS: entry.DurationMS, Recording: entry.Recording}
		candidate := SnapshotCandidate(track, release)
		// Album correspondence belongs to this exact release, possibly through
		// its current user name. Its old name must not exclude sibling tracks.
		positionInput := input
		positionInput.Album = ParseMetadataText("", MetadataAlbum)
		sameRecording := false
		for _, fact := range target.Facts {
			ref := fact.Metadata.Candidate.RecordingRef
			if ref != nil && *ref != "" && candidate.RecordingRef != nil && *ref == *candidate.RecordingRef {
				sameRecording = true
			}
		}
		if sameRecording {
			if input.Disc.State == ValueKnown && input.Disc.Value != int64(entry.DiscNumber) ||
				input.Track.State == ValueKnown && input.Track.Value != int64(entry.TrackNumber) {
				continue
			}
			return false
		}
		evidence := CheckMetadataEvidence(positionInput, MetadataFacts{Candidate: candidate, Source: "competing_position", AlbumComplete: release.Title.State == domain.Value, PositionVerified: true})
		if evidence.Eligibility != MetadataExcluded {
			return false
		}
	}
	return found
}
