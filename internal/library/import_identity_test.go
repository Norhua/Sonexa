package library

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func TestAutomaticImportRechecksCrossProviderArrivalAtCommit(t *testing.T) {
	s, st, _ := newTestService(t)
	title, artist, album := "Song", "Artist", "Album"
	batch, err := st.CreateBatch(&store.ImportBatch{TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.EnqueueItems(batch, []store.ImportItem{{RawRow: `["Song","Artist","Album"]`, InTitle: &title, InArtist: &artist, InAlbum: &album}}); err != nil {
		t.Fatal(err)
	}
	item, err := st.ClaimNextItem(batch)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	incoming := seed("release", "track", "group", album, artist)
	incoming.Snapshot.Track.Title = domain.Known(title)
	incoming.Snapshot.Track.Credit = domain.Known(domain.Credit{Text: artist})
	incoming.ImportItemID = item.ID
	incoming.AutomaticImport = true
	plan := prepare(t, s, incoming)
	other := seed("qq-release", "qq-track", "qq-group", album, artist)
	other.Snapshot.Track.Title = domain.Known(title)
	other.Snapshot.Track.Credit = domain.Known(domain.Credit{Text: artist})
	other.Snapshot.Track.Identity.Provider = "qq"
	other.Snapshot.Release.Identity.Provider = "qq"
	other.Snapshot.Release.Group.Provider = "qq"
	owned := commit(t, s, prepare(t, s, other), false)
	result, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{})
	if err != nil || !result.Existing || result.TrackID != owned.TrackID {
		t.Fatal(result, err)
	}
	var count int
	if err = st.DB().QueryRow(`SELECT COUNT(*) FROM track`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestAutomaticImportRechecksExistingTrackAtCommit(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(map[bool]string{true: "consistent", false: "different album"}[same], func(t *testing.T) {
			s, st, _ := newTestService(t)
			title, artist, album := "Song", "Artist", "Album"
			if !same {
				album = "Another Album"
			}
			batch, err := st.CreateBatch(&store.ImportBatch{TotalRows: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err = st.EnqueueItems(batch, []store.ImportItem{{RawRow: `["Song"]`, InTitle: &title, InArtist: &artist, InAlbum: &album}}); err != nil {
				t.Fatal(err)
			}
			item, err := st.ClaimNextItem(batch)
			if err != nil || item == nil {
				t.Fatal(item, err)
			}
			incoming := seed("release", "track", "group", "Album", "Artist")
			incoming.Snapshot.Track.Title = domain.Known(title)
			incoming.ImportItemID = item.ID
			incoming.ItemStatus = "imported"
			incoming.AutomaticImport = true
			plan := prepare(t, s, incoming)
			concurrent := incoming
			concurrent.ImportItemID = 0
			concurrent.AutomaticImport = false
			commit(t, s, prepare(t, s, concurrent), false)
			result, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{})
			if same {
				if err != nil || !result.Existing {
					t.Fatal(result, err)
				}
				saved, _ := st.GetItem(item.ID)
				if saved.Status != store.StatusSkipped {
					t.Fatal(saved)
				}
			} else {
				if !errors.Is(err, store.ErrPlanStale) {
					t.Fatal("inconsistent concurrent binding accepted", result, err)
				}
				saved, _ := st.GetItem(item.ID)
				if saved.Status != store.StatusMatching {
					t.Fatal(saved)
				}
			}
		})
	}
}
