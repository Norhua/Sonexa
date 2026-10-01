package app

import (
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestAppAdmissionCannotBypassExtraReleaseConfirmation(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	a := fullCandidate("a1")
	b := fullCandidate("b1")
	firstRef, secondRef := "release-a", "release-b"
	a.ReleaseRef = &firstRef
	b.ReleaseRef = &secondRef
	artist := "Album Artist"
	a.AlbumCredit = &artist
	b.AlbumCredit = &artist
	p, err := app.PrepareAdmission(rowFrom(matching.ScoredCandidate{Candidate: a}), 0)
	if err != nil {
		t.Fatal(err)
	}
	first, err := app.CommitAdmission(p.ID, p.Hash, domain.AdmissionApproval{})
	if err != nil {
		t.Fatal(err)
	}
	p, err = app.PrepareAdmission(rowFrom(matching.ScoredCandidate{Candidate: b}), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.CommitAdmission(p.ID, p.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrConfirmationRequired) {
		t.Fatal("App bypassed confirmation", err)
	}
	approval, err := app.ConfirmReleaseAdditions(p.ID, p.Hash)
	if err != nil {
		t.Fatal(err)
	}
	result, err := app.CommitAdmission(p.ID, p.Hash, *approval)
	if err != nil || result.GroupID != first.GroupID || result.ReleaseID == first.ReleaseID {
		t.Fatal(result, err)
	}
}
