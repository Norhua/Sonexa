package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func TestInvalidQQSettingsDoNotMovePersistPublishOrRefill(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	a.enricher.Pause()
	id, err := a.store.FindOrCreateLocalRelease("album", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.coverSvc.Library().ImportLocal(id, coverPNG(t, 250, 250), 0); err != nil {
		t.Fatal(err)
	}
	before := a.settings.Get()
	beforeDisk, err := os.ReadFile(a.settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	root, client, registry, lyricSvc, coverSources := a.paths.CoverDir, a.client, a.registry, a.lyricSvc, a.coverSources
	a.limiters.Configure(provider.QQQuota, 1, time.Hour)
	if err := a.limiters.Wait(context.Background(), provider.QQQuota); err != nil {
		t.Fatal(err)
	}
	for _, rate := range []float64{0, -1, 0.5, 60.1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		next := before
		next.QQMusic.RatePerMin = rate
		next.Cache.CoverDir = filepath.Join(t.TempDir(), "must-not-exist")
		if err := a.SaveSettings(next); err == nil {
			t.Fatalf("accepted invalid rate %g", rate)
		}
		afterDisk, err := os.ReadFile(a.settings.Path())
		if err != nil || !bytes.Equal(beforeDisk, afterDisk) || !reflect.DeepEqual(before, a.settings.Get()) {
			t.Fatal("invalid save persisted", err)
		}
		if a.paths.CoverDir != root || a.client != client || a.registry != registry || a.lyricSvc != lyricSvc || a.coverSources != coverSources {
			t.Fatal("invalid save published")
		}
		if _, err := os.Stat(next.Cache.CoverDir); !os.IsNotExist(err) {
			t.Fatal("invalid save moved directory", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = a.limiters.Wait(ctx, provider.QQQuota)
		cancel()
		if err == nil {
			t.Fatal("invalid save changed exhausted quota")
		}
		files, err := a.store.ListReleaseCoverFiles(id)
		if err != nil || len(files) != 1 {
			t.Fatal(files, err)
		}
		if _, err := a.coverSvc.Library().Read(files[0]); err != nil {
			t.Fatal("invalid save damaged old file", err)
		}
	}
}

func TestQQSaveWhileQueuedKeepsReservationAndNewSessionQuota(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	a.enricher.Pause()
	next := a.settings.Get()
	next.QQMusic.RatePerMin = 30
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	arrived := make(chan time.Time, 3)
	transport := appCoverTransport(func(r *http.Request) (*http.Response, error) {
		arrived <- time.Now()
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[]}}}}}`))}, nil
	})
	a.client.HTTP.Transport = transport
	limiters := a.limiters
	if err := limiters.Wait(context.Background(), provider.QQQuota); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	id := lyricTrack(t, a)
	oldSession, err := a.CreateLyricsSearch(id)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(oldSession)
	finished := make(chan error, 2)
	go func() { _, err := a.SearchLyrics(oldSession, "qq", lyrics.Query{Title: "old"}); finished <- err }()
	// The provider-level test observes the exact reservation/queue state. Here a
	// bounded delay lets the real binding reserve before SaveSettings is called.
	time.Sleep(100 * time.Millisecond)
	next.QQMusic.RatePerMin = 60
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	if a.limiters != limiters || a.client.Limiters != limiters {
		t.Fatal("saving replaced limiters")
	}
	a.client.HTTP.Transport = transport
	newSession, err := a.CreateLyricsSearch(id)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(newSession)
	go func() { _, err := a.SearchLyrics(newSession, "qq", lyrics.Query{Title: "new"}); finished <- err }()
	select {
	case first := <-arrived:
		if first.Sub(started) < 1800*time.Millisecond {
			t.Fatal("saving accelerated an old reservation or refilled new session", first.Sub(started))
		}
	case <-time.After(4 * time.Second):
		t.Fatal("queued request stalled")
	}
	for range 2 {
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("queue did not drain")
		}
	}
	cancelSession, err := a.CreateLyricsSearch(id)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, err := a.SearchLyrics(cancelSession, "qq", lyrics.Query{Title: "cancel"}); finished <- err }()
	a.CancelLyricsSearch(cancelSession)
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled queue succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not end waiting")
	}
	if len(arrived) != 1 {
		t.Fatal("cancelled request sent HTTP", len(arrived))
	}
}

func TestQQEffectiveStartupAndSavedRatesReachProductionAssembly(t *testing.T) {
	for _, tc := range []struct{ raw, want float64 }{{0, 45}, {90, 60}, {0.5, 1}, {-1, 45}, {30, 30}, {20.5, 20.5}, {1, 1}, {60, 60}} {
		t.Run(fmt.Sprint(tc.raw), func(t *testing.T) {
			a, done := newTestApp(t)
			defer done()
			defer a.coverSvc.Close()
			a.enricher.Pause()
			if err := os.WriteFile(a.settings.Path(), []byte(fmt.Sprintf(`{"qqmusic":{"ratePerMin":%g},"musicbrainz":{"contact":"test@example.com"}}`, tc.raw)), 0600); err != nil {
				t.Fatal(err)
			}
			settings, err := config.NewStore(a.paths.AppDir)
			if err != nil {
				t.Fatal(err)
			}
			a.settings = settings
			if err := a.rebuildProviders(settings.Get()); err != nil {
				t.Fatal(err)
			}
			got, err := a.GetSettings()
			if err != nil || got.QQMusic.RatePerMin != tc.want {
				t.Fatal(got.QQMusic, err)
			}
			p, _ := a.registry.Get(provider.QQMusic)
			n, per := p.RateLimit()
			if n != 1 || per != time.Duration(float64(time.Minute)/tc.want) {
				t.Fatal("adapter rate differs", n, per)
			}
			if err := a.SaveSettings(got); err != nil {
				t.Fatal(err)
			}
			reopened, err := config.NewStore(a.paths.AppDir)
			if err != nil || reopened.Get().QQMusic.RatePerMin != tc.want {
				t.Fatal("saved value differs", err)
			}
			// The configured bucket, not merely the adapter description, must use this rate.
			if err := a.limiters.Wait(context.Background(), provider.QQQuota); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), per/2)
			defer cancel()
			if err := a.limiters.Wait(ctx, provider.QQQuota); err == nil {
				t.Fatal("runtime quota faster than effective value")
			}
		})
	}
}
