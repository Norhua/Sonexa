package app

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// 真实 MB 精简回放：有署名、无角色关系。不要用合成的 enrichmentRelease 推断此录音有 vocal。
func TestSnowCreditSurvivesEnrichmentWithoutInventedParticipants(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.enricher.Pause()
	body, err := os.ReadFile("../../testdata/golden/responses/musicbrainz/release_snow_no_roles.json")
	if err != nil {
		t.Fatal(err)
	}
	const release = "54953b76-c791-4415-8092-1f5487cb8df9"
	const track = "3d9c04e3-2f21-4e1f-a307-e40a96ff82cf"
	requests := 0
	app.client.HTTP.Transport = coverReplayTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/ws/2/release/"+release {
			return nil, fmt.Errorf("unexpected request: %s", r.URL)
		}
		for _, inc := range []string{"artist-credits", "artist-rels", "recording-level-rels", "work-rels", "work-level-rels"} {
			if !strings.Contains(r.URL.Query().Get("inc"), inc) {
				t.Errorf("missing inc %s", inc)
			}
		}
		requests++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	id, err := app.store.AdmitCandidate(provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: track, ReleaseRef: strPtrOrNil(release),
		Title: strPtrOrNil("綿雪の夢"), AlbumTitle: strPtrOrNil("狼と巫女とお菓子の家"), ArtistName: strPtrOrNil("ハトリアヤ"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RefreshTrackDetail(id, "musicbrainz", release); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
	assertEnrichState(t, app, id, store.EnrichFetched)
	detail, err := app.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Participants) != 0 {
		t.Fatalf("invented roles: %+v", detail.Participants)
	}
	if detail.CreditText != "ハトリアヤ" || len(detail.CreditSpans) != 1 || detail.CreditSpans[0].Text != "ハトリアヤ" {
		t.Fatalf("lost known credit: %+v", detail)
	}
	facets, err := app.store.RoleFacets(detail.CreditSpans[0].ArtistID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, facet := range facets {
		counts[facet.Role] = facet.Count
	}
	if counts[store.RoleCredited] != 1 || counts[string(store.RoleVocal)] != 0 {
		t.Fatalf("credit must not become vocal: %+v", facets)
	}
	// 修改署名后旧区间不能变成新名字的链接，即使字节长度正好相同。
	if err := app.store.UpdateTrackFields(id, nil, strPtrOrNil("新しい名前"), true); err != nil {
		t.Fatal(err)
	}
	detail, err = app.GetTrackDetail(id)
	if err != nil || len(detail.CreditSpans) != 0 {
		t.Fatalf("stale spans linked edited credit: %+v, %v", detail, err)
	}
}
