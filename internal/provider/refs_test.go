package provider_test

import (
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
	"testing"
)

// TestValidateMatchRef 覆盖 8.1：手填非法标识被拒绝。
func TestValidateMatchRef(t *testing.T) {
	registry := provider.NewRegistry()
	registry.Register(&musicbrainz.Provider{})
	registry.Register(&itunes.Provider{})
	registry.Register(&qqmusic.Provider{})
	cases := []struct {
		name   string
		source provider.ProviderID
		ref    string
		ok     bool
	}{
		{"合法 MBID", provider.MusicBrainz, "c8b0a1f2-3d4e-5f60-9a8b-7c6d5e4f3a2b", true},
		{"MBID 大写也合法", provider.MusicBrainz, "C8B0A1F2-3D4E-5F60-9A8B-7C6D5E4F3A2B", true},
		{"非 UUID 被拒", provider.MusicBrainz, "not-a-uuid", false},
		{"iTunes 标识不能用于 MB", provider.MusicBrainz, "jp:534038851", false},
		{"空串被拒", provider.MusicBrainz, "", false},
		{"合法 iTunes 标识", provider.ITunes, "jp:534038851", true},
		{"iTunes 缺地区被拒", provider.ITunes, "534038851", false},
		{"iTunes 地区需两位小写", provider.ITunes, "JP:534038851", false},
		{"MBID 不能用于 iTunes", provider.ITunes, "c8b0a1f2-3d4e-5f60-9a8b-7c6d5e4f3a2b", false},
		{"未知上游被拒", provider.ProviderID("unknown"), "anything", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := registry.Get(c.source)
			if err == nil {
				_, err = p.Refs().ParseTrack(c.ref, "")
			}
			if c.ok && err != nil {
				t.Errorf("应通过校验, 得到 %v", err)
			}
			if !c.ok && err == nil {
				t.Error("应被拒绝")
			}
			// 被拒时错误信息要说明格式要求
			if !c.ok && err != nil && err.Error() == "" {
				t.Error("错误信息应说明格式要求")
			}
		})
	}
}

func TestRefLinksAndExactTargets(t *testing.T) {
	for _, tc := range []struct {
		p                            provider.MetadataProvider
		input, region, want, release string
	}{
		{&musicbrainz.Provider{}, "https://musicbrainz.org/track/C8B0A1F2-3D4E-5F60-9A8B-7C6D5E4F3A2B", "", "c8b0a1f2-3d4e-5f60-9a8b-7c6d5e4f3a2b", "8c4d9698-bcac-456a-a5ac-1bd90d64c4ff"},
		{&itunes.Provider{}, "https://music.apple.com/jp/album/test/123?i=456", "", "jp:456", "jp:123"},
		{&itunes.Provider{}, "456", "JP", "jp:456", "jp:123"},
		{&qqmusic.Provider{}, "https://y.qq.com/n/ryqq/songDetail/003RcjkV1XjQQy", "", "003RcjkV1XjQQy", "000BDJvT2aWQYZ"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			id, err := tc.p.Refs().ParseTrack(tc.input, tc.region)
			if err != nil || id.Ref != tc.want {
				t.Fatalf("%+v %v", id, err)
			}
			if _, err := tc.p.Refs().Target(tc.release, id.Ref); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := (&itunes.Provider{}).Refs().Target("jp:123", "us:456"); err == nil {
		t.Fatal("mixed storefront accepted")
	}
	if _, err := (&musicbrainz.Provider{}).Refs().ParseTrack("https://musicbrainz.org/recording/c8b0a1f2-3d4e-5f60-9a8b-7c6d5e4f3a2b", ""); err == nil {
		t.Fatal("recording accepted as track")
	}
	if got := itunes.MakeRef("JP", 534038851); got != "jp:534038851" {
		t.Fatal(got)
	}
}
