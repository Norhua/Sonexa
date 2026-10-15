package app

import (
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func TestCVDetailDisplaysEachWholeCreditWithoutChangingStoredRoles(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	credit := "角色A(CV.声优) feat. Guest & 角色B(CV.声优) & 角色A(CV.声优)"
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "Song", Credit: &credit})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := app.store.TrackRevision(id)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := app.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Participants) != 1 || detail.Participants[0].Role != "vocal" {
		t.Fatalf("participants = %+v", detail.Participants)
	}
	var names []string
	for _, artist := range detail.Participants[0].Artists {
		names = append(names, artist.Name)
		if artist.Identity != nil || deref(artist.Source) != store.CVCreditSource {
			t.Fatalf("display identity/source = %+v", artist)
		}
		header, err := app.GetArtistHeader(artist.ArtistID)
		if err != nil || header.Title != artist.Name {
			t.Fatalf("display link = %+v, %v", header, err)
		}
		tracks, err := app.ListTracks(store.TrackFilter{ArtistID: artist.ArtistID})
		if err != nil || len(tracks.Tracks) != 1 || tracks.Tracks[0].ID != id {
			t.Fatalf("display link tracks = %+v, %v", tracks, err)
		}
	}
	if !reflect.DeepEqual(names, []string{"角色A(CV.声优)", "角色B(CV.声优)"}) {
		t.Fatalf("whole names must exclude ordinary guests and duplicates: %v", names)
	}
	var roleCount int
	if err := app.store.DB().QueryRow("SELECT COUNT(*) FROM track_artist WHERE track_id=? AND role='vocal'", id).Scan(&roleCount); err != nil || roleCount != 1 {
		t.Fatalf("display wrote extra vocal roles: %d %v", roleCount, err)
	}
	after, err := app.store.TrackRevision(id)
	if err != nil || after != revision {
		t.Fatalf("read changed revision: %d -> %d, %v", revision, after, err)
	}
}

func TestCVDisplayPreservesExplicitRolesAndInvalidCreditLinks(t *testing.T) {
	text := "角色(CV.声优)"
	start, end := 0, len(text)
	actorName := "声优"
	entries := []store.CreditEntry{
		{ArtistID: 10, Position: 0, CreditedName: &text, SpanStart: &start, SpanEnd: &end},
		{ArtistID: 20, Position: 1, CreditedName: &actorName},
	}
	identity := &domain.Identity{Provider: "musicbrainz", Ref: "actor"}
	for _, source := range []string{"musicbrainz", "user", ""} {
		artist := Participant{ArtistID: 20, Name: actorName, Identity: identity, Attributes: []string{"backing vocals"}}
		if source != "" {
			artist.Source = &source
		}
		groups := []ParticipantGroup{{Role: "vocal", Artists: []Participant{artist}}}
		got := displayCVParticipants(groups, entries, text)
		if !reflect.DeepEqual(got[0].Artists, []Participant{artist}) {
			t.Fatalf("changed explicit role from %q: %+v", source, got)
		}
	}
	source := store.CVCreditSource
	artist := Participant{ArtistID: 20, Name: actorName, Source: &source, Identity: identity}
	for _, tc := range []struct {
		name    string
		text    string
		entries []store.CreditEntry
		role    string
	}{
		{"changed text", "其他(CV.演员)", entries, "vocal"},
		{"missing actor member", text, entries[:1], "vocal"},
		{"unrelated actor identity", text, []store.CreditEntry{entries[0], {ArtistID: 30, Position: 1, CreditedName: &actorName}}, "vocal"},
		{"other role", text, entries, "composer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groups := []ParticipantGroup{{Role: tc.role, Artists: []Participant{artist}}}
			got := displayCVParticipants(groups, tc.entries, tc.text)
			if !reflect.DeepEqual(got[0].Artists, []Participant{artist}) {
				t.Fatalf("must keep unmatched/other role: %+v", got)
			}
		})
	}
}
