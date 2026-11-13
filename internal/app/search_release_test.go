package app

import (
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func TestSearchAndEditorProjectExactReleaseState(t *testing.T) {
	for _, inLibrary := range []bool{false, true} {
		name := "sync-only"
		if inLibrary {
			name = "in-library"
		}
		t.Run(name, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			a.enricher.Pause()
			var requests atomic.Int32
			a.client.HTTP.Transport = countingTransport{&requests}
			base := candidateRow()
			base.AlbumCredit = base.Artist
			id, err := a.store.AdmitCandidate(base.toCandidate())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.store.DB().Exec("UPDATE track SET in_library=? WHERE id=?", inLibrary, id); err != nil {
				t.Fatal(err)
			}
			before, err := a.store.ReadEditorSession(id)
			if err != nil {
				t.Fatal(err)
			}
			rows := []CandidateRow{base, base, base, base, base}
			rows[1].MatchRef = "both-new-track"
			rows[2].MatchRef, rows[2].ReleaseRef = "group-only", strPtr("new-release")
			rows[3].MatchRef, rows[3].ReleaseGroupRef = "release-only", nil
			rows[4].MatchRef, rows[4].ReleaseRef, rows[4].ReleaseGroupRef = "neither", strPtr("new-release"), strPtr("new-group")
			candidates := make([]provider.Candidate, len(rows))
			for i := range rows {
				candidates[i] = rows[i].toCandidate()
			}
			p, err := a.registry.Get(provider.MusicBrainz)
			if err != nil {
				t.Fatal(err)
			}
			a.registry = provider.NewRegistry()
			a.registry.Register(&recordingSearcher{MetadataProvider: p, hit: candidates})
			check := func(got []CandidateRow, search bool) {
				t.Helper()
				if len(got) != len(rows) {
					t.Fatalf("got %d rows, want %d", len(got), len(rows))
				}
				byRef := map[string]CandidateRow{}
				for _, row := range got {
					byRef[row.MatchRef] = row
				}
				wantRelease := []bool{true, true, false, true, false}
				wantGroup := []bool{true, true, true, false, false}
				for i, row := range rows {
					actual := byRef[row.MatchRef]
					if actual.ReleaseExists != wantRelease[i] || actual.ReleaseGroupExists != wantGroup[i] || (actual.LocalTrackID != 0) != (i == 0) || actual.InLibrary != (i == 0 && inLibrary) {
						t.Fatalf("row %d: %+v", i, actual)
					}
					if !reflect.DeepEqual(actual.toCandidate(), row.toCandidate()) {
						t.Fatalf("candidate facts changed: %+v", actual)
					}
					if search {
						wantAction := "加入曲库"
						if i == 0 && inLibrary {
							wantAction = "已在库中"
						}
						if actual.Action != wantAction {
							t.Fatalf("structure changed action: %+v", actual)
						}
					}
				}
			}
			var eventRows []CandidateRow
			a.emit = func(event string, data ...any) {
				if event == EventProviderResults {
					eventRows = data[0].(providerResult).Rows
				}
			}
			response, err := a.SearchUpstream(*base.Title, *base.Artist, *base.Album, []string{base.Provider}, "")
			if err != nil {
				t.Fatal(err)
			}
			check(response.Aggregate, true)
			check(response.ByProvider[0].Rows, true)
			check(eventRows, true)
			editorRows, err := a.SearchEditCandidates(id, base.Provider, *base.Title, *base.Artist, *base.Album, "")
			if err != nil {
				t.Fatal(err)
			}
			check(editorRows, false)
			after, err := a.store.ReadEditorSession(id)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("candidate search changed editor entities", err)
			}
			if err := a.store.DeleteTrack(id); err != nil {
				t.Fatal(err)
			}
			release, err := a.store.GetReleaseByRef(base.Provider, *base.ReleaseRef)
			if err != nil {
				t.Fatal(err)
			}
			if release != nil {
				if err := a.store.DeleteOrphanRelease(release.ID); err != nil {
					t.Fatal(err)
				}
			}
			refreshed, err := a.RefreshSearchCandidates(editorRows)
			if err != nil {
				t.Fatal(err)
			}
			for i, row := range refreshed {
				if row.ReleaseExists || row.ReleaseGroupExists || row.LocalTrackID != 0 || row.Action != "加入曲库" || !reflect.DeepEqual(row.toCandidate(), editorRows[i].toCandidate()) {
					t.Fatal("refresh retained removed identities or changed facts", row)
				}
			}
			if requests.Load() != 0 {
				t.Fatal("local projection accessed network")
			}
		})
	}
}
