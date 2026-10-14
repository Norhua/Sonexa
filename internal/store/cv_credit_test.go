package store

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
)

func assertCVVocals(t *testing.T, s *Store, id int64, names []string, source string) {
	t.Helper()
	rows, err := s.db.Query(`SELECT a.name,ta.source FROM track_artist ta JOIN artist a ON a.id=ta.artist_id
		WHERE ta.track_id=? AND ta.role='vocal' ORDER BY ta.position`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name, actualSource string
		if err := rows.Scan(&name, &actualSource); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
		if actualSource != source {
			t.Fatalf("source = %s, want %s", actualSource, source)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, names) {
		t.Fatalf("vocals = %v, want %v", got, names)
	}
}

func TestCVCreditFallbackParsing(t *testing.T) {
	for _, credit := range []string{
		"角色(CV.声优)", "角色（ＣＶ：声优）", "角色【声優:声优】", "角色 (cv: 声优)",
		"角色(C.V.声优)", "角色(Vo.声优)", "角色(配音：声优)",
	} {
		t.Run(credit, func(t *testing.T) {
			s := openTestStore(t)
			id, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "Song", Credit: &credit})
			if err != nil {
				t.Fatal(err)
			}
			assertCVVocals(t, s, id, []string{"声优"}, CVCreditSource)
			artists, err := s.ListArtists()
			if err != nil || len(artists) != 2 {
				t.Fatalf("artists = %+v, %v", artists, err)
			}
			entries, err := s.GetTrackCredits(id)
			if err != nil || len(entries) != 2 || entries[0].SpanStart == nil || entries[1].SpanStart != nil || entries[1].SpanEnd != nil {
				t.Fatalf("CV link must stay whole without overlapping actor span: %+v, %v", entries, err)
			}
			for _, artist := range artists {
				tracks, err := s.ListTracks(TrackFilter{ArtistID: artist.ID})
				if err != nil || len(tracks) != 1 || tracks[0].ID != id || artist.TrackCount != 1 {
					t.Fatalf("artist tracks/count = %+v, %+v, %v", artist, tracks, err)
				}
			}
			session, err := s.ReadEditorSession(id)
			if err != nil || session.Current["track.roles.vocal"].Source != CVCreditSource {
				t.Fatalf("editor source = %+v, %v", session, err)
			}
		})
	}
	for _, credit := range []string{
		"Creator feat. Guest", "Band (US)", "角色(CV: Live Mix)", "角色(CV:)",
		"角色(CV: A & B)", "角色(CV: A)(CV: B)", "角色(CV: 声优", "(CV: 声优)",
	} {
		t.Run("no-fallback/"+credit, func(t *testing.T) {
			s := openTestStore(t)
			id, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "Song", Credit: &credit})
			if err != nil {
				t.Fatal(err)
			}
			assertCVVocals(t, s, id, nil, "")
		})
	}
}

func TestCVFallbackChangesWithCreditAndClearedBinding(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	snapshot.Track.Credit = domain.Known(domain.Credit{Text: "角色(CV.声优)"})
	snapshot.Track.Roles = nil
	applyTestSnapshot(t, s, id, snapshot)
	assertCVVocals(t, s, id, []string{"声优"}, CVCreditSource)
	if err := s.ClearProvenance(id); err != nil {
		t.Fatal(err)
	}
	assertCVVocals(t, s, id, []string{"声优"}, CVCreditSource)
	for _, text := range []string{"新角色(CV.新声优)", "普通署名", "角色(CV: Live Mix)", ""} {
		if err := s.UpdateTrackFields(id, nil, &text, true); err != nil {
			t.Fatal(err)
		}
		var vocals []string
		if text == "新角色(CV.新声优)" {
			vocals = []string{"新声优"}
		}
		assertCVVocals(t, s, id, vocals, CVCreditSource)
		artists, err := s.ListArtists()
		if err != nil {
			t.Fatal(err)
		}
		for _, artist := range artists {
			if artist.Name == "声优" || artist.Name == "角色(CV.声优)" {
				t.Fatalf("old CV artist remained: %+v", artists)
			}
		}
		hits, err := s.SearchTrackIDs("角色(CV.声优)", 10)
		if err != nil || len(hits) != 0 {
			t.Fatalf("stale credit in FTS: %v, %v", hits, err)
		}
	}
}

func TestCVFallbackYieldsToExplicitVocalsAndEmpty(t *testing.T) {
	for _, user := range []bool{false, true} {
		for _, empty := range []bool{false, true} {
			s := openTestStore(t)
			id, snapshot := snapshotTrack(t, s)
			snapshot.Track.Credit = domain.Known(domain.Credit{Text: "角色(CV.声优)"})
			snapshot.Track.Roles = nil
			applyTestSnapshot(t, s, id, snapshot)
			assertCVVocals(t, s, id, []string{"声优"}, CVCreditSource)
			roles := domain.Known([]domain.Role{{Artist: domain.Artist{Name: "实际演唱者"}, Role: "vocal", Attributes: []string{"backing vocals"}}})
			var want = []string{"实际演唱者"}
			if empty {
				roles, want = domain.Cleared[[]domain.Role](), nil
			}
			source := "test"
			if user {
				source = "user"
				if err := s.withTx(func(tx *sql.Tx) error {
					body, _ := json.Marshal(roles.Value)
					return writeEditorFieldsTx(tx, "track", id, domain.EditFields{"track.roles.vocal": {State: roles.State, ValueJSON: string(body)}})
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				snapshot.Track.Roles = map[string]domain.Field[[]domain.Role]{"vocal": roles}
				applyTestSnapshot(t, s, id, snapshot)
			}
			assertCVVocals(t, s, id, want, source)
			// 缺关系的再次补全和手改署名均不能盖过既有事实/用户空值。
			snapshot.Track.Roles = nil
			applyTestSnapshot(t, s, id, snapshot)
			text := "新角色(CV.新声优)"
			if err := s.UpdateTrackFields(id, nil, &text, true); err != nil {
				t.Fatal(err)
			}
			assertCVVocals(t, s, id, want, source)
			entries, err := s.GetTrackCredits(id)
			if err != nil || len(entries) != 2 || deref(entries[1].CreditedName) != "新声优" {
				t.Fatalf("independent actor association lost: %+v %v", entries, err)
			}
		}
	}
}

func TestCVFallbackSurvivesCopiedEditorFields(t *testing.T) {
	s := openTestStore(t)
	id, snapshot := snapshotTrack(t, s)
	snapshot.Track.Credit = domain.Known(domain.Credit{Text: "角色(CV.声优)"})
	snapshot.Track.Roles = nil
	applyTestSnapshot(t, s, id, snapshot)
	session, err := s.ReadEditorSession(id)
	if err != nil {
		t.Fatal(err)
	}
	// 专辑迁入会提交完整的当前字段，且可能同时改曲目署名。
	session.Current["track.credit"] = domain.EditValue{State: domain.Value, ValueJSON: `"新角色(CV.新声优)"`, Mode: domain.User, Source: "user"}
	if err := s.withTx(func(tx *sql.Tx) error {
		if err := clearTrackBindingTx(tx, id); err != nil {
			return err
		}
		return writeEditorFieldsTx(tx, "track", id, session.Current)
	}); err != nil {
		t.Fatal(err)
	}
	assertCVVocals(t, s, id, []string{"新声优"}, CVCreditSource)
}

func TestCVActorIdentityBoundaries(t *testing.T) {
	const text = "角色(CV.声优)"
	whole := domain.CreditMember{Artist: domain.Artist{Name: text}, CreditedName: text, Start: 0, End: len(text)}
	character := domain.CreditMember{Artist: domain.Artist{Name: "角色", Type: "character", Identity: &domain.Identity{Provider: "test", Ref: "character"}}, CreditedName: "角色", Start: 0, End: len("角色")}
	actor := domain.CreditMember{Artist: domain.Artist{Name: "声优", Type: "person", Identity: &domain.Identity{Provider: "test", Ref: "actor"}}, CreditedName: "声优", Start: len("角色(CV."), End: len(text) - 1}
	for _, parts := range [][]domain.CreditMember{
		{character, actor},
		{{Artist: actor.Artist, CreditedName: text, Start: 0, End: len(text)}},
	} {
		got := cvCreditActor(domain.Credit{Text: text, Members: parts}, whole)
		if got == nil || got.Identity == nil || got.Identity.Ref != "actor" {
			t.Fatalf("actor source identity lost: %+v", got)
		}
	}
	for _, part := range []domain.CreditMember{
		{Artist: character.Artist, CreditedName: text, Start: 0, End: len(text)},
		{Artist: actor.Artist, CreditedName: "声优", Start: 0, End: len("声优")}, // 文本与区间不符
	} {
		got := cvCreditActor(domain.Credit{Text: text, Members: []domain.CreditMember{part}}, whole)
		if got == nil || got.Name != "声优" || got.Identity != nil {
			t.Fatalf("borrowed invalid identity: %+v", got)
		}
	}
	conflict := actor
	conflict.Artist.Identity = &domain.Identity{Provider: "test", Ref: "other-actor"}
	if got := cvCreditActor(domain.Credit{Text: text, Members: []domain.CreditMember{actor, conflict}}, whole); got != nil {
		t.Fatalf("ambiguous upstream actors: %+v", got)
	}
}

func TestCVSameActorIsNotCountedTwice(t *testing.T) {
	s := openTestStore(t)
	credit := "角色A(CV.声优) feat. 角色B(CV.声优) & 声优"
	id, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "Song", Credit: &credit})
	if err != nil {
		t.Fatal(err)
	}
	assertCVVocals(t, s, id, []string{"声优"}, CVCreditSource)
	artists, err := s.ListArtists()
	if err != nil || len(artists) != 3 {
		t.Fatalf("duplicate actor: %+v, %v", artists, err)
	}
	for _, artist := range artists {
		if artist.TrackCount != 1 {
			t.Fatalf("duplicate track count: %+v", artist)
		}
	}
}
