package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

var ErrUnsupported = errors.New("来源不支持此能力")

type RefDescription struct {
	TrackHint      string `json:"trackHint"`
	ReleaseHint    string `json:"releaseHint"`
	RequiresRegion bool   `json:"requiresRegion"`
}
type RefCodec interface {
	Description() RefDescription
	ParseTrack(input, region string) (domain.TrackIdentity, error)
	ParseRelease(input, region string) (domain.ReleaseIdentity, error)
	Target(releaseRef, trackRef string) (domain.DetailTarget, error)
}
type Page struct {
	Offset int
	Limit  int
}
type VersionPage struct {
	Releases []domain.ReleaseSummary
	Total    *int
}
type VersionLister interface {
	ListVersions(context.Context, domain.GroupIdentity, Page) (*VersionPage, error)
}
type ArtworkTarget struct{ Release domain.ReleaseIdentity }
type ArtworkProvider = ArtworkStrategist

func (r *Registry) ListVersions(ctx context.Context, group domain.GroupIdentity, page Page) (*VersionPage, error) {
	p, err := r.Get(group.Provider)
	if err != nil {
		return nil, err
	}
	v, ok := p.(VersionLister)
	if !ok {
		return nil, ErrUnsupported
	}
	return v.ListVersions(ctx, group, page)
}
func (r *Registry) LoadArtworkCatalog(ctx context.Context, target ArtworkTarget, io ArtworkIO) (*domain.CoverCatalogInput, error) {
	p, err := r.Get(target.Release.Provider)
	if err != nil {
		return nil, err
	}
	a, ok := p.(ArtworkProvider)
	if !ok {
		return nil, ErrUnsupported
	}
	policy := a.ArtworkStrategy()
	key := domain.CoverCatalogKey{Source: policy.Source, ObjectType: policy.ObjectType, SourceRef: target.Release.Ref}
	if err := policy.Validate(key.ObjectType, key.SourceRef); err != nil {
		return nil, err
	}
	return policy.Load(ctx, key, io)
}

func FieldFromPointer[T any](v *T) domain.Field[T] {
	if v == nil {
		return domain.Missing[T]()
	}
	return domain.Known(*v)
}
func identity(id ProviderID, ref *string) *domain.Identity {
	if ref == nil || *ref == "" {
		return nil
	}
	return &domain.Identity{Provider: id, Ref: *ref}
}
func creditField(id ProviderID, text *string, members []CreditArtist) domain.Field[domain.Credit] {
	if text == nil {
		return domain.Missing[domain.Credit]()
	}
	c := domain.Credit{Text: *text, Members: []domain.CreditMember{}}
	for _, m := range members {
		ref := m.Identity
		if ref == nil {
			ref = identity(id, m.MBID)
		}
		name := m.CanonicalName
		if name == "" {
			name = m.Name
		}
		a := domain.Artist{Identity: ref, Name: name}
		if m.SortName != nil {
			a.SortName = *m.SortName
		}
		if m.Type != nil {
			a.Type = *m.Type
		}
		c.Members = append(c.Members, domain.CreditMember{Artist: a, CreditedName: m.Name, Start: m.Start, End: m.End})
	}
	return domain.Known(c)
}
func nonempty[T any](v []T) domain.Field[[]T] {
	if len(v) == 0 {
		return domain.Missing[[]T]()
	}
	return domain.Known(v)
}

// CandidateSnapshot 只转换来源已经归一化的字段，不出网也不借用查询文本。
func CandidateSnapshot(id ProviderID, c Candidate) (*domain.Snapshot, error) {
	if c.Provider != id {
		return nil, fmt.Errorf("候选来源与适配器不一致")
	}
	d := &Detail{Provider: id, MatchRef: c.MatchRef, Title: c.Title, TrackCredit: c.ArtistName,
		Credits: c.Credits, AlbumCredits: c.AlbumCredits, GroupCredits: c.GroupCredits, ReleaseIdentifiers: c.ReleaseIdentifiers,
		AlbumTitle: c.AlbumTitle, AlbumCredit: c.AlbumCredit, GroupTitle: c.GroupTitle, GroupCredit: c.GroupCredit,
		ReleaseRef: c.ReleaseRef, ReleaseGroupRef: c.ReleaseGroupRef, PrimaryType: c.PrimaryType,
		Year: c.Year, TrackNumber: c.TrackNumber, DiscNumber: c.DiscNumber, DurationMS: c.DurationMS,
		RecordingRef: c.RecordingRef, FirstReleaseDate: c.FirstReleaseDate, Country: c.Country,
		ReleaseStatus: c.ReleaseStatus, ReleaseTrackTotal: c.ReleaseTrackTotal, Media: c.Media,
		Labels: c.Labels, Identifiers: c.Identifiers, TracklistCompleteness: domain.Unloaded}
	return DetailSnapshot(d), nil
}

// DetailSnapshot 的输入须已由来源适配器验证并归一化，不推测来源字段含义。
func DetailSnapshot(d *Detail) *domain.Snapshot {
	meta := domain.SnapshotMeta{Provider: d.Provider, Completeness: domain.Partial}
	if !d.FetchedAt.IsZero() {
		meta.FetchedAt = d.FetchedAt.UTC().Format(time.RFC3339Nano)
	}
	s := &domain.Snapshot{}
	group := identity(d.Provider, d.ReleaseGroupRef)
	if group != nil {
		s.Group = &domain.GroupSnapshot{Meta: meta, Identity: *group, Title: FieldFromPointer(d.GroupTitle), Credit: creditField(d.Provider, d.GroupCredit, d.GroupCredits), PrimaryType: FieldFromPointer(d.PrimaryType), SecondaryTypes: nonempty(d.SecondaryTypes), FirstReleaseDate: FieldFromPointer(d.FirstReleaseDate)}
	}
	release := identity(d.Provider, d.ReleaseRef)
	if release != nil {
		r := &domain.ReleaseSnapshot{Meta: meta, Identity: *release, Group: group, Title: FieldFromPointer(d.AlbumTitle), Credit: creditField(d.Provider, d.AlbumCredit, d.AlbumCredits), PrimaryType: FieldFromPointer(d.PrimaryType), SecondaryTypes: nonempty(d.SecondaryTypes), DiscTotal: FieldFromPointer(d.DiscTotal), Media: nonempty(d.Media), ReleaseTrackTotal: FieldFromPointer(d.ReleaseTrackTotal), Date: FieldFromPointer(d.ReleaseDate), Country: FieldFromPointer(d.Country), Status: FieldFromPointer(d.ReleaseStatus), Labels: nonempty(d.Labels), Identifiers: nonempty(d.ReleaseIdentifiers), Annotation: FieldFromPointer(d.Comment), TracklistCompleteness: d.TracklistCompleteness}
		if r.TracklistCompleteness == "" {
			r.TracklistCompleteness = domain.Unloaded
		}
		var entries []domain.TracklistEntry
		for _, e := range d.Tracklist {
			entry := domain.TracklistEntry{DiscNumber: e.DiscNumber, TrackNumber: e.TrackNumber, Title: domain.Known(e.Title), Credit: creditField(d.Provider, e.TrackCredit, nil), DurationMS: FieldFromPointer(e.DurationMS), Identifiers: nonempty(e.Identifiers)}
			if ref := identity(d.Provider, e.TrackRef); ref != nil {
				entry.Track = domain.Known(*ref)
			}
			if ref := identity(d.Provider, e.RecordingRef); ref != nil {
				entry.Recording = domain.Known(*ref)
			}
			entries = append(entries, entry)
		}
		if len(entries) > 0 {
			r.Tracklist = domain.Known(entries)
		}
		if d.ArtworkURL != nil {
			r.Artwork = domain.Known([]domain.Artwork{{Identity: *release, URL: *d.ArtworkURL, Type: "Front"}})
		}
		s.Release = r
	}
	if d.MatchRef != "" {
		t := &domain.TrackSnapshot{Meta: meta, Identity: domain.Identity{Provider: d.Provider, Ref: d.MatchRef}, Release: release, Title: FieldFromPointer(d.Title), Credit: creditField(d.Provider, d.TrackCredit, d.Credits), Year: FieldFromPointer(d.Year), DiscNumber: FieldFromPointer(d.DiscNumber), TrackNumber: FieldFromPointer(d.TrackNumber), TrackNumberText: FieldFromPointer(d.TrackNumberText), Genre: FieldFromPointer(d.Genre), Lyrics: FieldFromPointer(d.Lyrics), DurationMS: FieldFromPointer(d.DurationMS), Identifiers: nonempty(d.Identifiers), Roles: map[string]domain.Field[[]domain.Role]{}}
		if ref := identity(d.Provider, d.RecordingRef); ref != nil {
			t.Recording = domain.Known(*ref)
		}
		var works []domain.Identity
		for _, ref := range d.WorkRefs {
			works = append(works, domain.Identity{Provider: d.Provider, Ref: ref})
		}
		t.Works = nonempty(works)
		for _, a := range d.Artists {
			if a.Role == "album_artist" {
				continue
			}
			ref := a.Identity
			if ref == nil {
				ref = identity(d.Provider, a.MBID)
			}
			artist := domain.Artist{Name: a.Name, Identity: ref}
			if a.Type != nil {
				artist.Type = *a.Type
			}
			if a.SortName != nil {
				artist.SortName = *a.SortName
			}
			attrs := append([]string(nil), a.Attributes...)
			if len(attrs) == 0 && a.Attribute != nil {
				attrs = []string{*a.Attribute}
			}
			role := domain.Role{Artist: artist, Role: a.Role, Attributes: attrs, Position: a.Position}
			if a.CreditedName != nil {
				role.CreditedName = *a.CreditedName
			}
			f := t.Roles[a.Role]
			f.State = domain.Value
			f.Value = append(f.Value, role)
			t.Roles[a.Role] = f
		}
		s.Track = t
	}
	return s
}

func CompleteUsing(ctx context.Context, p MetadataProvider, target domain.DetailTarget) (*domain.Snapshot, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	if target.Release.Provider != p.ID() {
		return nil, fmt.Errorf("详情来源不匹配")
	}
	trackRef := ""
	if target.Track != nil {
		trackRef = target.Track.Ref
	}
	validated, err := p.Refs().Target(target.Release.Ref, trackRef)
	if err != nil {
		return nil, err
	}
	if validated.Track != nil {
		trackRef = validated.Track.Ref
	}
	d, err := p.FetchDetail(ctx, Ref{Provider: p.ID(), ID: validated.Release.Ref, TrackRef: trackRef, SkipCache: target.SkipCache})
	if err != nil {
		return nil, err
	}
	if d == nil || d.Provider != p.ID() || d.ReleaseRef == nil || *d.ReleaseRef != validated.Release.Ref || d.MatchRef != trackRef {
		return nil, fmt.Errorf("详情响应未匹配精确目标")
	}
	return DetailSnapshot(d), nil
}
func SummaryUsing(ctx context.Context, p MetadataProvider, release domain.ReleaseIdentity) (*domain.ReleaseSummary, error) {
	s, err := p.Complete(ctx, domain.DetailTarget{Release: release})
	if err != nil {
		return nil, err
	}
	if s.Release == nil {
		return nil, fmt.Errorf("来源未返回发行摘要")
	}
	summary := s.Release.Summary()
	return &summary, nil
}
