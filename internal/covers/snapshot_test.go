package covers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func TestResolveRetainsAutomaticArtworkSnapshot(t *testing.T) {
	svc, _ := resolverService(t)
	ctx := svc.SnapshotContext(context.Background())
	svc.ConfigureSources(nil, nil, nil)
	calls := 0
	svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := []byte(twoSizeIndex)
		if strings.HasSuffix(r.URL.Path, ".png") {
			body = pngBytes(250, 250)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	target := Target{Provider: string(provider.MusicBrainz), Ref: testReleaseMBID}
	got, err := svc.Resolve(ctx, target, Thumbnail)
	if err != nil || got.Outcome != OutcomeOK || calls != 2 {
		t.Fatalf("旧请求丢失自动图片策略: %+v err=%v calls=%d", got, err, calls)
	}
	got, err = svc.Resolve(context.Background(), target, Thumbnail)
	if err != nil || got.Outcome != OutcomeEmpty || calls != 2 {
		t.Fatalf("新请求使用了旧策略: %+v err=%v calls=%d", got, err, calls)
	}
}
