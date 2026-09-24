package covers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestResolverGroupFallbackPersistsActualRelease(t *testing.T) {
	svc, st := resolverService(t)
	group := "e8ec3011-cd49-4558-9d11-01f7fe9b552f"
	calls := map[string]int{}
	svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Path]++
		status, body := 200, []byte(`{"images":[]}`)
		if strings.Contains(r.URL.Path, "release-group") {
			body = []byte(`{"release":"https://musicbrainz.org/release/actual","images":[{"id":"123","types":["Medium"],"thumbnails":{"250":"https://archive.org/250.png"}}]}`)
		}
		if r.URL.Path == "/250.png" {
			body = pngBytes(250, 250)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	target := Target{Provider: "musicbrainz", Ref: testReleaseMBID, GroupRef: group}
	for range 2 {
		got, err := svc.Resolve(context.Background(), target, Thumbnail)
		if err != nil || got.File.Width != 250 {
			t.Fatalf("%+v %v", got, err)
		}
	}
	if calls["/release/"+testReleaseMBID] != 1 || calls["/release-group/"+group] != 1 || calls["/250.png"] != 1 {
		t.Fatal(calls)
	}
	catalog, err := st.GetCoverCatalog(store.CoverCatalogKey{Source: "caa", ObjectType: "release-group", SourceRef: group})
	if err != nil || catalog.ActualReleaseURL != "https://musicbrainz.org/release/actual" {
		t.Fatal("实际来源未保留")
	}
}

func TestTargetOutcomeRevisionAndLegacyPathRejection(t *testing.T) {
	svc, _ := resolverService(t)
	svc.SetITunesDetailLoader(func(_ context.Context, ref string) (*provider.Detail, error) {
		return &provider.Detail{Provider: provider.ITunes, ReleaseRef: &ref}, nil
	})
	for _, raw := range []string{"/cover?adopted=/etc/passwd", "/cover?provider=itunes&ref=jp:12&usage=detail&revision=7"} {
		r := httptest.NewRecorder()
		svc.Handler().ServeHTTP(r, httptest.NewRequest("GET", raw, nil))
		if r.Code != 404 {
			t.Fatalf("%s=%d", raw, r.Code)
		}
	}
	if got := svc.Outcomes([]string{(Target{Provider: "itunes", Ref: "jp:12"}).Key(Detail, 7)}); got[0] != OutcomeEmpty {
		t.Fatal(got)
	}
	if got := svc.Outcomes([]string{(Target{Provider: "itunes", Ref: "jp:12"}).Key(Detail, 0)}); got[0] != OutcomeUnknown {
		t.Fatal("版本间结果串台")
	}
}

func TestAllConsumersCancelAndCloseDrainsDetachedWork(t *testing.T) {
	pool := NewPool(newTestCache(t), 1, "test")
	ctx, cancel := context.WithCancel(context.Background())
	started, finished := make(chan struct{}), make(chan struct{})
	go func() {
		_, _ = pool.Shared(ctx, "x", func(ctx context.Context) (any, error) {
			close(started)
			<-ctx.Done()
			defer close(finished)
			return nil, ctx.Err()
		})
	}()
	<-started
	cancel()
	pool.Close()
	select {
	case <-finished:
	default:
		t.Fatal("关闭未等待已取消工作")
	}
}

func TestPrefetchCancellationDoesNotCancelHandlerConsumer(t *testing.T) {
	svc, _ := resolverService(t)
	started, finish := make(chan struct{}), make(chan struct{})
	var once sync.Once
	svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/release/") {
			once.Do(func() { close(started) })
			select {
			case <-finish:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		data := []byte(twoSizeIndex)
		if strings.HasSuffix(r.URL.Path, ".png") {
			data = pngBytes(250, 250)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
	})
	target := Target{Provider: "musicbrainz", Ref: testReleaseMBID}
	key := target.Key(Thumbnail, 0)
	svc.Prefetch(context.Background(), []Ref{{Key: key, Target: target, Usage: Thumbnail}})
	<-started
	done := make(chan error, 1)
	go func() { _, err := svc.Resolve(context.Background(), target, Thumbnail); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		svc.pool.mu.Lock()
		job := svc.pool.jobs["catalog:release:"+testReleaseMBID]
		n := 0
		if job != nil {
			n = job.waiters
		}
		svc.pool.mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("第二消费者未加入")
		}
		time.Sleep(time.Millisecond)
	}
	svc.CancelOutOfView(nil)
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(fmt.Errorf("取消预取杀死直接请求: %w", err))
	}
}
