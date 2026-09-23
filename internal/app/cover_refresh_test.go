package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func TestForceTracklistBypassesExistingResponse(t *testing.T) {
	for _, source := range []string{"musicbrainz", "itunes"} {
		t.Run(source, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			requests := 0
			a.client.HTTP.Transport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				body := fmt.Sprintf(`{"id":"r","title":"version-%d","media":[{"position":1,"track-count":1,"tracks":[{"id":"t","position":1,"number":"1","title":"song"}]}]}`, requests)
				if source == "itunes" {
					body = fmt.Sprintf(`{"resultCount":2,"results":[{"wrapperType":"collection","collectionId":1,"collectionName":"version-%d","trackCount":1},{"wrapperType":"track","kind":"song","trackId":2,"collectionId":1,"trackName":"song","trackNumber":1,"discNumber":1}]}`, requests)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			ref := "r"
			if source == "itunes" {
				ref = "jp:1"
			}
			p, err := a.registry.Get(provider.ProviderID(source))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.FetchDetail(context.Background(), provider.Ref{ID: ref}); err != nil {
				t.Fatal(err)
			}
			if _, err := p.FetchDetail(context.Background(), provider.Ref{ID: ref}); err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatal("普通缓存未命中")
			}
			if err := a.FetchTracklist(source, ref, true); err != nil {
				t.Fatal(err)
			}
			if requests != 2 {
				t.Fatal("force 未穿透缓存")
			}
			release, err := a.store.GetReleaseByRef(source, ref)
			if err != nil || release == nil || release.Title != "version-2" {
				t.Fatalf("未更新内容: %+v %v", release, err)
			}
		})
	}
}
