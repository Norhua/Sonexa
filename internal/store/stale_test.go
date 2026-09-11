package store

import (
	"testing"
	"time"
)

// TestITunesTracklistExpires 覆盖 7.7 与 15.13：
// 过期的 iTunes 清单被判为 none，而 MB 清单不受时间约束。
func TestITunesTracklistExpires(t *testing.T) {
	cfg := DefaultStaleConfig()
	old := time.Now().AddDate(0, 0, -40) // 40 天前，超过默认 30 天
	recent := time.Now().AddDate(0, 0, -5)

	cases := []struct {
		name      string
		provider  string
		fetchedAt *time.Time
		wantStale bool
		wantState TracklistState
	}{
		{"iTunes 清单过期", "itunes", &old, true, TracklistNone},
		{"iTunes 清单未过期", "itunes", &recent, false, TracklistComplete},
		{"iTunes 无获取时间视为过期", "itunes", nil, true, TracklistNone},
		{"MusicBrainz 一年前的清单仍有效", "musicbrainz", ptrTime(time.Now().AddDate(-1, 0, 0)), false, TracklistComplete},
		{"MusicBrainz 无获取时间也有效", "musicbrainz", nil, false, TracklistComplete},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Release{
				Provider:       c.provider,
				TracklistState: TracklistComplete,
				FetchedAt:      c.fetchedAt,
			}
			if got := cfg.IsStale(r); got != c.wantStale {
				t.Errorf("IsStale = %v, 期望 %v", got, c.wantStale)
			}
			if got := cfg.EffectiveTracklistState(r); got != c.wantState {
				t.Errorf("有效清单状态 = %q, 期望 %q", got, c.wantState)
			}
		})
	}
}

// TestCustomITunesTTL 确保 TTL 可配置。
func TestCustomITunesTTL(t *testing.T) {
	cfg := StaleConfig{ITunesTTL: 24 * time.Hour}
	twoDaysAgo := time.Now().AddDate(0, 0, -2)
	r := &Release{Provider: "itunes", TracklistState: TracklistComplete, FetchedAt: &twoDaysAgo}

	if !cfg.IsStale(r) {
		t.Error("TTL 为 1 天时，2 天前的清单应已过期")
	}
}

// TestNilReleaseIsStale 确保缺失的发行被当作需要重取。
func TestNilReleaseIsStale(t *testing.T) {
	cfg := DefaultStaleConfig()
	if !cfg.IsStale(nil) {
		t.Error("缺失的发行应被判为需要重取")
	}
	if got := cfg.EffectiveTracklistState(nil); got != TracklistNone {
		t.Errorf("缺失发行的状态 = %q, 期望 none", got)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
