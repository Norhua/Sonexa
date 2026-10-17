package app

import (
	"fmt"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/covers"
)

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
			title, artist, album, country := "世界中の雨", "チームドラゴン from AKB48", "心の羽根", "jp"
			if source.ID == "caa" {
				title, artist, album = "God knows...", "平野綾", "涼宮ハルヒの詰合"
			}
			request := CoverSearchRequest{SearchID: uuid.NewString(), ReleaseID: id, Title: title, Artist: artist, Album: album, Country: country, Sources: []string{source.ID}}
			if err := a.StartCoverSearch(request); err != nil {
				t.Fatal(err)
			}
			defer a.CancelCoverSearch(request.SearchID)
			a.coverWorkers.Wait()
			results := a.CoverSearchResults(request.SearchID)
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
