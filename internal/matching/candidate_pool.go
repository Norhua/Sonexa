package matching

import (
	"fmt"
	"strconv"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

// CandidatePool groups exact targets, not recordings or similar text. Related
// unbound records remain visible evidence and cannot become owned candidates.
type CandidatePool struct {
	Targets   []CandidateTarget
	Related   []LocalRecord
	Truncated bool
}

type CandidateTarget struct {
	Identity  CandidateIdentity
	Facts     []CandidateFact
	Ownership LocalMatch
	Conflicts []FactConflict
}

// CandidateFact is one coherent view; it is never completed with another
// view's title/credit/position. Current user values are explicitly distinguished
// from upstream facts, even when their strings happen to match.
type CandidateFact struct {
	Kind            string
	LocalTrackID    int64
	TrackRevision   int64
	ReleaseRevision int64
	Metadata        MetadataFacts
	Current         *LocalCurrentFacts
	Track           *domain.TrackSnapshot
	Release         *domain.ReleaseSnapshot
}

type FactConflict struct {
	Field, LeftSource, RightSource, LeftValue, RightValue string
}

type TargetEvidence struct {
	Eligibility MetadataEligibility
	Facets      []MetadataEvidence
	Reasons     []DecisionReason
}

// BuildCandidatePool does only local reads and pure comparison. Each candidate
// is inspected by its source; matching never interprets source-native IDs.
func BuildCandidatePool(local LocalRecallResult, remote []provider.Candidate, sources *CandidateSources, lookup LocalLookup) CandidatePool {
	out := CandidatePool{Truncated: local.Truncated}
	indices := map[domain.Identity]int{}
	add := func(fact CandidateFact) {
		identity := sources.Inspect(fact.Metadata.Candidate)
		index, found := indices[identity.Track]
		if identity.State != IdentityExact {
			found = false // unresolved/invalid seeds never deduplicate by text or recording
		}
		if !found {
			index = len(out.Targets)
			out.Targets = append(out.Targets, CandidateTarget{Identity: identity})
			if identity.State == IdentityExact {
				indices[identity.Track] = index
			}
		}
		target := &out.Targets[index]
		for _, previous := range target.Facts {
			target.Conflicts = append(target.Conflicts, compareCandidateFacts(previous, fact)...)
		}
		target.Facts = append(target.Facts, fact)
	}
	// Preserve every remote fact view, including conflicting facts for the same
	// track. One target still appears once; no "first row wins" reconciliation.
	for i, c := range remote {
		add(CandidateFact{Kind: "remote", Metadata: MetadataFacts{Candidate: c,
			Source:        fmt.Sprintf("remote:%s:%s:%d", c.Provider, c.MatchRef, i),
			AlbumComplete: nonemptyText(c.AlbumTitle), PositionVerified: sources.Inspect(c).State == IdentityExact && positive(c.DiscNumber) && positive(c.TrackNumber),
			CreditRelations: candidateCreditRelations(c, "remote")}})
	}
	for _, record := range local.Records {
		if record.TrackIdentity == nil || record.ReleaseIdentity == nil {
			out.Related = append(out.Related, record)
			continue
		}
		base := provider.Candidate{Provider: record.TrackIdentity.Provider, MatchRef: record.TrackIdentity.Ref, ReleaseRef: &record.ReleaseIdentity.Ref}
		if record.TrackIdentity.Provider != record.ReleaseIdentity.Provider || sources.Inspect(base).State != IdentityExact {
			out.Related = append(out.Related, record)
			continue
		}
		origin := "local:" + strconv.FormatInt(record.TrackID, 10)
		current := record.Current.Profile
		// Display evidence remains a whole current profile. It does not borrow
		// snapshot duration or automatically certify a user-edited position.
		add(CandidateFact{Kind: "current", LocalTrackID: record.TrackID,
			TrackRevision: record.TrackRevision, ReleaseRevision: record.ReleaseRevision,
			Current:  &record.Current,
			Metadata: MetadataFacts{Candidate: base, Names: &current, Source: origin + ":current", AlbumComplete: current.Album.Raw != "", PositionVerified: currentPositionVerified(record)}})
		if record.TrackSnapshot != nil {
			c := SnapshotCandidate(record.TrackSnapshot, record.ReleaseSnapshot)
			// A legacy snapshot can omit its release assertion; the current
			// binding supplies only identity, never missing descriptive fields.
			if c.ReleaseRef == nil {
				c.ReleaseRef = base.ReleaseRef
			}
			add(CandidateFact{Kind: "snapshot", LocalTrackID: record.TrackID,
				TrackRevision: record.TrackSnapshotRevision, ReleaseRevision: record.ReleaseSnapshotRevision,
				Track: record.TrackSnapshot, Release: record.ReleaseSnapshot,
				Metadata: MetadataFacts{Candidate: c, Source: origin + ":snapshot", AlbumComplete: nonemptyText(c.AlbumTitle),
					PositionVerified: completeLocalPosition(c, record.ReleaseSnapshot), CreditRelations: candidateCreditRelations(c, origin+":snapshot")}})
		}
	}
	for i := range out.Targets {
		target := &out.Targets[i]
		// Always re-read ownership; recalled IDs/revisions are evidence, not a
		// substitute for the current exact binding check (which can fail).
		target.Ownership, _ = ResolveLocal(lookup, sources, target.Facts[0].Metadata.Candidate)
		for _, conflict := range target.Conflicts {
			if conflict.Field == "release" {
				target.Ownership = LocalMatch{State: OwnershipConflict, Identity: target.Identity, Reason: "candidate_release_conflict"}
			}
		}
	}
	return out
}

func (t CandidateTarget) Check(input InputProfile) TargetEvidence {
	out := TargetEvidence{Eligibility: MetadataExcluded}
	eligible, unresolved := false, false
	for _, fact := range t.Facts {
		evidence := CheckMetadataEvidence(input, fact.Metadata)
		out.Facets = append(out.Facets, evidence)
		eligible = eligible || evidence.Eligibility == MetadataEligible
		unresolved = unresolved || evidence.Eligibility == MetadataUnresolved
	}
	if eligible {
		out.Eligibility = MetadataEligible
	} else if unresolved {
		out.Eligibility = MetadataUnresolved
	}
	if len(t.Conflicts) > 0 {
		out.Eligibility = MetadataUnresolved
		for _, conflict := range t.Conflicts {
			out.Reasons = append(out.Reasons, DecisionReason{Code: "candidate_fact_conflict", Field: conflict.Field, Detail: conflict.LeftSource + " <> " + conflict.RightSource})
		}
	}
	if t.Identity.State != IdentityExact || t.Ownership.State == OwnershipConflict || t.Ownership.State == OwnershipError {
		out.Eligibility = MetadataUnresolved
		out.Reasons = append(out.Reasons, DecisionReason{Code: "candidate_identity_unavailable", Detail: t.Ownership.Reason})
	}
	return out
}

func compareCandidateFacts(a, b CandidateFact) []FactConflict {
	var out []FactConflict
	add := func(field, left, right string) {
		out = append(out, FactConflict{Field: field, LeftSource: a.Metadata.Source, RightSource: b.Metadata.Source, LeftValue: left, RightValue: right})
	}
	left, right := a.Metadata.Candidate, b.Metadata.Candidate
	if nonemptyText(left.ReleaseRef) && nonemptyText(right.ReleaseRef) && *left.ReleaseRef != *right.ReleaseRef {
		add("release", *left.ReleaseRef, *right.ReleaseRef)
	}
	// A user rename is not conflicting upstream testimony. Its own evidence
	// is evaluated separately, and cannot fill holes in the source view.
	if a.Kind == "current" || b.Kind == "current" {
		return out
	}
	for _, field := range []struct {
		name        MetadataField
		left, right *string
	}{
		{MetadataTitle, left.Title, right.Title}, {MetadataArtist, left.ArtistName, right.ArtistName}, {MetadataAlbum, left.AlbumTitle, right.AlbumTitle},
	} {
		if !nonemptyText(field.left) || !nonemptyText(field.right) {
			continue
		}
		e := CompareMetadataText(ParseMetadataText(*field.left, field.name), ParseMetadataText(*field.right, field.name), MetadataTextContext{Field: field.name, IndependentSupport: true})
		if field.name == MetadataArtist {
			e = CompareMetadataCredit(ParseMetadataCredit(*field.left), ParseMetadataCredit(*field.right), MetadataCreditContext{Text: MetadataTextContext{IndependentSupport: true}})
		}
		if !e.Reliable() {
			add(string(field.name), *field.left, *field.right)
		}
	}
	for _, field := range []struct {
		name        string
		left, right *int
	}{{"disc", left.DiscNumber, right.DiscNumber}, {"track", left.TrackNumber, right.TrackNumber}, {"duration", left.DurationMS, right.DurationMS}, {"release_total", left.ReleaseTrackTotal, right.ReleaseTrackTotal}} {
		if !positive(field.left) || !positive(field.right) {
			continue
		}
		diff := *field.left - *field.right
		if diff < 0 {
			diff = -diff
		}
		if (field.name == "duration" && diff > 10000) || (field.name != "duration" && diff != 0) {
			add(field.name, strconv.Itoa(*field.left), strconv.Itoa(*field.right))
		}
	}
	for _, field := range []struct {
		name        string
		left, right *string
	}{{"recording", left.RecordingRef, right.RecordingRef}, {"group", left.ReleaseGroupRef, right.ReleaseGroupRef}, {"country", left.Country, right.Country}} {
		if nonemptyText(field.left) && nonemptyText(field.right) && *field.left != *field.right {
			add(field.name, *field.left, *field.right)
		}
	}
	return out
}

func nonemptyText(p *string) bool { return p != nil && *p != "" }
func positive(p *int) bool        { return p != nil && *p > 0 }
func snapshotValue[T any](f domain.Field[T]) *T {
	if f.State != domain.Value {
		return nil
	}
	value := f.Value
	return &value
}

// SnapshotCandidate projects already normalized facts, not provider-specific
// IDs. Missing track or release fields remain missing; no query/current values
// enter the source candidate, even if the source has no display title.
func SnapshotCandidate(track *domain.TrackSnapshot, release *domain.ReleaseSnapshot) provider.Candidate {
	c := provider.Candidate{Provider: track.Identity.Provider, MatchRef: track.Identity.Ref,
		Title: snapshotValue(track.Title), Year: snapshotValue(track.Year), DiscNumber: snapshotValue(track.DiscNumber), TrackNumber: snapshotValue(track.TrackNumber), DurationMS: snapshotValue(track.DurationMS)}
	if track.Credit.State == domain.Value {
		c.ArtistName = &track.Credit.Value.Text
	}
	if track.Release != nil {
		c.ReleaseRef = &track.Release.Ref
	}
	if track.Recording.State == domain.Value {
		c.RecordingRef = &track.Recording.Value.Ref
	}
	if track.Identifiers.State == domain.Value {
		c.Identifiers = track.Identifiers.Value
	}
	if release != nil {
		c.ReleaseRef = &release.Identity.Ref
		c.AlbumTitle, c.ReleaseTrackTotal, c.Country = snapshotValue(release.Title), snapshotValue(release.ReleaseTrackTotal), snapshotValue(release.Country)
		c.PrimaryType, c.ReleaseStatus = snapshotValue(release.PrimaryType), snapshotValue(release.Status)
		if release.Credit.State == domain.Value {
			c.AlbumCredit = &release.Credit.Value.Text
		}
		if release.Group != nil {
			c.ReleaseGroupRef = &release.Group.Ref
		}
		if release.Media.State == domain.Value {
			c.Media = release.Media.Value
		}
	}
	return c
}

func completeLocalPosition(c provider.Candidate, release *domain.ReleaseSnapshot) bool {
	if release == nil || release.TracklistCompleteness != domain.Complete || release.Tracklist.State != domain.Value || !positive(c.DiscNumber) || !positive(c.TrackNumber) {
		return false
	}
	matches := 0
	for _, entry := range release.Tracklist.Value {
		if entry.Track.State == domain.Value && entry.Track.Value == (domain.Identity{Provider: c.Provider, Ref: c.MatchRef}) {
			if entry.DiscNumber != *c.DiscNumber || entry.TrackNumber != *c.TrackNumber {
				return false
			}
			matches++
		}
	}
	return matches == 1
}

func candidateCreditRelations(c provider.Candidate, source string) []CreditRelation {
	if !nonemptyText(c.AlbumCredit) || !nonemptyText(c.ArtistName) {
		return nil
	}
	return []CreditRelation{{Kind: "release_credit", From: *c.AlbumCredit, To: *c.ArtistName, Source: source}}
}

func currentPositionVerified(record LocalRecord) bool {
	if record.TrackSnapshot == nil || record.ReleaseSnapshot == nil {
		return false
	}
	c := SnapshotCandidate(record.TrackSnapshot, record.ReleaseSnapshot)
	p := record.Current.Profile
	return completeLocalPosition(c, record.ReleaseSnapshot) && p.Disc.State == ValueKnown && p.Track.State == ValueKnown &&
		p.Disc.Value == int64(*c.DiscNumber) && p.Track.Value == int64(*c.TrackNumber)
}
