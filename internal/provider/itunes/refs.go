package itunes

import (
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"net/url"
	"regexp"
	"strings"
)

type refCodec struct{}

var canonicalRef = regexp.MustCompile(`^[a-z]{2}:[1-9][0-9]*$`)
var numericRef = regexp.MustCompile(`^[1-9][0-9]*$`)

func (refCodec) Description() provider.RefDescription {
	return provider.RefDescription{TrackHint: "小写地区:歌曲编号（如 jp:534038851），或 Apple Music 歌曲链接", ReleaseHint: "小写地区:专辑编号，或 Apple Music 专辑链接", RequiresRegion: true}
}
func appleRef(input, region string, track bool) (domain.Identity, error) {
	ref := strings.TrimSpace(input)
	if strings.Contains(ref, "://") {
		u, err := url.Parse(ref)
		if err != nil || (u.Hostname() != "music.apple.com" && u.Hostname() != "itunes.apple.com") {
			return domain.Identity{}, fmt.Errorf("无效 Apple Music 链接")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 3 {
			return domain.Identity{}, fmt.Errorf("链接缺少地区和编号")
		}
		region = parts[0]
		ref = strings.TrimPrefix(parts[len(parts)-1], "id")
		if track {
			if id := u.Query().Get("i"); id != "" {
				ref = id
			} else if parts[1] != "song" {
				return domain.Identity{}, fmt.Errorf("歌曲链接缺少歌曲编号 i")
			}
		}
		ref = region + ":" + ref
	} else if numericRef.MatchString(ref) && region != "" {
		ref = strings.ToLower(region) + ":" + ref
	}
	if !canonicalRef.MatchString(ref) {
		return domain.Identity{}, fmt.Errorf("iTunes 标识应为小写地区:数字编号")
	}
	return domain.Identity{Provider: provider.ITunes, Ref: ref}, nil
}
func (refCodec) ParseTrack(input, region string) (domain.TrackIdentity, error) {
	return appleRef(input, region, true)
}
func (refCodec) ParseRelease(input, region string) (domain.ReleaseIdentity, error) {
	return appleRef(input, region, false)
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
		_, rc := SplitRef(r.Ref)
		_, tc := SplitRef(v.Ref)
		if rc != tc {
			return t, fmt.Errorf("歌曲与专辑 storefront 不一致")
		}
		t.Track = &v
	}
	return t, t.Validate()
}
