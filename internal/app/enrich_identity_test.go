package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// A release with distinct tracks/credits, reproducing image3/image4. IDs are
// deliberately synthetic; no live service or user database is used.
const enrichmentRelease = `{
 "id":"10000000-0000-4000-8000-000000000001","title":"狼と巫女とお菓子の家","artist-credit":[{"name":"Crest"}],
 "media":[{"position":1,"format":"CD","track-count":4,"tracks":[
  {"id":"20000000-0000-4000-8000-000000000001","position":1,"number":"1","title":"Honey Trap","length":267000,
   "artist-credit":[{"name":"Chia"}],"recording":{"id":"rec-honey"}},
  {"id":"20000000-0000-4000-8000-000000000002","position":2,"number":"2","title":"Under The Moon","length":230000},
  {"id":"20000000-0000-4000-8000-000000000003","position":3,"number":"3","title":"Fire","length":216000},
  {"id":"20000000-0000-4000-8000-000000000004","position":4,"number":"4","title":"綿雪の夢","length":321000,
   "artist-credit":[{"name":"ハトリアヤ"}],"recording":{"id":"rec-snow","isrcs":["JPTEST000004"],
    "relations":[{"type":"vocal","artist":{"id":"art-snow","name":"ハトリアヤ","type":"Person"}},
     {"type":"performance","work":{"id":"work-snow","relations":[
      {"type":"composer","artist":{"id":"art-composer","name":"Snow Composer","type":"Person"}}]}}]}}
 ]}]
}`
const enrichmentSearch = `{"recordings":[{"id":"rec-snow","title":"綿雪の夢","artist-credit":[{"name":"ハトリアヤ"}],
 "releases":[{"id":"10000000-0000-4000-8000-000000000001","title":"狼と巫女とお菓子の家","media":[{"position":1,"format":"CD","track-count":4,
 "track":[{"id":"20000000-0000-4000-8000-000000000004","position":4,"title":"綿雪の夢","length":321000}]}]}]}]}`

func replayEnrichment(t *testing.T, app *App, body string) *atomic.Int32 {
	t.Helper()
	var details atomic.Int32
	app.client.HTTP.Transport = coverReplayTransport(func(r *http.Request) (*http.Response, error) {
		response := body
		switch r.URL.Path {
		case "/ws/2/release/10000000-0000-4000-8000-000000000001", "/lookup":
			details.Add(1)
		case "/ws/2/recording":
			response = enrichmentSearch
		default:
			return nil, fmt.Errorf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	return &details
}

func snowCandidate() provider.Candidate {
	number, disc, duration := 4, 1, 321000
	return provider.Candidate{Provider: provider.MusicBrainz, MatchRef: "20000000-0000-4000-8000-000000000004",
		Title: strPtrOrNil("綿雪の夢"), ArtistName: strPtrOrNil("ハトリアヤ"), AlbumTitle: strPtrOrNil("狼と巫女とお菓子の家"),
		ReleaseRef: strPtrOrNil("10000000-0000-4000-8000-000000000001"), RecordingRef: strPtrOrNil("rec-snow"),
		TrackNumber: &number, DiscNumber: &disc, DurationMS: &duration}
}

func TestNonFirstTrackEnrichmentKeepsIdentityAndCachesTracklist(t *testing.T) {
	for _, mode := range []string{"background", "refresh", "import-on-match"} {
		t.Run(mode, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			app.enricher.Pause() // admit/import must not start a competing worker
			requests := replayEnrichment(t, app, enrichmentRelease)
			var id int64
			var err error
			if mode == "import-on-match" {
				cur := app.settings.Get()
				cur.Matching.TracklistTiming = config.TracklistOnMatch
				app.rebuildRunner(cur)
				batch, e := app.store.CreateBatch(&store.ImportBatch{Filename: "snow.csv", TotalRows: 1})
				if e != nil {
					t.Fatal(e)
				}
				c := snowCandidate()
				if e = app.store.EnqueueItems(batch, []store.ImportItem{{RawRow: "{}", InTitle: c.Title, InArtist: c.ArtistName, InAlbum: c.AlbumTitle}}); e != nil {
					t.Fatal(e)
				}
				if e = app.runner.Start(context.Background(), batch); e != nil {
					t.Fatal(e)
				}
				waitFor(t, "import completion", func() bool { return !app.runner.IsRunning(batch) })
				id, err = app.store.FindByMatchRef("musicbrainz", "20000000-0000-4000-8000-000000000004")
			} else {
				id, err = app.store.AdmitCandidate(snowCandidate())
				if err != nil {
					t.Fatal(err)
				}
				if mode == "background" {
					err = app.StartEnrich()
					if err != nil {
						t.Fatal(err)
					}
					waitFor(t, "background completion", func() bool { return app.enricher.Status().State == enrichIdle })
				} else {
					err = app.RefreshTrackDetail(id, "musicbrainz", "10000000-0000-4000-8000-000000000001")
				}
			}
			if err != nil || id == 0 {
				t.Fatalf("admission/enrichment: id=%d err=%v", id, err)
			}
			assertEnrichState(t, app, id, store.EnrichFetched)
			page, err := app.ListTracks(store.TrackFilter{})
			if err != nil || len(page.Tracks) != 1 {
				t.Fatalf("library: %+v %v", page, err)
			}
			row := page.Tracks[0]
			if row.Title != "綿雪の夢" || deref(row.Artist) != "ハトリアヤ" || row.TrackNumber == nil || *row.TrackNumber != 4 {
				t.Fatalf("non-first track overwritten: %+v", row)
			}
			detail, err := app.GetTrackDetail(id)
			if err != nil {
				t.Fatal(err)
			}
			m := detail.Metadata
			if deref(m.MatchRef) != "20000000-0000-4000-8000-000000000004" || deref(m.RecordingRef) != "rec-snow" || deref(m.WorkRef) != "work-snow" || deref(m.ISRC) != "JPTEST000004" || m.DurationMS == nil || *m.DurationMS != 321000 {
				t.Fatalf("wrong target metadata: %+v", m)
			}
			if detail.CreditText != "ハトリアヤ" || !strings.Contains(fmt.Sprint(detail.Participants), "Snow Composer") {
				t.Fatalf("wrong target participants: %+v", detail)
			}
			album, err := app.GetAlbumDetail(*row.ReleaseID)
			if err != nil {
				t.Fatal(err)
			}
			if album.Incomplete || album.TracklistState != "complete" || len(album.Rows) != 4 || album.OwnedCount != 1 {
				t.Fatalf("detail did not cache tracklist: %+v", album)
			}
			for i, r := range album.Rows {
				if r.Ghost != (i != 3) || (i == 3 && (r.TrackID != id || r.Title != row.Title || r.TrackNumber != *row.TrackNumber)) {
					t.Errorf("album/detail disagree: %+v", r)
				}
			}
			// Reopening a complete album must not need another metadata request.
			if err := app.FetchTracklist("musicbrainz", "10000000-0000-4000-8000-000000000001", false); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 1 {
				t.Errorf("detail requests = %d, want 1", requests.Load())
			}
		})
	}
}

func TestUnresolvedDetailTargetDoesNotModifyTrack(t *testing.T) {
	for _, ref := range []string{"", "20000000-0000-4000-8000-000000000009", "rec-snow@10000000-0000-4000-8000-000000000001"} {
		t.Run("ref="+ref, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			app.enricher.Pause()
			replayEnrichment(t, app, enrichmentRelease)
			c := snowCandidate()
			c.MatchRef = ref
			id, err := app.store.AdmitCandidate(c)
			if ref == "" {
				if err == nil {
					t.Fatal("blank exact identity must be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := app.store.GetMetadata(id)
			if err != nil {
				t.Fatal(err)
			}
			if err := app.RefreshTrackDetail(id, "musicbrainz", "10000000-0000-4000-8000-000000000001"); err == nil {
				t.Fatal("unresolved target must fail, not select first track or mark fetched")
			}
			after, err := app.store.GetMetadata(id)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("unresolved detail changed track:\nbefore=%+v\nafter=%+v", before, after)
			}
			assertEnrichState(t, app, id, store.EnrichPending)
		})
	}
}

func TestSharedReleaseCacheStillSelectsEachTarget(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.enricher.Pause()
	requests := replayEnrichment(t, app, enrichmentRelease)
	for _, ref := range []string{"20000000-0000-4000-8000-000000000004", "20000000-0000-4000-8000-000000000001"} {
		c := snowCandidate()
		c.MatchRef = ref
		c.ISRC = nil
		if ref == "20000000-0000-4000-8000-000000000001" {
			c.Title = strPtrOrNil("Honey Trap")
			c.RecordingRef = strPtrOrNil("rec-honey")
		}
		id, err := app.store.AdmitCandidate(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := app.RefreshTrackDetail(id, "musicbrainz", "10000000-0000-4000-8000-000000000001"); err != nil {
			t.Fatal(err)
		}
		m, err := app.store.GetMetadata(id)
		if err != nil {
			t.Fatal(err)
		}
		wantNumber := 4
		if ref == "20000000-0000-4000-8000-000000000001" {
			wantNumber = 1
		}
		if deref(m.MatchRef) != ref || m.Title != *c.Title || m.TrackNumber == nil || *m.TrackNumber != wantNumber {
			t.Fatalf("cached response selected wrong track: %+v", m)
		}
	}
	if requests.Load() != 1 {
		t.Errorf("shared release should reuse raw response, got %d requests", requests.Load())
	}
}

func TestTracklistRefreshDoesNotEnrichAnArbitraryTrack(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.enricher.Pause()
	replayEnrichment(t, app, enrichmentRelease)
	c := snowCandidate()
	c.MatchRef = "20000000-0000-4000-8000-000000000001"
	c.Title = strPtrOrNil("Search title")
	c.RecordingRef = strPtrOrNil("rec-honey")
	id, err := app.store.AdmitCandidate(c)
	if err != nil {
		t.Fatal(err)
	}
	// Prime the list; the old FetchTracklist path enriched the first owned
	// recording only on subsequent fetches.
	for i := 0; i < 2; i++ {
		if err := app.FetchTracklist("musicbrainz", "10000000-0000-4000-8000-000000000001", true); err != nil {
			t.Fatal(err)
		}
	}
	track, err := app.store.GetTrack(id)
	if err != nil {
		t.Fatal(err)
	}
	if track.Title != "Search title" {
		t.Fatalf("release-only fetch changed track: %+v", track)
	}
	assertEnrichState(t, app, id, store.EnrichPending)
}

func TestDetailIdentityChangeDuringFetchIsRejected(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.enricher.Pause()
	id, err := app.store.AdmitCandidate(snowCandidate())
	if err != nil {
		t.Fatal(err)
	}
	replayEnrichment(t, app, enrichmentRelease)
	transport := app.client.HTTP.Transport
	app.client.HTTP.Transport = coverReplayTransport(func(r *http.Request) (*http.Response, error) {
		if err := app.store.SetProvenance(id, "musicbrainz", "new-track"); err != nil {
			return nil, err
		}
		return transport.RoundTrip(r)
	})
	if err := app.RefreshTrackDetail(id, "musicbrainz", "10000000-0000-4000-8000-000000000001"); err == nil {
		t.Fatal("stale response should fail")
	}
	m, err := app.store.GetMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if deref(m.MatchRef) != "new-track" || m.WorkRef != nil {
		t.Fatalf("stale detail was applied: %+v", m)
	}
	assertEnrichState(t, app, id, store.EnrichPending)
}

func TestTruncatedDetailTracklistRemainsPartial(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.enricher.Pause()
	replayEnrichment(t, app, strings.Replace(enrichmentRelease, `"track-count":4`, `"track-count":8`, 1))
	id, err := app.store.AdmitCandidate(snowCandidate())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RefreshTrackDetail(id, "musicbrainz", "10000000-0000-4000-8000-000000000001"); err != nil {
		t.Fatal(err)
	}
	rel, err := app.store.GetReleaseByRef("musicbrainz", "10000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TracklistState != store.TracklistPartial {
		t.Fatalf("truncated list marked %s", rel.TracklistState)
	}
}

func TestITunesEnrichmentUsesCountryQualifiedTrackRef(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.enricher.Pause()
	replayEnrichment(t, app, `{"results":[
	 {"wrapperType":"collection","collectionId":10,"collectionName":"Album","artistName":"Album Artist","trackCount":2},
	 {"wrapperType":"track","trackId":11,"trackName":"First","artistName":"First Artist","trackNumber":1,"discNumber":1},
	 {"wrapperType":"track","trackId":12,"trackName":"Second","artistName":"Second Artist","trackNumber":2,"discNumber":1,"trackTimeMillis":200000}]}`)
	id, err := app.store.AdmitCandidate(provider.Candidate{Provider: provider.ITunes, MatchRef: "jp:12",
		Title: strPtrOrNil("Second"), ArtistName: strPtrOrNil("Search Artist"), AlbumTitle: strPtrOrNil("Album"), ReleaseRef: strPtrOrNil("jp:10")})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RefreshTrackDetail(id, "itunes", "jp:10"); err != nil {
		t.Fatal(err)
	}
	d, err := app.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Track.Title != "Second" || d.CreditText != "Second Artist" || d.Metadata.TrackNumber == nil || *d.Metadata.TrackNumber != 2 {
		t.Fatalf("iTunes target not enriched: %+v", d)
	}
	p, err := app.registry.Get(provider.ITunes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.FetchDetail(context.Background(), provider.Ref{ID: "jp:10", TrackRef: "us:12"}); err == nil {
		t.Fatal("wrong storefront must not resolve target")
	}
}
