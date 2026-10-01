package library

import (
	"encoding/json"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"os"
	"strconv"
	"testing"
)

func researchRelease(t *testing.T, id string) domain.Snapshot {
	t.Helper()
	raw, err := os.ReadFile("../provider/musicbrainz/testdata/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := musicbrainz.ParseDetailJSON(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	return *provider.DetailSnapshot(detail)
}
func mappingSessions(t *testing.T, snapshot domain.Snapshot) []*domain.EditorSession {
	t.Helper()
	sessions := []*domain.EditorSession{}
	for i, entry := range snapshot.Release.Tracklist.Value {
		track := trackFromAlbumEntry(entry, *snapshot.Release)
		body, err := json.Marshal(domain.Snapshot{Track: &track, Release: snapshot.Release})
		if err != nil {
			t.Fatal(err)
		}
		fields, err := domain.EditorFields(domain.Snapshot{Track: &track})
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, &domain.EditorSession{TrackID: int64(i + 1), Current: fields, SnapshotJSON: string(body)})
	}
	return sessions
}
func TestAlbumMappingResearchVersionsBothDirections(t *testing.T) {
	for _, pair := range [][2]string{{"0bcd51de-dabe-4106-9118-716f67fef4db", "8c4d9698-bcac-456a-a5ac-1bd90d64c4ff"}, {"bb08c318-b5c2-426e-9c43-58c520ae116c", "d642286a-4ff9-476a-8708-116176151f97"}} {
		a, b := researchRelease(t, pair[0]), researchRelease(t, pair[1])
		for direction := 0; direction < 2; direction++ {
			source, target := a, b
			if direction == 1 {
				source, target = b, a
			}
			sessions := mappingSessions(t, source)
			plan, err := planAlbumRows(domain.EditDraft{BindingAction: "set"}, &domain.EditImpact{}, sessions, target, nil)
			if err != nil {
				t.Fatal(err)
			}
			if plan.HasConflicts {
				t.Fatalf("unexpected fixture ambiguity: %+v", plan.Rows)
			}
			moved, stayed := 0, 0
			for i, row := range plan.Rows {
				title := source.Release.Tracklist.Value[i].Title.Value
				if row.Stay {
					stayed++
					if title != "White Love" && title != "深愛" {
						t.Fatalf("unexpected unmapped %s", title)
					}
					if row.After["track.year"] != row.Before["track.year"] {
						t.Fatal("unmapped year changed")
					}
					continue
				}
				moved++
				for _, entry := range target.Release.Tracklist.Value {
					if entry.Track.Value.Ref == row.TargetRef {
						if row.After["track.trackNumber"].ValueJSON != strconv.Itoa(entry.TrackNumber) {
							t.Fatal("wrong target position")
						}
						if entry.Recording.Value != source.Release.Tracklist.Value[i].Recording.Value {
							t.Fatal("used array position instead of recording")
						}
					}
				}
			}
			wantStay := 0
			if direction == 1 {
				wantStay = 1
			}
			if moved != 14 || stayed != wantStay {
				t.Fatalf("moved=%d stayed=%d", moved, stayed)
			}
		}
	}
}
func TestAlbumMappingAmbiguityOwnershipAndTextAreExplicit(t *testing.T) {
	identity := domain.Identity{Provider: "test", Ref: "recording"}
	track := domain.TrackSnapshot{Identity: domain.Identity{Provider: "test", Ref: "source"}, Recording: domain.Known(identity), Title: domain.Known("Song"), DurationMS: domain.Known(10000)}
	body, _ := json.Marshal(domain.Snapshot{Track: &track})
	source := &domain.EditorSession{TrackID: 1, Current: domain.EditFields{"track.year": {State: domain.Value, ValueJSON: "2000", Mode: domain.User}}, SnapshotJSON: string(body)}
	a := domain.TracklistEntry{Track: domain.Known(domain.Identity{Provider: "test", Ref: "a"}), Recording: domain.Known(identity), DiscNumber: 1, TrackNumber: 1, Title: domain.Known("Song"), DurationMS: domain.Known(12000)}
	b := a
	b.Track = domain.Known(domain.Identity{Provider: "test", Ref: "b"})
	b.DiscNumber = 2
	target := domain.Snapshot{Release: &domain.ReleaseSnapshot{Identity: domain.Identity{Provider: "test", Ref: "target"}, Date: domain.Known("2012"), Tracklist: domain.Known([]domain.TracklistEntry{a, b}), TracklistCompleteness: domain.Complete}}
	draft := domain.EditDraft{BindingAction: "set"}
	plan, err := planAlbumRows(draft, &domain.EditImpact{}, []*domain.EditorSession{source}, target, nil)
	if err != nil || !plan.HasConflicts || len(plan.Rows[0].Choices) != 2 {
		t.Fatalf("duplicate recording: %+v %v", plan, err)
	}
	draft.Mappings = []domain.EditMapping{{TrackID: 1, TargetRef: "b"}}
	plan, err = planAlbumRows(draft, &domain.EditImpact{}, []*domain.EditorSession{source}, target, map[string]int64{"b": 9})
	if err != nil || !plan.HasConflicts {
		t.Fatalf("owned target: %+v %v", plan, err)
	}
	plan, err = planAlbumRows(draft, &domain.EditImpact{}, []*domain.EditorSession{source}, target, nil)
	if err != nil || plan.HasConflicts || plan.Rows[0].After["track.discNumber"].ValueJSON != "2" || plan.Rows[0].After["track.year"].ValueJSON != "2000" {
		t.Fatalf("explicit mapping/user protection: %+v %v", plan, err)
	}
	track.Recording = domain.Missing[domain.Identity]()
	body, _ = json.Marshal(domain.Snapshot{Track: &track})
	source.SnapshotJSON = string(body)
	draft.Mappings = nil
	plan, err = planAlbumRows(draft, &domain.EditImpact{}, []*domain.EditorSession{source}, target, nil)
	if err != nil || !plan.HasConflicts || plan.Rows[0].Evidence != "文本与时长建议" {
		t.Fatalf("text auto-matched: %+v %v", plan, err)
	}
}
