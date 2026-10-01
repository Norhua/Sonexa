package musicbrainz

import (
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

func releaseCounts(rel release) (*int, []domain.Medium) {
	var media []domain.Medium
	total, known := 0, len(rel.Media) > 0
	if rel.MediumCount > 0 && len(rel.Media) != rel.MediumCount {
		known = false
	}
	seen := map[int]bool{}
	for _, m := range rel.Media {
		entry := domain.Medium{Number: m.Position, Format: provider.FieldFromPointer(strPtr(m.Format))}
		if m.TrackCount > 0 {
			entry.TrackTotal = domain.Known(m.TrackCount)
			total += m.TrackCount
		} else {
			known = false
		}
		if m.Position <= 0 || seen[m.Position] {
			known = false
		}
		seen[m.Position] = true
		media = append(media, entry)
	}
	if rel.TrackCount != nil && *rel.TrackCount > 0 {
		return rel.TrackCount, media
	}
	// 只有完整媒体摘要才求和；搜索只返回命中碟时总数仍未知。
	if known && (rel.MediumCount > 0 || allDetailMedia(rel.Media)) {
		return &total, media
	}
	return nil, media
}
func allDetailMedia(media []medium) bool {
	for _, m := range media {
		if m.Tracks == nil {
			return false
		}
	}
	return len(media) > 0
}
func releaseLabels(rel release) []domain.Label {
	var out []domain.Label
	for _, l := range rel.LabelInfo {
		v := domain.Label{CatalogNumber: l.CatalogNumber}
		if l.Label != nil {
			v.Name = l.Label.Name
			if l.Label.ID != "" {
				v.Identity = &domain.Identity{Provider: provider.MusicBrainz, Ref: l.Label.ID}
			}
		}
		out = append(out, v)
	}
	return out
}
func recordingIdentifiers(rec *recording) ([]domain.Identifier, []string) {
	var ids []domain.Identifier
	var works []string
	for _, v := range rec.ISRCs {
		ids = append(ids, domain.Identifier{Kind: "ISRC", Value: v})
	}
	for _, rel := range rec.Relations {
		if rel.Work != nil {
			if rel.Work.ID != "" {
				works = append(works, rel.Work.ID)
			}
			for _, v := range rel.Work.ISWCs {
				ids = append(ids, domain.Identifier{Kind: "ISWC", Value: v})
			}
		}
	}
	return ids, works
}
func fillCandidateFacts(c *provider.Candidate, rec recording, rel release) {
	c.AlbumCredit = strPtr(joinArtistCredit(rel.ArtistCredit))
	c.Credits = parseCreditArtists(rec.ArtistCredit)
	for _, m := range rel.Media {
		for _, t := range m.TrackList() {
			if t.ID == c.MatchRef && joinArtistCredit(t.ArtistCredit) != "" {
				c.Credits = parseCreditArtists(t.ArtistCredit)
			}
		}
	}
	c.AlbumCredits = parseCreditArtists(rel.ArtistCredit)
	if rel.Barcode != "" {
		c.ReleaseIdentifiers = []domain.Identifier{{Kind: "barcode", Value: rel.Barcode}}
	}
	c.ReleaseTrackTotal, c.Media = releaseCounts(rel)
	c.Labels = releaseLabels(rel)
	c.Identifiers, _ = recordingIdentifiers(&rec)
	if rel.ReleaseGroup != nil {
		c.GroupCredits = parseCreditArtists(rel.ReleaseGroup.ArtistCredit)
		c.GroupTitle = strPtr(rel.ReleaseGroup.Title)
		c.GroupCredit = strPtr(joinArtistCredit(rel.ReleaseGroup.ArtistCredit))
	}
}
