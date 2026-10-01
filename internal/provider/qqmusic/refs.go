package qqmusic

import (
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"net/url"
	"strings"
)

type refCodec struct{}

func (refCodec) Description() provider.RefDescription {
	return provider.RefDescription{TrackHint: "QQ 音乐歌曲 MID 或歌曲链接", ReleaseHint: "QQ 音乐专辑 MID 或专辑链接"}
}
func qqRef(input, kind string) (domain.Identity, error) {
	ref := strings.TrimSpace(input)
	if strings.Contains(ref, "://") {
		u, err := url.Parse(ref)
		if err != nil || u.Hostname() != "y.qq.com" {
			return domain.Identity{}, fmt.Errorf("无效 QQ 音乐链接")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || (parts[len(parts)-2] != kind+"Detail" && parts[len(parts)-2] != kind) {
			return domain.Identity{}, fmt.Errorf("需要 QQ 音乐 %s 链接", kind)
		}
		ref = strings.TrimSuffix(parts[len(parts)-1], ".html")
	}
	if !ValidMID(ref) {
		return domain.Identity{}, fmt.Errorf("QQ 音乐标识应为 10–20 位字母数字 MID")
	}
	return domain.Identity{Provider: provider.QQMusic, Ref: ref}, nil
}
func (refCodec) ParseTrack(input, region string) (domain.TrackIdentity, error) {
	return qqRef(input, "song")
}
func (refCodec) ParseRelease(input, region string) (domain.ReleaseIdentity, error) {
	return qqRef(input, "album")
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
