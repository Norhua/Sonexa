package app

import (
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/store"
)

func TestITunesAlbumUsesStoredStateRegardlessOfAge(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	old := time.Now().AddDate(-2, 0, 0)
	for _, state := range []store.TracklistState{store.TracklistComplete, store.TracklistPartial, store.TracklistFailed} {
		for _, fetched := range []*time.Time{nil, &old} {
			ref := string(state)
			if fetched == nil {
				ref += "-no-date"
			}
			id, err := app.store.UpsertRelease(&store.Release{Provider: strPtrOrNil("itunes"), ProviderRef: &ref, Title: "Album", TracklistState: state, FetchedAt: fetched})
			if err != nil {
				t.Fatal(err)
			}
			got, err := app.GetAlbumDetail(id)
			if err != nil || got.TracklistState != string(state) {
				t.Fatalf("清单状态被日期改变: %+v %v", got, err)
			}
		}
	}
}
