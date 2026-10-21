package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func awaitLifecycle(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("生命周期操作未及时结束")
	}
}

func TestSettingsRetainLyricsServiceForUnfetchedCandidates(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	a.enricher.Pause()
	track, other := lyricTrack(t, a), lyricTrack(t, a)
	oldClient := a.client
	oldService := a.lyricSvc
	var calls atomic.Int32
	oldClient.HTTP.Transport = coverReplayTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[{"mid":"0039MnYb0qxYhV","name":"歌"},{"mid":"0039MnYb0qxYhW","name":"歌"}]}}}}}`
		request, _ := io.ReadAll(r.Body)
		if strings.Contains(string(request), "GetPlayLyricInfo") {
			body = `{"code":0,"req":{"code":0,"data":{"qrc":0,"lyric":"` + base64.StdEncoding.EncodeToString([]byte("[00:01]旧候选的新获取")) + `"}},"plain":{"code":0,"data":{"lyric":""}}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	session, err := a.CreateLyricsSearch(track)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(session)
	rows, err := a.SearchLyrics(session, "qq", lyrics.Query{Title: "歌"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	next := a.settings.Get()
	next.ITunes.DefaultCountry = "us"
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	newSession, err := a.CreateLyricsSearch(track)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(newSession)
	if a.lyricSessions[session].service != oldService || a.lyricSessions[newSession].service != a.lyricSvc || a.lyricSvc == oldService || oldClient.Limiters != a.client.Limiters {
		t.Fatal("旧会话、能力重建或共享配额身份丢失")
	}
	// An ended work round permits a single explicit fetch while retaining the
	// original capability snapshot; do not prefetch both fixtures here.
	sessionState := a.lyricSessions[session]
	sessionState.roundCancel()
	finishLyricsPrefetch(t, a, session)
	doc, err := a.FetchLyrics(session, rows[0].ID)
	if err != nil || doc.Document.Text != "[00:01]旧候选的新获取" || calls.Load() != 2 {
		t.Fatalf("旧候选未使用原服务: doc=%+v err=%v calls=%d", doc, err, calls.Load())
	}
	next.MusicBrainz.Contact = ""
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	if _, err := a.FetchLyrics(session, rows[1].ID); err == nil || !strings.Contains(err.Error(), OutboundDisabledReason) || calls.Load() != 2 {
		t.Fatalf("旧服务绕过当前门禁: %v calls=%d", err, calls.Load())
	}
	if err := a.AcknowledgeLyricsPreview(session, rows[0].ID, doc.Version); err != nil {
		t.Fatal(err)
	}
	rev, _ := a.LyricsRevision(track)
	if _, err := a.AdoptLyrics(session, rows[0].ID, rev); err != nil {
		t.Fatal("禁网拒绝已预览歌词的本地保存", err)
	}
	untouched, err := a.GetTrackDetail(other)
	if err != nil || untouched.Metadata.Lyrics != nil {
		t.Fatal("歌词写入了错误目标", err)
	}
}

func TestSettingsDuringCoverSearchAndDownloadKeepSnapshotAndMoveRoot(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	a.enricher.Pause()
	a.coverSvc.EnableNetworkPolicy(a.checkCoverOutbound)
	svc, pool := a.coverSvc, a.coverSvc.Pool()
	target, _ := a.store.FindOrCreateLocalRelease("target", nil)
	other, _ := a.store.FindOrCreateLocalRelease("other", nil)
	policy, err := provider.NewImagePolicy([]provider.ImageHost{{Host: "only.test"}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	policies, err := provider.NewImagePolicies(map[string]*provider.ImagePolicy{"only": policy})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetImagePolicies(policies)
	started, finishSearch := make(chan struct{}), make(chan struct{})
	var releaseSearch sync.Once
	defer releaseSearch.Do(func() { close(finishSearch) })
	key := domain.CoverCatalogKey{Source: "only", ObjectType: "album", SourceRef: "native"}
	setCoverSources(t, a, &coverSearchProvider{id: "only", search: func(ctx context.Context, q sources.Query) ([]sources.Result, error) {
		close(started)
		select {
		case <-finishSearch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []sources.Result{{Input: &domain.CoverCatalogInput{Key: key, Images: []domain.CoverImage{{SourceImageID: "front", PrimaryType: "Front", Variants: []domain.CoverVariant{{Spec: "250", URL: "https://only.test/image.png"}}}}}}}, nil
	}})
	request := CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: target, Title: "song", Sources: []string{"only"}}
	if err := a.StartCoverSearch(request); err != nil {
		t.Fatal(err)
	}
	defer a.CancelCoverSearch(request.SearchID)
	awaitLifecycle(t, started)
	next := a.settings.Get()
	next.ITunes.DefaultCountry = "us"
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	fresh := request
	fresh.SearchID = uuid.NewString()
	if err := a.StartCoverSearch(fresh); err == nil {
		t.Fatal("新会话使用了旧能力集合")
	}
	releaseSearch.Do(func() { close(finishSearch) })
	a.coverWorkers.Wait()
	results := a.CoverSearchResults(request.SearchID)
	if len(results) != 1 || len(results[0].Cards) != 1 {
		t.Fatal(results)
	}
	variant := results[0].Cards[0].Image.Variants[0]
	imageStarted, finishImage := make(chan struct{}), make(chan struct{})
	var releaseImage sync.Once
	defer releaseImage.Do(func() { close(finishImage) })
	data := coverPNG(t, 250, 250)
	var calls atomic.Int32
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Host != "only.test" {
			t.Errorf("错误图片主机: %s", r.URL)
		}
		close(imageStarted)
		select {
		case <-finishImage:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})
	previewDone := make(chan struct{})
	preview := httptest.NewRecorder()
	go func() {
		defer close(previewDone)
		a.coverAssetHandler().ServeHTTP(preview, httptest.NewRequest("GET", fmt.Sprintf("/cover?variantId=%d&searchId=%s", variant.ID, request.SearchID), nil))
	}()
	awaitLifecycle(t, imageStarted)
	oldRoot := a.paths.CoverDir
	next.Cache.CoverDir = filepath.Join(t.TempDir(), "moved")
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	if a.coverSvc != svc || a.coverSvc.Pool() != pool {
		t.Fatal("目录移动重建了服务或工作池")
	}
	releaseImage.Do(func() { close(finishImage) })
	awaitLifecycle(t, previewDone)
	if preview.Code != 200 || calls.Load() != 1 {
		t.Fatalf("旧策略未保留: status=%d calls=%d body=%s", preview.Code, calls.Load(), preview.Body.String())
	}
	next.MusicBrainz.Contact = ""
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptCoverCandidate(request.SearchID, variant.ID, 0); err != nil {
		t.Fatal("迁移或禁网破坏了旧会话采用", err)
	}
	files, err := a.store.ListReleaseCoverFiles(target)
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	if _, err := os.Stat(filepath.Join(next.Cache.CoverDir, files[0].RelativePath)); err != nil {
		t.Fatal("文件未写入新根", err)
	}
	if _, err := os.Stat(filepath.Join(oldRoot, files[0].RelativePath)); !os.IsNotExist(err) {
		t.Fatal("在旧根写入了迟到文件", err)
	}
	if selection, err := a.store.GetCoverSelection(other); err != nil || selection != nil || calls.Load() != 1 {
		t.Fatal("错误目标或本地采用额外出网", selection, err)
	}
}

func TestSettingsDisableQueuedLyricsWithoutSending(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	a.enricher.Pause()
	session, err := a.CreateLyricsSearch(lyricTrack(t, a))
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(session)
	a.limiters.Configure(provider.QQQuota, 1, time.Second)
	if err := a.limiters.Wait(context.Background(), provider.QQQuota); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	a.client.HTTP.Transport = coverReplayTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("禁网后不应到达 HTTP")
	})
	finished := make(chan struct{})
	var searchErr error
	go func() {
		defer close(finished)
		_, searchErr = a.SearchLyrics(session, "qq", lyrics.Query{Title: "歌"})
	}()
	deadline := time.Now().Add(time.Second)
	for a.interactive.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("请求未进入等待")
		}
		time.Sleep(time.Millisecond)
	}
	next := a.settings.Get()
	next.MusicBrainz.Contact = ""
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	awaitLifecycle(t, finished)
	if searchErr == nil || !strings.Contains(searchErr.Error(), OutboundDisabledReason) || calls.Load() != 0 {
		t.Fatalf("排队请求未复查新门禁: %v calls=%d", searchErr, calls.Load())
	}
}

func TestShutdownDrainsImageDownloadBeforeStoreClose(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	key := domain.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: "jp:123"}
	if _, err := a.store.SaveCoverCatalog(key, []domain.CoverImage{{SourceImageID: "front", PrimaryType: "Front", Variants: []domain.CoverVariant{{Spec: "250", URL: "https://is1.mzstatic.com/image.png"}}}}); err != nil {
		t.Fatal(err)
	}
	catalog, err := a.store.GetCoverCatalog(key)
	if err != nil {
		t.Fatal(err)
	}
	variant := catalog.Images[0].Variants[0]
	started, cancelled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(finish) })
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		<-finish
		return nil, r.Context().Err()
	})
	previewDone := make(chan struct{})
	preview := httptest.NewRecorder()
	go func() {
		defer close(previewDone)
		a.coverAssetHandler().ServeHTTP(preview, httptest.NewRequest("GET", fmt.Sprintf("/cover?variantId=%d", variant.ID), nil))
	}()
	awaitLifecycle(t, started)
	closed := make(chan struct{})
	go func() { a.shutdown(context.Background()); close(closed) }()
	awaitLifecycle(t, cancelled)
	if _, err := a.store.GetCoverVariant(variant.ID); err != nil {
		t.Fatal("图片池未退出 Store 已关闭", err)
	}
	select {
	case <-closed:
		t.Fatal("未等待取消后的传输退出")
	default:
	}
	release.Do(func() { close(finish) })
	awaitLifecycle(t, closed)
	awaitLifecycle(t, previewDone)
	if preview.Code == 200 {
		t.Fatal("取消的图片返回成功")
	}
}

func TestShutdownDrainsLyricsAndCoverSearchBeforeStoreClose(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	track := lyricTrack(t, a)
	releaseID, _ := a.store.FindOrCreateLocalRelease("target", nil)
	started, cancelled, finish := make(chan struct{}, 2), make(chan struct{}, 2), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(finish) })
	a.client.HTTP.Transport = coverReplayTransport(func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-r.Context().Done()
		cancelled <- struct{}{}
		<-finish
		return nil, r.Context().Err()
	})
	setCoverSources(t, a, &coverSearchProvider{id: "only", search: func(ctx context.Context, _ sources.Query) ([]sources.Result, error) {
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		<-finish
		return []sources.Result{coverResult("jp:123", "https://is1.mzstatic.com/late.png", "album", "song")}, nil
	}})
	session, err := a.CreateLyricsSearch(track)
	if err != nil {
		t.Fatal(err)
	}
	lyricDone := make(chan error, 1)
	go func() { _, err := a.SearchLyrics(session, "qq", lyrics.Query{Title: "歌"}); lyricDone <- err }()
	if err := a.StartCoverSearch(CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: releaseID, Sources: []string{"only"}}); err != nil {
		t.Fatal(err)
	}
	awaitLifecycle(t, started)
	awaitLifecycle(t, started)
	closed := make(chan struct{})
	go func() { a.shutdown(context.Background()); close(closed) }()
	awaitLifecycle(t, cancelled)
	awaitLifecycle(t, cancelled)
	select {
	case <-closed:
		t.Fatal("未等待业务退出即关闭")
	default:
	}
	if _, err := a.store.TrackRevision(track); err != nil {
		t.Fatal("业务未结束已关闭 Store", err)
	}
	if _, err := a.CreateLyricsSearch(track); err == nil {
		t.Fatal("关闭期间接受了新会话")
	}
	release.Do(func() { close(finish) })
	awaitLifecycle(t, closed)
	if err := <-lyricDone; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := a.store.TrackRevision(track); err == nil {
		t.Fatal("Store 未关闭")
	}
}

func TestShutdownWaitsForCoverAdoptionAfterDownload(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	target, _ := a.store.FindOrCreateLocalRelease("target", nil)
	setCoverSources(t, a, &coverSearchProvider{id: "only", search: func(context.Context, sources.Query) ([]sources.Result, error) {
		return []sources.Result{coverResult("jp:123", "https://is1.mzstatic.com/image/100x100bb.jpg", "album", "song")}, nil
	}})
	request := CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: target, Sources: []string{"only"}}
	if err := a.StartCoverSearch(request); err != nil {
		t.Fatal(err)
	}
	a.coverWorkers.Wait()
	variant := a.CoverSearchResults(request.SearchID)[0].Cards[0].Image.Variants[0]
	if _, _, err := a.coverSvc.Library().SaveVariant(0, variant.ID, coverPNG(t, 250, 250), false, 0); err != nil {
		t.Fatal(err)
	}
	emitting, finish := make(chan struct{}), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(finish) })
	a.emit = func(name string, _ ...any) {
		if name == EventCoverChanged {
			close(emitting)
			<-finish
		}
	}
	adoptDone := make(chan error, 1)
	go func() { _, err := a.AdoptCoverCandidate(request.SearchID, variant.ID, 0); adoptDone <- err }()
	awaitLifecycle(t, emitting)
	closed := make(chan struct{})
	go func() { a.shutdown(context.Background()); close(closed) }()
	awaitLifecycle(t, a.coverSessions[request.SearchID].ctx.Done())
	select {
	case <-closed:
		t.Fatal("关闭只等待下载而没有等待采用")
	default:
	}
	if selection, err := a.store.GetCoverSelection(target); err != nil || selection == nil {
		t.Fatal("采用完成前 Store 已关闭", err)
	}
	release.Do(func() { close(finish) })
	awaitLifecycle(t, closed)
	if err := <-adoptDone; err != nil {
		t.Fatal(err)
	}
}
