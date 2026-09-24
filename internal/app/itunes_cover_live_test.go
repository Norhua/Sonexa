package app

import (
	"fmt"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/norhua/sonexa/internal/covers"
)

// 少量公开请求；默认跳过，不让离线 CI 依赖 iTunes/CDN 可用性。
func TestLiveITunesImportCovers(t *testing.T) {
	if os.Getenv("SONEXA_LIVE_ITUNES") != "1" {
		t.Skip("set SONEXA_LIVE_ITUNES=1 for live iTunes import/cover verification")
	}
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	svc, err := newCoverService(a.store, a.paths, a.settings.Get(), "test")
	if err != nil {
		t.Fatal(err)
	}
	a.coverSvc = a.attachLadderBuilder(svc)
	svc.EnableNetworkPolicy(a.checkCoverOutbound)
	defer svc.Close()
	response, err := a.SearchUpstream("ツバサ", "4円", "side-C", []string{"itunes"}, "jp")
	if err != nil {
		t.Fatal(err)
	}
	var row *CandidateRow
	for i := range response.Aggregate {
		if response.Aggregate[i].MatchRef == "jp:1475065334" {
			row = &response.Aggregate[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("target missing: %+v", response)
	}
	request := func(target, usage string) {
		t.Helper()
		r := httptest.NewRecorder()
		a.coverAssetHandler().ServeHTTP(r, httptest.NewRequest("GET", "/cover?"+target+"&usage="+usage, nil))
		if r.Code != 200 {
			t.Fatalf("%s: %d %s", usage, r.Code, r.Body.String())
		}
		info, err := covers.InspectImage(r.Body.Bytes())
		if err != nil || info.Width <= 0 {
			t.Fatalf("image: %+v %v", info, err)
		}
		t.Logf("%s %s: %dx%d %s", target, usage, info.Width, info.Height, info.Format)
	}
	request("provider=itunes&ref="+*row.ReleaseRef, "thumbnail")
	id, err := a.AddToLibrary(*row)
	if err != nil {
		t.Fatal(err)
	}
	d, err := a.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("releaseId=%d", *d.Track.ReleaseID)
	request(target, "thumbnail")
	request(target, "detail")
	if err := a.RefreshTrackDetail(id, "itunes", *row.ReleaseRef); err != nil {
		t.Fatal(err)
	}
	d, err = a.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.CreditText != "4円" || len(d.CreditSpans) != 1 || len(d.Participants) != 0 || deref(d.Metadata.Genre) != "J-Pop" {
		t.Fatalf("details: %+v", d)
	}
	t.Logf("track=%s album=%s credit=%s genre=%s participants=%d", d.Track.Title, deref(d.Metadata.Album), d.CreditText, deref(d.Metadata.Genre), len(d.Participants))
}
