package musicbrainz

import (
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"net/url"
	"regexp"
	"strings"
)

type refCodec struct{}

var uuidRef = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (refCodec) Description() provider.RefDescription {
	return provider.RefDescription{TrackHint: "MusicBrainz track UUID 或曲目链接", ReleaseHint: "MusicBrainz release UUID 或发行链接"}
}
func mbRef(input, kind string) (domain.Identity, error) {
	ref := strings.TrimSpace(input)
	if strings.Contains(ref, "://") {
		u, err := url.Parse(ref)
		if err != nil || (u.Hostname() != "musicbrainz.org" && u.Hostname() != "www.musicbrainz.org") {
			return domain.Identity{}, fmt.Errorf("无效 MusicBrainz 链接")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 2 || parts[0] != kind {
			return domain.Identity{}, fmt.Errorf("需要 MusicBrainz %s 链接", kind)
		}
		ref = parts[1]
	}
	if !uuidRef.MatchString(ref) {
		return domain.Identity{}, fmt.Errorf("MusicBrainz %s 标识应为 UUID", kind)
	}
	return domain.Identity{Provider: provider.MusicBrainz, Ref: strings.ToLower(ref)}, nil
}
func (refCodec) ParseTrack(input, region string) (domain.TrackIdentity, error) {
	return mbRef(input, "track")
}
func (refCodec) ParseRelease(input, region string) (domain.ReleaseIdentity, error) {
	return mbRef(input, "release")
}
func (c refCodec) Target(release, track string) (domain.DetailTarget, error) {
	r, err := c.ParseRelease(release, "")
	if err != nil {
		return domain.DetailTarget{}, err
	}
	t := domain.DetailTarget{Release: r}
	if track != "" {
		v, err := c.ParseTrack(track, "")
		if err != nil {
			return t, err
		}
		t.Track = &v
	}
	return t, t.Validate()
}
