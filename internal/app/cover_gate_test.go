package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestCoverOutboundGateAcrossBindingsAndAssets(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	svc, err := newCoverService(a.store, a.paths, a.settings.Get(), "test")
	if err != nil {
		t.Fatal(err)
	}
	a.coverSvc = svc
	svc.EnableNetworkPolicy(a.checkCoverOutbound)
	defer svc.Close()
	ref, source := uuid.NewString(), "musicbrainz"
	id, err := a.store.UpsertRelease(&store.Release{Title: "album", Provider: &source, ProviderRef: &ref, TracklistState: store.TracklistNone})
	if err != nil {
		t.Fatal(err)
	}
	key := store.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: "jp:12"}
	if _, err := a.store.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "art", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "600", URL: "https://is1.mzstatic.com/600.png"}}}}); err != nil {
		t.Fatal(err)
	}
	catalog, err := a.store.GetCoverCatalog(key)
	if err != nil {
		t.Fatal(err)
	}
	variant := catalog.Images[0].Variants[0]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.coverSessions = map[string]*coverSession{"existing": {ctx: ctx, cancel: cancel, releaseID: id, variants: map[int64]bool{variant.ID: true}}}

	settings := a.settings.Get()
	settings.MusicBrainz.Contact = ""
	if err := a.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var calls atomic.Int32
	http.DefaultTransport = appCoverTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"images":[]}`))}, nil
	})
	request := func(raw string, status int) {
		t.Helper()
		r := httptest.NewRecorder()
		a.coverAssetHandler().ServeHTTP(r, httptest.NewRequest("GET", raw, nil))
		if r.Code != status || (status == 403 && !strings.Contains(r.Body.String(), OutboundDisabledReason)) {
			t.Fatalf("%s: %d %s", raw, r.Code, r.Body.String())
		}
	}
	for _, source := range []string{"caa", "itunes", "qq", "163"} {
		err := a.StartCoverSearch(CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: id, Title: "song", Sources: []string{source}})
		if err == nil || !strings.Contains(err.Error(), OutboundDisabledReason) {
			t.Fatalf("%s 未返回统一禁用原因: %v", source, err)
		}
	}
	request(fmt.Sprintf("/cover?releaseId=%d&usage=detail", id), 403) // 未查目录
	caaKey := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: ref}
	if got, err := a.store.GetCoverCatalog(caaKey); err != nil || got != nil {
		t.Fatalf("出网禁用写成查询故障/永久空: %+v %v", got, err)
	}
	if _, err := a.store.SaveCoverCatalog(caaKey, []store.CoverImage{{SourceImageID: "front", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "1200", URL: "https://archive.org/front.png"}}}}); err != nil {
		t.Fatal(err)
	}
	request(fmt.Sprintf("/cover?releaseId=%d&usage=detail", id), 403) // 有目录缺图片
	request(fmt.Sprintf("/cover?variantId=%d", variant.ID), 403)      // 手动预览
	if _, err := a.AdoptCoverCandidate("existing", variant.ID, 0); err == nil || !strings.Contains(err.Error(), OutboundDisabledReason) {
		t.Fatalf("已存在搜索会话绕过采用 gate: %v", err)
	}
	if got, err := a.store.GetCoverVariant(variant.ID); err != nil || got.State != variant.State {
		t.Fatalf("出网禁用不应改变规格可用状态: %+v %v", got, err)
	}

	// 同一真实预取绑定在缺图时记录禁用状态，有本地图时仍可完成。
	target := covers.Target{ReleaseID: id}
	prefetch := func(revision int64, want string) {
		t.Helper()
		key := target.Key(covers.Detail, revision)
		if err := a.PrefetchCovers([]covers.Ref{{Key: key, Target: target, Usage: covers.Detail}}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for {
			got := a.CoverTargetOutcomes([]CoverTargetQuery{{ReleaseID: id, Usage: "detail", Revision: revision}})
			if got[0] == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("预取状态=%v，期望 %s", got, want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	prefetch(0, "disabled")
	// 已下载预览可读取、可采用，不因关闭网络而被拒绝。
	if _, _, err := svc.Library().SaveVariant(0, variant.ID, coverPNG(t, 600, 600), false, 0); err != nil {
		t.Fatal(err)
	}
	request(fmt.Sprintf("/cover?variantId=%d", variant.ID), 200)
	result, err := a.AdoptCoverCandidate("existing", variant.ID, 0)
	if err != nil || result.Revision != 1 {
		t.Fatalf("采用缓存失败: %+v %v", result, err)
	}
	request(fmt.Sprintf("/cover?releaseId=%d&usage=detail&revision=1", id), 200)
	prefetch(1, "ok")

	path := filepath.Join(t.TempDir(), "local.png")
	if err := os.WriteFile(path, coverPNG(t, 900, 700), 0o600); err != nil {
		t.Fatal(err)
	}
	a.pickCoverFile = func(context.Context, wruntime.OpenDialogOptions) (string, error) { return path, nil }
	if _, err := a.PickAndAdoptCover(id, 1); err != nil {
		t.Fatalf("本地文件导入被 gate 阻止: %v", err)
	}
	request(fmt.Sprintf("/cover?releaseId=%d&usage=detail&revision=2", id), 200)
	if calls.Load() != 0 {
		t.Fatalf("禁用后实际出网 %d 次", calls.Load())
	}
}

func TestCoverSearchRechecksGateAfterStart(t *testing.T) {
	for _, source := range []string{"caa", "itunes", "qq", "163"} {
		t.Run(source, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			if err := a.coverSvc.AttachLibrary(a.store); err != nil {
				t.Fatal(err)
			}
			defer a.coverSvc.Close()
			a.coverSvc.EnableNetworkPolicy(a.checkCoverOutbound)
			id, err := a.store.FindOrCreateLocalRelease("album", nil)
			if err != nil {
				t.Fatal(err)
			}
			old := http.DefaultTransport
			defer func() { http.DefaultTransport = old }()
			var calls atomic.Int32
			http.DefaultTransport = appCoverTransport(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				// 在首次响应时撤销出网许可，重定向不得再发送请求。
				settings := a.settings.Get()
				settings.MusicBrainz.Contact = ""
				if err := a.settings.Save(settings); err != nil {
					return nil, err
				}
				header := make(http.Header)
				header.Set("Location", "/redirected")
				return &http.Response{StatusCode: 302, Header: header, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			events := make(chan CoverSearchEvent, 1)
			a.emit = func(name string, values ...any) {
				if name == EventCoverResults {
					events <- values[0].(CoverSearchEvent)
				}
			}
			request := CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: id, Title: "song", Sources: []string{source}}
			if err := a.StartCoverSearch(request); err != nil {
				t.Fatal(err)
			}
			defer a.CancelCoverSearch(request.SearchID)
			select {
			case event := <-events:
				if event.State != "error" || !strings.Contains(event.Error, OutboundDisabledReason) {
					t.Fatal(event)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("搜索未返回禁用原因")
			}
			a.coverWorkers.Wait()
			if calls.Load() != 1 {
				t.Fatalf("重定向绕过 gate: %d", calls.Load())
			}
		})
	}
}
