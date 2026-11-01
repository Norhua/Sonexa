package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type youImportProvider struct {
	provider.MetadataProvider
	candidates []provider.Candidate
	calls      atomic.Int32
}

func (p *youImportProvider) SearchTracks(context.Context, provider.Query) ([]provider.Candidate, error) {
	p.calls.Add(1)
	return p.candidates, nil
}
func youIdentityCandidates(t *testing.T) (provider.Candidate, provider.Candidate) {
	t.Helper()
	body, err := os.ReadFile("../../testdata/track-identity/you-identities.json")
	if err != nil {
		t.Fatal(err)
	}
	type target struct{ Track, Title, Album string }
	var fixture struct {
		Recording, ISRC, Artist string
		Bonus, Collection       target
	}
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	makeCandidate := func(v target, key string) provider.Candidate {
		c := fullCandidate("you-" + key)
		c.MatchRef, c.Title, c.ArtistName, c.AlbumCredit, c.AlbumTitle = v.Track, strPtr(v.Title), strPtr(fixture.Artist), strPtr(fixture.Artist), strPtr(v.Album)
		c.ReleaseRef, c.ReleaseGroupRef = strPtr(fixtureMBID("you-release-"+key)), strPtr(fixtureMBID("you-group-"+key))
		c.ISRC, c.RecordingRef = strPtr(fixture.ISRC), strPtr(fixture.Recording)
		c.Identifiers = []domain.Identifier{{Kind: "isrc", Value: fixture.ISRC}}
		return c
	}
	return makeCandidate(fixture.Bonus, "bonus"), makeCandidate(fixture.Collection, "collection")
}
func TestYouRealTrackIdentitiesAcrossReviewSearchAndImport(t *testing.T) {
	for _, mode := range []string{"review", "search", "explicit_import", "no_album"} {
		t.Run(mode, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			a.enricher.Pause()
			if err := a.coverSvc.AttachLibrary(a.store); err != nil {
				t.Fatal(err)
			}
			defer a.coverSvc.Close()
			bonus, collection := youIdentityCandidates(t)
			base, _ := a.registry.Get(provider.MusicBrainz)
			p := &youImportProvider{MetadataProvider: base, candidates: []provider.Candidate{collection, bonus}}
			a.registry.Register(p)
			a.rebuildRunner(a.settings.Get())
			first, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: bonus}))
			if err != nil {
				t.Fatal(err)
			}
			track, _ := a.store.GetTrack(first)
			if _, err := a.coverSvc.Library().ImportLocal(*track.ReleaseID, coverPNG(t, 120, 120), 0); err != nil {
				t.Fatal(err)
			}
			playlist, err := a.CreateCollection("preserve", "playlist", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.AddToCollection(playlist, []int64{first}); err != nil {
				t.Fatal(err)
			}
			before, _ := a.store.GetTrack(first)
			coverBefore, _ := a.store.GetCoverSelection(*track.ReleaseID)
			var snapshotBefore string
			if err := a.store.DB().QueryRow("SELECT body FROM track_snapshot WHERE track_id=?", first).Scan(&snapshotBefore); err != nil {
				t.Fatal(err)
			}
			var second int64
			switch mode {
			case "review":
				id := enqueueOneItem(t, a, "you", *collection.ArtistName, *collection.AlbumTitle)
				if err := a.store.MarkReview(id, store.ReviewAmbiguous, "[]", "[]"); err != nil {
					t.Fatal(err)
				}
				scope, err := a.GetReviewProblemScope(mustProblemID(t, a, id))
				if err != nil {
					t.Fatal(err)
				}
				input, err := a.GetReviewAdmissionScope(scope)
				if err != nil {
					t.Fatal(err)
				}
				plan, err := a.PrepareProblemAdmission(rowFrom(matching.ScoredCandidate{Candidate: collection}), *input)
				if err != nil {
					t.Fatal(err)
				}
				result, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{})
				if err != nil {
					t.Fatal(err)
				}
				second = result.TrackID
			case "search":
				response, err := a.SearchUpstream("you", *collection.ArtistName, *collection.AlbumTitle, []string{"musicbrainz"}, "")
				if err != nil {
					t.Fatal(err)
				}
				for _, group := range response.ByProvider {
					for _, row := range group.Rows {
						if row.MatchRef == collection.MatchRef {
							second, err = a.AddToLibrary(row)
							if err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			default:
				album := *collection.AlbumTitle
				if mode == "no_album" {
					album = ""
				}
				batch := submitYouInput(t, a, *collection.ArtistName, album)
				stats, err := a.store.Stats(batch)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "no_album" {
					if stats.Review != 1 || stats.Imported != 0 || stats.Skipped != 0 {
						t.Fatal("ambiguous input inherited ownership", stats)
					}
				} else {
					if stats.Imported != 1 || stats.Skipped != 0 {
						t.Fatal("explicit second album swallowed", stats)
					}
					second, err = a.store.FindByMatchRef("musicbrainz", collection.MatchRef)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode != "no_album" && (second == 0 || second == first) {
				t.Fatal("second track missing", first, second)
			}
			batch := submitYouInput(t, a, *bonus.ArtistName, "冴えない彼女の育てかた 第4巻 特典C...")
			stats, err := a.store.Stats(batch)
			if err != nil || stats.ExistingSkipped != 1 || stats.Imported != 0 || stats.Review != 0 {
				t.Fatal("truncated bonus did not skip", stats, err)
			}
			var skippedID int64
			if err := a.store.DB().QueryRow("SELECT track_id FROM import_item WHERE batch_id=?", batch).Scan(&skippedID); err != nil || skippedID != first {
				t.Fatal(skippedID, err)
			}
			after, _ := a.store.GetTrack(first)
			coverAfter, _ := a.store.GetCoverSelection(*track.ReleaseID)
			var snapshotAfter string
			if err := a.store.DB().QueryRow("SELECT body FROM track_snapshot WHERE track_id=?", first).Scan(&snapshotAfter); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(coverBefore, coverAfter) || !reflect.DeepEqual(snapshotBefore, snapshotAfter) {
				t.Fatal("old values/cover/source overwritten")
			}
			members, err := a.store.ListTracks(store.TrackFilter{CollectionID: playlist})
			if err != nil || len(members) != 1 || members[0].ID != first {
				t.Fatal("playlist changed", members, err)
			}
			want := 2
			if mode == "no_album" {
				want = 1
			}
			if count, err := a.store.CountTracks(store.TrackFilter{}); err != nil || count != want {
				t.Fatal(count, err)
			}
			wantCalls := int32(2)
			if mode == "review" {
				wantCalls = 1
			}
			if p.calls.Load() != wantCalls {
				t.Fatal("unexpected necessary requests", p.calls.Load(), wantCalls)
			}
			problems, err := a.GetReviewProblemStats()
			wantProblems := 0
			if mode == "no_album" {
				wantProblems = 1
			}
			if err != nil || problems.ReviewProblems != wantProblems {
				t.Fatal(problems, err)
			}
			if second != 0 {
				binding, err := a.store.FindExactBinding(domain.Identity{Provider: provider.MusicBrainz, Ref: collection.MatchRef})
				if err != nil || binding == nil || binding.TrackID != second || binding.Release.Ref != *collection.ReleaseRef {
					t.Fatal(binding, err)
				}
			}
			t.Logf("%s: tracks=%d bonus_skips=1 problems=%d false_auto_actions=0 search_calls=%d", mode, want, wantProblems, p.calls.Load())
		})
	}
}
func mustProblemID(t *testing.T, a *App, itemID int64) int64 {
	t.Helper()
	scope, err := a.store.ItemProblemScope(itemID)
	if err != nil {
		t.Fatal(err)
	}
	return scope.ProblemID
}
func submitYouInput(t *testing.T, a *App, artist, album string) int64 {
	t.Helper()
	path := filepath.Join(t.TempDir(), "you.tsv")
	if err := os.WriteFile(path, []byte("title\tartist\talbum\nyou\t"+artist+"\t"+album+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	batch, err := a.SubmitImport(path, "", []string{"title", "artist", "album"}, "UTF-8", "\t", true)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "you import finished", func() bool { return !a.runner.IsRunning(batch) })
	return batch
}
