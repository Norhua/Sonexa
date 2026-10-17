package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/provider"
)

func liveCoverSample(source string, getenv func(string) string) sources.Query {
	q := sources.Query{Title: "世界中の雨", Artist: "チームドラゴン from AKB48", Album: "心の羽根", Country: "jp", Limit: 20, Force: true}
	if source == "caa" {
		q.Title, q.Artist, q.Album = "God knows...", "平野綾", "涼宮ハルヒの詰合"
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"TITLE", &q.Title}, {"ARTIST", &q.Artist}, {"ALBUM", &q.Album}, {"COUNTRY", &q.Country},
	} {
		if value := getenv("SONEXA_COVER_" + field.name); value != "" {
			*field.value = value
		}
	}
	return q
}

// Optional iTunes diagnosis reuses the production metadata projection and cover
// relevance rule. One extra budgeted request; no raw response/body is logged.
func logLiveITunesCoverRecall(t *testing.T, a *App, ctx context.Context, q sources.Query) {
	t.Helper()
	p, err := a.registry.Get(provider.ITunes)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := p.SearchTracks(ctx, provider.Query{Title: q.Title, Artist: q.Artist, Album: q.Album, Country: q.Country, Limit: q.Limit, SkipCache: true})
	if err != nil {
		t.Fatal("iTunes recall diagnosis failed; stop without another request")
	}
	identified, relevant := 0, 0
	for i, c := range rows {
		usable := c.ReleaseRef != nil && c.ThumbURL != nil
		match := sources.Relevant(q, deref(c.Title), deref(c.ArtistName), deref(c.AlbumTitle))
		if usable {
			identified++
			if match {
				relevant++
			}
		}
		if i < 5 {
			t.Logf("itunes recall ref=%s title=%q artist=%q album=%q has_identity_and_artwork=%t relevant=%t", c.MatchRef, deref(c.Title), deref(c.ArtistName), deref(c.AlbumTitle), usable, match)
		}
	}
	t.Logf("itunes recall total=%d identity_and_artwork=%d relevant=%d", len(rows), identified, relevant)
}

// Default-off production composition, including CAA/iTunes and the actual image
// pool/security policy. No direct HTTP shortcut or modified image transport.
func TestLiveCoverSources(t *testing.T) {
	requireLive(t, "SONEXA_LIVE_COVERS", os.Getenv("SONEXA_LIVE_COVERS"))
	a, audit := newLiveApp(t, 12)
	ctx, cancel := liveContext(a, audit, 3*time.Minute)
	defer cancel()
	a.ctx = ctx
	var imagePermits atomic.Int32
	a.coverSvc.Pool().SetGate(func() error {
		if err := audit.gate(); err != nil {
			return err
		}
		if err := a.checkCoverOutbound(); err != nil {
			return err
		}
		n := imagePermits.Add(1)
		if n > 24 {
			audit.stop()
			return errLiveStopped
		}
		t.Logf("image_or_catalog_permit=%d (includes redirects and production retries)", n)
		return nil
	})
	matched := false
	for _, source := range a.ListCoverSources() {
		if only := os.Getenv("SONEXA_COVER_SOURCE"); only != "" && source.ID != only {
			continue
		}
		matched = true
		if !t.Run(source.ID, func(t *testing.T) {
			id, err := a.store.FindOrCreateLocalRelease("live-cover-verification", nil)
			if err != nil {
				t.Fatal(err)
			}
			q := liveCoverSample(source.ID, os.Getenv)
			if source.ID == "itunes" && os.Getenv("SONEXA_COVER_DIAGNOSTICS") == "1" {
				logLiveITunesCoverRecall(t, a, ctx, q)
			}
			request := CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: id, Title: q.Title, Artist: q.Artist, Album: q.Album, Country: q.Country, Sources: []string{source.ID}}
			if err := a.StartCoverSearch(request); err != nil {
				t.Fatal(err)
			}
			defer a.CancelCoverSearch(request.SearchID)
			a.coverWorkers.Wait()
			results := a.CoverSearchResults(request.SearchID)
			for _, result := range results {
				t.Logf("source=%s state=%s cards=%d has_error=%t", result.Source, result.State, len(result.Cards), result.Error != "")
			}
			if len(results) != 1 || results[0].State != "ok" || len(results[0].Cards) == 0 {
				t.Fatalf("source search unsuccessful results=%d (no response dump)", len(results))
			}
			card := results[0].Cards[0]
			if len(card.Image.Variants) == 0 {
				t.Fatal("no image variants")
			}
			variant := card.Image.Variants[0]
			for _, v := range card.Image.Variants {
				if v.Spec == "250" || source.ID == "qq" && v.Spec == "800" || source.ID == "163" && v.Spec == "original" {
					variant = v
				}
			}
			preview := httptest.NewRecorder()
			a.coverAssetHandler().ServeHTTP(preview, httptest.NewRequest("GET", fmt.Sprintf("/cover?variantId=%d&searchId=%s", variant.ID, request.SearchID), nil).WithContext(ctx))
			if preview.Code != 200 {
				t.Fatalf("preview status=%d", preview.Code)
			}
			info, err := covers.InspectImage(preview.Body.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			adopted, err := a.AdoptCoverCandidate(request.SearchID, variant.ID, a.CoverRevision(id))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("source=%s requested=%s actual=%dx%d bytes=%d adopted_revision=%d", source.ID, variant.Spec, info.Width, info.Height, info.Bytes, adopted.Revision)
		}) {
			audit.stop()
			return
		}
	}
	if !matched {
		t.Fatal("unknown SONEXA_COVER_SOURCE")
	}
}

func TestLiveITunesRecallUsesProductionQueryAndBudget(t *testing.T) {
	a, audit := newLiveApp(t, 1)
	calls := 0
	a.client.HTTP.Transport = audit.transport(roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if r.URL.Host != "itunes.apple.com" || r.URL.Path != "/search" || q.Get("term") != "song artist album" || q.Get("country") != "jp" || q.Get("limit") != "20" {
			t.Error("diagnosis changed the production query", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[
			{"trackId":1,"collectionId":2,"trackName":"song","artistName":"artist","collectionName":"album","artworkUrl100":"https://is1.mzstatic.com/100x100bb.jpg"},
			{"trackId":3,"collectionId":4,"trackName":"other","artistName":"artist","collectionName":"unrelated","artworkUrl100":"https://is1.mzstatic.com/100x100bb.jpg"}
		]}`))}, nil
	}), true)
	ctx, cancel := liveContext(a, audit, time.Second)
	defer cancel()
	logLiveITunesCoverRecall(t, a, ctx, sources.Query{Title: "song", Artist: "artist", Album: "album", Country: "jp", Limit: 20, Force: true})
	if calls != 1 || len(audit.snapshot()) != 1 || audit.gate() == nil {
		t.Fatal("diagnostic request escaped the shared budget", calls)
	}
}

func TestLiveCoverSampleDefaultsAndOverrides(t *testing.T) {
	for _, source := range []string{"caa", "itunes", "qq", "163"} {
		q := liveCoverSample(source, func(string) string { return "" })
		wantTitle := "世界中の雨"
		if source == "caa" {
			wantTitle = "God knows..."
		}
		if q.Title != wantTitle || q.Country != "jp" || q.Limit != 20 || !q.Force {
			t.Fatal("default changed", source, q)
		}
		values := map[string]string{"SONEXA_COVER_TITLE": "ツバサ", "SONEXA_COVER_ARTIST": "4円", "SONEXA_COVER_ALBUM": "side-C", "SONEXA_COVER_COUNTRY": "us"}
		q = liveCoverSample(source, func(key string) string { return values[key] })
		if q.Title != "ツバサ" || q.Artist != "4円" || q.Album != "side-C" || q.Country != "us" || q.Limit != 20 || !q.Force {
			t.Fatal("override changed query policy", source, q)
		}
	}
}
