package app

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/store"
)

func TestProductionCoverHandlerQualityCacheAndGate(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	svc, err := newCoverService(a.store, a.paths, a.settings.Get(), "test")
	if err != nil {
		t.Fatal(err)
	}
	a.coverSvc = svc
	defer svc.Close()
	svc.ConfigureNetwork(func() error {
		if gate := a.CheckOutbound(); !gate.Allowed {
			return &covers.OutboundError{Reason: gate.Reason}
		}
		return nil
	}, "test")
	ref, source := "2659e6f6-ce21-4198-92e5-4878f5e7e3c0", "musicbrainz"
	id, err := a.store.UpsertRelease(&store.Release{Title: "album", Provider: &source, ProviderRef: &ref, TracklistState: store.TracklistNone})
	if err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var mu sync.Mutex
	calls := map[string]int{}
	http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		data := []byte(`{"images":[{"id":"1","front":true,"types":["Front"],"image":"https://archive.org/original.png","thumbnails":{"250":"https://archive.org/250.png","1200":"https://archive.org/1200.png"}}]}`)
		if strings.HasSuffix(r.URL.Path, ".png") {
			size := 250
			if strings.Contains(r.URL.Path, "1200") {
				size = 1200
			}
			data = coverPNG(t, size, size)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
	})
	request := func(usage string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		a.coverAssetHandler().ServeHTTP(r, httptest.NewRequest("GET", "/cover?releaseId="+strconv.FormatInt(id, 10)+"&usage="+usage, nil))
		return r
	}
	for _, usage := range []string{"thumbnail", "detail", "thumbnail"} {
		r := request(usage)
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
		info, err := covers.InspectImage(r.Body.Bytes())
		want := 250
		if usage == "detail" {
			want = 1200
		}
		if err != nil || info.Width != want {
			t.Fatalf("%+v %v", info, err)
		}
	}
	if calls["/release/"+ref] != 1 || calls["/250.png"] != 1 || calls["/1200.png"] != 1 {
		t.Fatal(calls)
	}
	if err := a.ClearCache(); err != nil {
		t.Fatal(err)
	}
	if r := request("detail"); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if calls["/release/"+ref] != 1 || calls["/1200.png"] != 2 {
		t.Fatal("清理后没有复用目录", calls)
	}
	if err := a.ClearCache(); err != nil {
		t.Fatal(err)
	}
	settings := a.settings.Get()
	settings.MusicBrainz.Contact = ""
	if err := a.settings.Save(settings); err != nil {
		t.Fatal(err)
	}
	before := fmt.Sprint(calls)
	if r := request("detail"); r.Code != 403 {
		t.Fatalf("未禁用自动图片: %d", r.Code)
	}
	if fmt.Sprint(calls) != before {
		t.Fatal("禁用后仍出网")
	}
	if _, err := svc.Library().ImportLocal(id, coverPNG(t, 900, 900), 0); err != nil {
		t.Fatal(err)
	}
	if r := request("detail"); r.Code != 200 {
		t.Fatal("本地图被网络 gate 阻止")
	}
}
