package app

import (
	"testing"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestCoverDTOsShareReleaseAndRevisionWithoutChangingCandidateIdentity(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	source, ref, title, album := "itunes", "jp:12", "song", "album"
	trackID, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: title, Album: &album, MatchSource: &source, MatchRef: strPtrOrNil("jp:1"),
		Release: &store.ReleaseInput{Provider: &source, ProviderRef: &ref},
	})
	if err != nil {
		t.Fatal(err)
	}
	track, err := a.store.GetTrack(trackID)
	if err != nil {
		t.Fatal(err)
	}
	releaseID := *track.ReleaseID
	var events []CoverChangedEvent
	a.emit = func(name string, values ...any) {
		if name == EventCoverChanged {
			events = append(events, values[0].(CoverChangedEvent))
		}
	}
	selection, err := a.coverSvc.Library().ImportLocal(releaseID, coverPNG(t, 900, 600), 0)
	if err != nil {
		t.Fatal(err)
	}
	a.notifyCoverChanged(selection)
	if len(events) != 1 || events[0].ReleaseID != releaseID || events[0].Revision != 1 || events[0].Provider != source || events[0].Ref != ref {
		t.Fatalf("封面事件缺少目标或候选发行身份: %+v", events)
	}
	view, err := a.store.GetTrackView(trackID)
	if err != nil || view.CoverRevision != 1 || *view.ReleaseID != releaseID {
		t.Fatal(view, err)
	}
	detail, err := a.GetTrackDetail(trackID)
	if err != nil || detail.Track.CoverRevision != 1 || *detail.Track.ReleaseID != releaseID {
		t.Fatal(detail, err)
	}
	albumDetail, err := a.GetAlbumDetail(releaseID)
	if err != nil || albumDetail.CoverRevision != 1 || albumDetail.ReleaseID != releaseID {
		t.Fatal(albumDetail, err)
	}
	albums, err := a.store.ListAlbums()
	if err != nil || len(albums) != 1 || albums[0].CoverRevision != 1 || albums[0].ReleaseID != releaseID {
		t.Fatal(albums, err)
	}
	candidate := provider.Candidate{Provider: provider.ITunes, MatchRef: "jp:2", Title: &title, AlbumTitle: &album, ReleaseRef: &ref}
	rows := a.rowsFrom([]matching.ScoredCandidate{{Candidate: candidate}}, "加入曲库")
	if len(rows) != 1 || rows[0].ReleaseID != releaseID || rows[0].CoverRevision != 1 || rows[0].MatchRef != "jp:2" {
		t.Fatal(rows)
	}
	refs := a.BuildCoverRefs(rows)
	if len(refs) != 1 || refs[0].Target.ReleaseID != releaseID || refs[0].Usage != covers.Thumbnail || refs[0].Key != (covers.Target{ReleaseID: releaseID}).Key(covers.Thumbnail, 1) {
		t.Fatal(refs)
	}
	otherRef := "jp:13"
	candidate.ReleaseRef = &otherRef
	other := a.rowsFrom([]matching.ScoredCandidate{{Candidate: candidate}}, "加入曲库")
	if other[0].ReleaseID != 0 || other[0].CoverRevision != 0 {
		t.Fatal("文本相同的另一发行借用了封面", other)
	}
}
