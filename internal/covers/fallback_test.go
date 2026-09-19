package covers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFallbackRetainsErrorsUnlessImageSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/error":
			w.WriteHeader(503)
		case "/image":
			w.Write(jpegBytes())
		case "/index":
			w.Write([]byte(`{"images":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	for _, tc := range []struct {
		name             string
		paths            []string
		success, missing bool
	}{
		{"all missing", []string{"/missing", "/index"}, false, true},
		{"mixed failure", []string{"/error", "/missing", "/index"}, false, false},
		{"recovers", []string{"/error", "/image"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPool(newTestCache(t), 1, "test")
			p.SetIndexParser(func(u string, b []byte) ([]string, bool, error) { return nil, u == srv.URL+"/index", nil })
			urls := []string{}
			for _, path := range tc.paths {
				urls = append(urls, srv.URL+path)
			}
			data, err := p.Fetch(context.Background(), Fetcher{Key: "k", URLs: urls})
			if (err == nil) != tc.success || errors.Is(err, ErrNotFound) != tc.missing {
				t.Fatalf("data=%v err=%v", data, err)
			}
		})
	}
}
