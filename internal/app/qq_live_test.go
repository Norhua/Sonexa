package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func TestLiveQQMetadata(t *testing.T) {
	requireLive(t, "SONEXA_QQ_METADATA_LIVE", os.Getenv("SONEXA_QQ_METADATA_LIVE"))
	a, audit := newLiveApp(t, 12)
	ctx, cancel := liveContext(a, audit, 2*time.Minute)
	defer cancel()
	p, err := a.registry.Get(provider.QQMusic)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := p.SearchTracks(ctx, provider.Query{Title: "晴天", Artist: "周杰伦", Limit: 25, SkipCache: true})
	if err != nil || len(rows) == 0 {
		t.Fatalf("metadata search candidates=%d err=%v", len(rows), err)
	}
	t.Logf("metadata search candidates=%d", len(rows))
	for _, sample := range []struct {
		name, album, track string
		discs              int
	}{
		{"single-disc", "000MkMni19ClKG", "0039MnYb0qxYhV", 1},
		{"multi-disc", "0032ezFm3F53yO", "000zwjWq0T51ys", 2},
	} {
		if !t.Run(sample.name, func(t *testing.T) {
			d, err := p.FetchDetail(ctx, provider.Ref{Provider: provider.QQMusic, ID: sample.album, TrackRef: sample.track, SkipCache: true})
			if err != nil {
				t.Fatal(err)
			}
			if d.MatchRef != sample.track || d.ReleaseRef == nil || *d.ReleaseRef != sample.album || len(d.Tracklist) == 0 || d.DiscTotal == nil || *d.DiscTotal != sample.discs {
				t.Fatal("metadata identity, tracklist or disc count changed")
			}
			positions := map[int]int{}
			for _, track := range d.Tracklist {
				if track.TrackRef == nil || *track.TrackRef == "" || track.DiscNumber < 1 || track.TrackNumber != positions[track.DiscNumber]+1 {
					t.Fatal("tracklist position/identity changed")
				}
				positions[track.DiscNumber] = track.TrackNumber
			}
			if len(positions) != sample.discs {
				t.Fatal("disc structure incomplete")
			}
			lyricBytes := 0
			if d.Lyrics != nil {
				lyricBytes = len(*d.Lyrics)
			}
			t.Logf("album=%s discs=%d tracks=%d roles=%d lyric_bytes=%d", sample.album, len(positions), len(d.Tracklist), len(d.Artists), lyricBytes)
		}) {
			return
		}
	}
	// A real fallback needs a real sample without inline lyrics. Do not strip
	// lyrics from a live response or substitute a fixture to manufacture evidence.
	album, track := os.Getenv("SONEXA_QQ_FALLBACK_ALBUM"), os.Getenv("SONEXA_QQ_FALLBACK_TRACK")
	if album == "" || track == "" {
		t.Fatal("歌词回退未验证：需 SONEXA_QQ_FALLBACK_ALBUM/TRACK 指定详情不含歌词的真实样例；不改写响应制造回退")
	}
	before := len(audit.snapshot())
	d, err := p.FetchDetail(ctx, provider.Ref{Provider: provider.QQMusic, ID: album, TrackRef: track, SkipCache: true})
	if err != nil {
		t.Fatal(err)
	}
	fallback := false
	for _, request := range audit.snapshot()[before:] {
		fallback = fallback || strings.Contains(request.path, "fcg_query_lyric_new.fcg") && request.status == 200
	}
	if !fallback || d.Lyrics == nil || *d.Lyrics == "" {
		t.Fatal("真实样例未触发并成功取得歌词回退；保持未验证")
	}
	t.Logf("metadata fallback requests=%d lyric_bytes=%d", len(audit.snapshot())-before, len(*d.Lyrics))
}

// Discovery is a separately authorized, one-shot investigation, not a fallback
// inside the regression test. At most two searches, three details per search,
// and sixteen actual API sends including pagination, fallback and redirects.
// Titles may be selected before the run; they never increase its budget.
func qqDiscoveryTitles(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return []string{"綿雪の夢", "世界中の雨"}, nil
	}
	var titles []string
	seen := map[string]bool{}
	for _, title := range strings.Split(value, "\n") {
		title = strings.TrimSpace(title)
		if title != "" && !seen[title] {
			titles = append(titles, title)
			seen[title] = true
		}
	}
	if len(titles) > 2 {
		return nil, fmt.Errorf("样例查找最多指定两个歌曲标题，不扩大单轮查询上限")
	}
	return titles, nil
}

func TestLiveQQFallbackDiscovery(t *testing.T) {
	requireLive(t, "SONEXA_QQ_FALLBACK_DISCOVERY", os.Getenv("SONEXA_QQ_FALLBACK_DISCOVERY"))
	titles, err := qqDiscoveryTitles(os.Getenv("SONEXA_QQ_DISCOVERY_TITLES"))
	if err != nil {
		t.Fatal(err)
	}
	a, audit := newLiveApp(t, 16)
	ctx, cancel := liveContext(a, audit, 90*time.Second)
	defer cancel()
	p, err := a.registry.Get(provider.QQMusic)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	checked := 0
	for _, title := range titles {
		rows, err := p.SearchTracks(ctx, provider.Query{Title: title, Limit: 3, SkipCache: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("discovery query=%q candidates=%d", title, len(rows))
		for _, c := range rows[:min(3, len(rows))] {
			if c.ReleaseRef == nil || c.MatchRef == "" || seen[c.MatchRef] {
				continue
			}
			seen[c.MatchRef] = true
			before := len(audit.snapshot())
			d, err := p.FetchDetail(ctx, provider.Ref{Provider: provider.QQMusic, ID: *c.ReleaseRef, TrackRef: c.MatchRef, SkipCache: true})
			if err != nil {
				t.Fatal(err)
			}
			// Metadata deliberately tolerates lyric failure; discovery must not
			// hide a restriction and move on to another candidate.
			audit.mu.Lock()
			stopped := audit.stopped
			audit.mu.Unlock()
			if stopped {
				t.Fatal(errLiveStopped)
			}
			checked++
			fallback := false
			for _, r := range audit.snapshot()[before:] {
				fallback = fallback || strings.Contains(r.path, "fcg_query_lyric_new.fcg") && r.status == 200
			}
			lyricBytes := 0
			usable := false
			if d.Lyrics != nil {
				lyricBytes = len(*d.Lyrics)
				_, parseErr := lyrics.DocumentFromPayload(&lyrics.Payload{Text: *d.Lyrics})
				usable = parseErr == nil
			}
			t.Logf("discovery candidate=%d album=%s track=%s fallback=%t usable_lyrics=%t lyric_bytes=%d", checked, *c.ReleaseRef, c.MatchRef, fallback, usable, lyricBytes)
			if fallback && usable {
				t.Logf("FOUND SONEXA_QQ_FALLBACK_ALBUM=%s SONEXA_QQ_FALLBACK_TRACK=%s requests=%d", *c.ReleaseRef, c.MatchRef, len(audit.snapshot()))
				return
			}
		}
	}
	t.Fatalf("本次有界查找未找到成功回退样例：details=%d requests=%d；停止，不扩大查询或改写响应", checked, len(audit.snapshot()))
}

// Deliberately sequential and bounded: no concurrency, retry-on-failure or
// automatic continuation at a higher rate after any failed stage.
func sampleQQRates(configure func(int) error, request func(int) error) error {
	for _, rate := range []int{30, 45, 60} {
		if err := configure(rate); err != nil {
			return err
		}
		for business := range 3 {
			if err := request(business); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestLiveQQRateSamples(t *testing.T) {
	requireLive(t, "SONEXA_QQ_RATE_LIVE", os.Getenv("SONEXA_QQ_RATE_LIVE"))
	a, audit := newLiveApp(t, 12)
	ctx, cancel := liveContext(a, audit, 90*time.Second)
	defer cancel()
	err := sampleQQRates(func(rate int) error {
		if err := audit.gate(); err != nil {
			return err
		}
		next := a.settings.Get()
		next.QQMusic.RatePerMin = float64(rate)
		if err := a.SaveSettings(next); err != nil {
			return err
		}
		a.client.HTTP.Transport = audit.transport(http.DefaultTransport, true)
		t.Logf("sample rate_per_minute=%d requests=3 (metadata, covers, lyrics)", rate)
		return nil
	}, func(business int) error {
		switch business {
		case 0:
			p, _ := a.registry.Get(provider.QQMusic)
			rows, err := p.SearchTracks(ctx, provider.Query{Title: "晴天", Artist: "周杰伦", SkipCache: true})
			if err == nil && len(rows) == 0 {
				err = fmt.Errorf("no metadata candidates")
			}
			return err
		case 1:
			p, _ := a.coverSources.Get(provider.QQMusic)
			rows, err := p.SearchCovers(ctx, sources.Query{Title: "晴天", Artist: "周杰伦", Force: true})
			if err == nil && len(rows) == 0 {
				err = fmt.Errorf("no cover candidates")
			}
			return err
		default:
			rows, err := a.lyricSvc.Search(ctx, "qq", lyrics.Query{Title: "晴天", Artist: "周杰伦"})
			if err == nil && len(rows) == 0 {
				err = fmt.Errorf("no lyric candidates")
			}
			return err
		}
	})
	if err != nil {
		t.Fatal("停止速率样本，不继续提速：", err)
	}
	before := len(audit.snapshot())
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err := a.lyricSvc.Search(cancelled, "qq", lyrics.Query{Title: "晴天"}); !errors.Is(err, context.Canceled) || len(audit.snapshot()) != before {
		t.Fatal("cancelled request sent HTTP", err)
	}
	t.Logf("samples=%d cancellation=ok; bounded observations are not an official or long-term safe quota", before)
}

func TestQQDiscoveryTitlesRemainBounded(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        []string
		wantErr     bool
	}{
		{"default", "", []string{"綿雪の夢", "世界中の雨"}, false},
		{"blank", " \n\t ", []string{"綿雪の夢", "世界中の雨"}, false},
		{"single", " 夜に駆ける ", []string{"夜に駆ける"}, false},
		{"two", "夜に駆ける\n爱你 (青春离别版)", []string{"夜に駆ける", "爱你 (青春离别版)"}, false},
		{"deduplicate", "夜に駆ける\n\n夜に駆ける", []string{"夜に駆ける"}, false},
		{"too-many", "one\ntwo\nthree", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := qqDiscoveryTitles(tc.value)
			if (err != nil) != tc.wantErr || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("titles=%v err=%v", got, err)
			}
		})
	}
}

func TestQQRateSamplesStopBeforeIncreasing(t *testing.T) {
	for _, failAt := range []int{0, 1, 2, 3, 8, -1} {
		var rates []int
		calls := 0
		err := sampleQQRates(func(rate int) error { rates = append(rates, rate); return nil }, func(int) error {
			index := calls
			calls++
			if index == failAt {
				return errLiveStopped
			}
			return nil
		})
		if failAt >= 0 {
			if !errors.Is(err, errLiveStopped) || calls != failAt+1 || len(rates) != failAt/3+1 {
				t.Fatal("continued after error", rates, calls, err)
			}
		} else if err != nil || calls != 9 || !reflect.DeepEqual(rates, []int{30, 45, 60}) {
			t.Fatal(rates, calls, err)
		}
	}
}
