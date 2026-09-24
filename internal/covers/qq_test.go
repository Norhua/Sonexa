package covers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestQQResolverFallsBackWhenLargeSizeMissing(t *testing.T) {
	svc, _ := resolverService(t)
	requested := []string{}
	svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.Path)
		if strings.Contains(r.URL.Path, "T002R1200x1200") || strings.Contains(r.URL.Path, "T002R800x800") {
			return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(pngBytes(500, 500))))}, nil
	})
	got, err := svc.Resolve(context.Background(), Target{Provider: "qq", Ref: "000BDJvT2aWQYZ"}, Detail)
	if err != nil || got.File == nil || got.File.Width != 500 {
		t.Fatalf("%+v %v (%v)", got, err, requested)
	}
	if !strings.Contains(requested[0], "T002R1200x1200M000000BDJvT2aWQYZ") {
		t.Fatalf("详情应先尝试最大档: %v", requested)
	}
	if got, err := svc.Resolve(context.Background(), Target{Provider: "qq", Ref: "bad/ref"}, Thumbnail); err != nil || got.File != nil {
		t.Fatalf("无效 MID 不应出网: %+v %v", got, err)
	}
}
