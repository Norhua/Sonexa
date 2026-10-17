package app

import (
	"bytes"
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/provider"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/domain"
)

func TestCoverOnlyProviderExistingCatalogNotSavedAgain(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	id, err := a.store.FindOrCreateLocalRelease("target", nil)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.CoverCatalogKey{Source: "only", ObjectType: "album", SourceRef: "native"}
	catalogID, err := a.store.SaveCoverCatalog(key, []domain.CoverImage{{SourceImageID: "front", PrimaryType: "Front", Variants: []domain.CoverVariant{{Spec: "250", URL: "https://example.test/image"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetCoverCatalogRelease(catalogID, "https://example.test/actual"); err != nil {
		t.Fatal(err)
	}
	policy, err := provider.NewImagePolicy([]provider.ImageHost{{Host: "example.test"}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	policies, err := provider.NewImagePolicies(map[string]*provider.ImagePolicy{"only": policy})
	if err != nil {
		t.Fatal(err)
	}
	a.coverSvc.SetImagePolicies(policies)
	before, _ := a.store.GetCoverCatalog(key)
	setCoverSources(t, a, &coverSearchProvider{id: "only", search: func(context.Context, sources.Query) ([]sources.Result, error) {
		return []sources.Result{{Existing: &key}}, nil
	}})
	if _, err := a.registry.Get("only"); err == nil {
		t.Fatal("single cover source leaked into metadata")
	}
	request := CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: id, Title: "song", Sources: []string{"only"}}
	if err := a.StartCoverSearch(request); err != nil {
		t.Fatal(err)
	}
	a.coverWorkers.Wait()
	defer a.CancelCoverSearch(request.SearchID)
	results := a.CoverSearchResults(request.SearchID)
	if len(results) != 1 || len(results[0].Cards) != 1 {
		t.Fatal(results)
	}
	after, _ := a.store.GetCoverCatalog(key)
	if before.Version != after.Version || after.ActualReleaseURL != "https://example.test/actual" {
		t.Fatal("existing catalog overwritten", before, after)
	}
	variant := after.Images[0].Variants[0]
	calls := 0
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	data := coverPNG(t, 250, 250)
	http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "example.test" {
			t.Error(r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}, nil
	})
	// A session preview retains its own policy even after new work uses another snapshot.
	replacement, _ := provider.NewImagePolicy([]provider.ImageHost{{Host: "new.test"}}, 2)
	newPolicies, _ := provider.NewImagePolicies(map[string]*provider.ImagePolicy{"only": replacement})
	a.coverSvc.SetImagePolicies(newPolicies)
	preview := httptest.NewRecorder()
	a.coverAssetHandler().ServeHTTP(preview, httptest.NewRequest("GET", fmt.Sprintf("/cover?variantId=%d&searchId=%s", variant.ID, request.SearchID), nil))
	if preview.Code != 200 || calls != 1 {
		t.Fatalf("preview=%d calls=%d body=%s", preview.Code, calls, preview.Body.String())
	}
	if _, err := a.AdoptCoverCandidate(request.SearchID, variant.ID, 0); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledCoverLateResultDoesNotPersist(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	id, _ := a.store.FindOrCreateLocalRelease("target", nil)
	started, release := make(chan struct{}), make(chan struct{})
	result := coverResult("jp:999", "https://is1.mzstatic.com/late/100x100bb.jpg", "album", "song")
	setCoverSources(t, a, &coverSearchProvider{id: "only", search: func(context.Context, sources.Query) ([]sources.Result, error) {
		close(started)
		<-release
		return []sources.Result{result}, nil
	}})
	request := CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: id, Title: "song", Sources: []string{"only"}}
	if err := a.StartCoverSearch(request); err != nil {
		t.Fatal(err)
	}
	<-started
	a.CancelCoverSearch(request.SearchID)
	close(release)
	a.coverWorkers.Wait()
	catalog, err := a.store.GetCoverCatalog(result.Key())
	if err != nil || catalog != nil {
		t.Fatal("late result persisted", catalog, err)
	}
}
