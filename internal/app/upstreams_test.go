package app

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
)

func TestProductionAssemblyListsAndFailedBuildDoesNotPublish(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	ids := []string{}
	for _, p := range a.registry.List() {
		ids = append(ids, string(p.ID()))
	}
	if !reflect.DeepEqual(ids, []string{"musicbrainz", "itunes", "qq"}) {
		t.Fatal(ids)
	}
	if !reflect.DeepEqual(a.ListLyricsSources(), []lyrics.Source{{ID: "163", Name: "网易云音乐"}, {ID: "qq", Name: "QQ 音乐"}, {ID: "kugou", Name: "酷狗音乐"}}) {
		t.Fatal(a.ListLyricsSources())
	}
	if !reflect.DeepEqual(a.ListCoverSources(), []CoverSourceInfo{{"caa", "CAA", false}, {"itunes", "iTunes", true}, {"qq", "QQ", false}, {"163", "网易云", false}}) {
		t.Fatal(a.ListCoverSources())
	}
	beforeClient, beforeRegistry, beforeLyrics, beforeCovers := a.client, a.registry, a.lyricSvc, a.coverSources
	svc := a.coverSvc
	a.coverSvc = nil
	if err := a.rebuildProviders(a.settings.Get()); err == nil {
		t.Fatal("nil catalog dependency accepted")
	}
	a.coverSvc = svc
	if a.client != beforeClient || a.registry != beforeRegistry || a.lyricSvc != beforeLyrics || a.coverSources != beforeCovers {
		t.Fatal("failed assembly published partial services")
	}
	cur := a.settings.Get()
	cur.ITunes.DefaultCountry = "jp"
	a.mu.Lock()
	err := a.rebuildProviders(cur)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	p, _ := a.registry.Get(provider.ITunes)
	if p.(*itunes.Provider).ResolveCountry("", "") != "jp" || a.client.Limiters != beforeClient.Limiters {
		t.Fatal("country or long-lived quota lost")
	}
}

func TestSaveSettingsAssemblyFailureHasNoSideEffects(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	svc := a.coverSvc
	defer svc.Close()
	before := a.settings.Get()
	client, metadata, lyricSvc, coverSources := a.client, a.registry, a.lyricSvc, a.coverSources
	next := before
	next.ITunes.DefaultCountry = "us"
	next.Cache.CoverDir = t.TempDir() + "/must-not-create"
	a.coverSvc = nil
	err := a.SaveSettings(next)
	a.coverSvc = svc
	if err == nil {
		t.Fatal("缺少目录服务的装配没有失败")
	}
	if !reflect.DeepEqual(a.settings.Get(), before) || a.client != client || a.registry != metadata || a.lyricSvc != lyricSvc || a.coverSources != coverSources {
		t.Fatal("失败后发布或保存了半套配置")
	}
	if _, err := os.Stat(next.Cache.CoverDir); !os.IsNotExist(err) {
		t.Fatal("装配失败仍产生目录副作用", err)
	}
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	if a.client == client || a.registry == metadata || a.lyricSvc == lyricSvc || a.coverSources == coverSources || a.client.Limiters != client.Limiters {
		t.Fatal("有效保存没有整套更新或替换了限流器")
	}
	p, _ := a.registry.Get(provider.ITunes)
	if p.(*itunes.Provider).ResolveCountry("", "") != "us" {
		t.Fatal("新配置未生效")
	}
}

func TestAssemblyDoesNotConfigureOrResetQuota(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	a.limiters.Configure(provider.QQQuota, 1, time.Hour)
	if err := a.limiters.Wait(context.Background(), provider.QQQuota); err != nil {
		t.Fatal(err)
	}
	if _, err := assembleUpstreams(config.Defaults(), a.limiters, a.store, a.coverSvc, "test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := a.limiters.Wait(ctx, provider.QQQuota); err == nil {
		t.Fatal("construction reconfigured quota")
	}
}
