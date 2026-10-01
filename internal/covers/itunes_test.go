package covers

import (
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestITunesResolverSharesLookupAndImageAndKeepsCountry(t *testing.T) {
	svc, _ := resolverService(t) // 并发 1 也不能嵌套死锁
	var mu sync.Mutex
	lookups := map[string]int{}
	images := map[string]int{}
	svc.SetDetailLoader(func(_ context.Context, target provider.Ref) (*provider.Detail, error) {
		ref := target.ID
		mu.Lock()
		lookups[ref]++
		mu.Unlock()
		art := "https://is1.mzstatic.com/" + ref + "/100x100bb.jpg"
		return &provider.Detail{Provider: provider.ITunes, ReleaseRef: &ref, ArtworkURL: &art}, nil
	})
	svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		images[r.URL.Path]++
		mu.Unlock()
		size := 250
		if strings.HasSuffix(r.URL.Path, "/1200x1200bb.jpg") {
			size = 1200
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(pngBytes(size, size))))}, nil
	})
	var wg sync.WaitGroup
	for range 3 {
		for _, usage := range []Usage{Thumbnail, Detail} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := svc.Resolve(context.Background(), Target{Provider: "itunes", Ref: "jp:12"}, usage)
				want := 250
				if usage == Detail {
					want = 1200
				}
				if err != nil || got.File == nil || got.File.Width != want {
					t.Errorf("%s: %+v %v", usage, got, err)
				}
			}()
		}
	}
	wg.Wait()
	if _, err := svc.Resolve(context.Background(), Target{Provider: "itunes", Ref: "us:12"}, Thumbnail); err != nil {
		t.Fatal(err)
	}
	if lookups["jp:12"] != 1 || lookups["us:12"] != 1 || len(images) != 3 {
		t.Fatal(lookups, images)
	}
	for path, n := range images {
		if n != 1 {
			t.Fatalf("duplicate image: %s=%d", path, n)
		}
	}
}

func TestITunesCatalogEmptyErrorAndMissingArtwork(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			svc, st := resolverService(t)
			calls := 0
			svc.SetDetailLoader(func(_ context.Context, target provider.Ref) (*provider.Detail, error) {
				ref := target.ID
				calls++
				if fail {
					return nil, fmt.Errorf("upstream failed")
				}
				return &provider.Detail{Provider: provider.ITunes, ReleaseRef: &ref}, nil
			})
			for range 2 {
				got, err := svc.Resolve(context.Background(), Target{Provider: "itunes", Ref: "jp:12"}, Detail)
				if (err != nil) != fail || (!fail && got.Outcome != OutcomeEmpty) {
					t.Fatalf("%+v %v", got, err)
				}
			}
			c, err := st.GetCoverCatalog(store.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: "jp:12"})
			want := "empty"
			if fail {
				want = "error"
			}
			if err != nil || c.State != want || calls != 1 {
				t.Fatalf("catalog: %+v %v, calls=%d", c, err, calls)
			}
		})
	}
}

func TestITunesAdmissionWithoutPreviewAndSizeFallback(t *testing.T) {
	svc, st := resolverService(t)
	title, album, ref, art := "ツバサ", "Side-C", "jp:1475065332", "https://is1.mzstatic.com/sidec/250x250bb.jpg"
	candidate := provider.Candidate{Provider: provider.ITunes, MatchRef: "jp:1475065334", Title: &title, AlbumTitle: &album, ReleaseRef: &ref, ThumbURL: &art}
	candidate.ArtworkCatalog = (&itunes.Provider{}).ArtworkStrategy().Candidate(candidate)
	id, err := st.AdmitCandidate(candidate)
	if err != nil {
		t.Fatal(err)
	}
	track, err := st.GetTrack(id)
	if err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Path)
		status, data := 404, []byte{}
		if strings.HasSuffix(r.URL.Path, "/600x600bb.jpg") {
			status, data = 200, pngBytes(500, 400)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	})
	got, err := svc.Resolve(context.Background(), Target{ReleaseID: *track.ReleaseID}, Detail)
	if err != nil || got.File.Width != 500 || got.File.Height != 400 {
		t.Fatalf("fallback must use actual size: %+v %v", got, err)
	}
	if len(calls) != 3 || !strings.Contains(calls[0], "1200x1200") || !strings.Contains(calls[1], "3000x3000") || !strings.Contains(calls[2], "600x600") {
		t.Fatal(calls)
	}
	if _, err := svc.Resolve(context.Background(), Target{ReleaseID: *track.ReleaseID}, Detail); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatal("retried unavailable variants or downloaded same image", calls)
	}
}

func TestITunesDoesNotReuseManualPreviewAsAutomaticSelection(t *testing.T) {
	svc, st := resolverService(t)
	key := store.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: "jp:12"}
	if _, err := st.SaveCoverCatalog(key, itunes.CoverImages("https://is1.mzstatic.com/a/100x100bb.jpg", "album", "artist")); err != nil {
		t.Fatal(err)
	}
	c, err := st.GetCoverCatalog(key)
	if err != nil {
		t.Fatal(err)
	}
	// 手动查看 3000 档，不把这份预览直接纳入自动本地图集合。
	if _, _, err := svc.Library().SaveVariant(0, c.Images[0].Variants[0].ID, pngBytes(3000, 900), false, 0); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "/250x250bb.jpg") {
			t.Errorf("wrong automatic size: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(pngBytes(250, 250))))}, nil
	})
	got, err := svc.Resolve(context.Background(), Target{Provider: "itunes", Ref: "jp:12"}, Thumbnail)
	if err != nil || got.File.Width != 250 || calls != 1 {
		t.Fatalf("manual preview changed auto cover: %+v %v calls=%d", got, err, calls)
	}
}
