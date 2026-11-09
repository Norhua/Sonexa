package app

import (
	"context"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

func TestPlaylistAssemblyReusesQQAndKeepsSpotifySeparate(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	sources := a.playlistSources.List()
	if len(sources) != 2 || sources[0].ID != "qq" || !sources[0].Available || sources[1].ID != "spotify" {
		t.Fatal(sources)
	}
	// The test executable has no application resource bundle. Lack of helper
	// cannot stop app/store initialization or hide the unavailable source.
	if sources[1].Available || sources[1].Reason == "" {
		t.Fatal("missing helper not reported", sources[1])
	}
	qq, _ := a.playlistSources.Get("qq")
	metadata, _ := a.registry.Get("qq")
	if any(qq) != any(metadata) {
		t.Fatal("QQ capability did not reuse instance")
	}
	if _, err := a.registry.Get("spotify"); err == nil {
		t.Fatal("Spotify registered as metadata")
	}
	if _, err := a.coverSources.Get("spotify"); err == nil {
		t.Fatal("Spotify registered as covers")
	}
	if _, err := a.lyricSvc.Sources.Get("spotify"); err == nil {
		t.Fatal("Spotify registered as lyrics")
	}
	old := a.playlistSources
	svc := a.coverSvc
	a.coverSvc = nil
	if err := a.rebuildProviders(a.settings.Get()); err == nil {
		t.Fatal("invalid assembly accepted")
	}
	a.coverSvc = svc
	if a.playlistSources != old {
		t.Fatal("failed assembly published playlist sources")
	}
}

func TestSpotifyQuotaSurvivesReassembly(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	a.limiters.Configure(provider.SpotifyQuota, 1, time.Hour)
	if err := a.limiters.Wait(context.Background(), provider.SpotifyQuota); err != nil {
		t.Fatal(err)
	}
	if _, err := assembleUpstreams(a.settings.Get(), a.limiters, a.store, a.coverSvc, "test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := a.limiters.Wait(ctx, provider.SpotifyQuota); err == nil {
		t.Fatal("assembly reset Spotify quota")
	}
}
