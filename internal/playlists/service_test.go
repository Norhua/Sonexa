package playlists

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

type planProvider struct {
	snapshot Snapshot
	started  chan struct{}
	wait     bool
	calls    int
	mu       sync.Mutex
}

func (p *planProvider) ID() provider.ProviderID            { return p.snapshot.Ref.Provider }
func (p *planProvider) DisplayName() string                { return "Playlist only" }
func (p *planProvider) PlaylistAvailability() Availability { return Availability{Available: true} }
func (p *planProvider) ParsePlaylist(input string) (Ref, error) {
	return Ref{Provider: p.ID(), ID: input}, nil
}
func (p *planProvider) FetchPlaylist(ctx context.Context, ref Ref) (*Snapshot, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.started != nil {
		p.started <- struct{}{}
	}
	if p.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s := p.snapshot
	s.Ref = ref
	return &s, nil
}

type memoryRepository struct {
	state  *State
	writes int
	result *CommitResult
	hash   string
}

func (r *memoryRepository) DeleteSyncedPlaylist(id int64) error {
	r.state = nil
	return nil
}

func (r *memoryRepository) ReadPlaylistState(id int64) (*State, error) { return r.state, nil }
func (r *memoryRepository) FindSyncedPlaylist(ref Ref) (*Binding, error) {
	if r.state != nil && r.state.Binding.Ref == ref {
		return &r.state.Binding, nil
	}
	return nil, nil
}
func (r *memoryRepository) ApplyPlaylist(ctx context.Context, p Plan, ids []int64) (*CommitResult, error) {
	r.writes++
	r.hash = p.Hash
	r.result = &CommitResult{PlaylistID: 1, Revision: 1}
	return r.result, nil
}
func (r *memoryRepository) PlaylistOperationResult(id, hash string) (*CommitResult, error) {
	if r.result != nil && hash == r.hash {
		return r.result, nil
	}
	return nil, nil
}

type planClock struct{ now time.Time }

func (c *planClock) Now() time.Time { return c.now }
func serviceFixture(t *testing.T, p *planProvider, r *memoryRepository, clock Clock) *Service {
	t.Helper()
	registry, err := NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := NewFetcher()
	s, err := NewService(registry, r, fetcher, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); fetcher.Close() })
	return s
}
func TestPlanPreparationIsDetachedBoundedAndCancelable(t *testing.T) {
	p := &planProvider{snapshot: diffSnapshot("one")}
	r := &memoryRepository{}
	clock := &planClock{now: time.Now()}
	s := serviceFixture(t, p, r, clock)
	preview, err := s.PrepareBinding(context.Background(), "operation", "only-playlist", "list", " alias ")
	if err != nil || r.writes != 0 || preview.Alias != "alias" {
		t.Fatal(preview, err, r.writes)
	}
	preview.Difference.Added[0].Entry.TrackRef = "forged"
	if s.plans[preview.ID].plan.Difference.Added[0].Entry.TrackRef != "one" {
		t.Fatal("caller mutated retained plan")
	}
	if _, err := s.PrepareBinding(context.Background(), "duplicate", "only-playlist", "list", ""); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := s.Commit(context.Background(), preview.ID, "forged", nil); !errors.Is(err, ErrPlanStale) {
		t.Fatal(err)
	}
	s.Cancel(preview.ID)
	if _, err := s.Commit(context.Background(), preview.ID, preview.Hash, nil); !errors.Is(err, ErrPlanStale) || r.writes != 0 {
		t.Fatal(err)
	}
	preview, err = s.PrepareBinding(context.Background(), "empty", "only-playlist", "empty", "")
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(PlanTTL)
	if _, err := s.Commit(context.Background(), preview.ID, preview.Hash, nil); !errors.Is(err, ErrPlanStale) {
		t.Fatal(err)
	}
	if s.bytes != 0 || len(s.plans) != 0 {
		t.Fatal("expired plan retained")
	}
	for i := 0; i < MaxPlans; i++ {
		if _, err := s.PrepareBinding(context.Background(), strings.Repeat("o", i+1), "only-playlist", strings.Repeat("p", i+1), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PrepareBinding(context.Background(), "overflow", "only-playlist", "overflow", ""); Code(err) != Resource {
		t.Fatal(err)
	}
	if len(s.plans) != MaxPlans {
		t.Fatal("silently evicted existing plan")
	}
}
func TestPlanSuccessfulEmptyDuplicateAndIdempotentCommit(t *testing.T) {
	p := &planProvider{snapshot: diffSnapshot()}
	r := &memoryRepository{}
	s := serviceFixture(t, p, r, nil)
	preview, err := s.PrepareBinding(context.Background(), "empty", "only-playlist", "list", "")
	if err != nil || len(preview.Difference.Added) != 0 {
		t.Fatal(preview, err)
	}
	first, err := s.Commit(context.Background(), preview.ID, preview.Hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Commit(context.Background(), preview.ID, preview.Hash, nil)
	if err != nil || *first != *second || r.writes != 1 {
		t.Fatal(first, second, err)
	}
	r.state = diffState(p.snapshot)
	if _, err := s.PrepareBinding(context.Background(), "duplicate", "only-playlist", "list", ""); err == nil {
		t.Fatal("duplicate binding allowed")
	}
}
func TestPlanCancellationAndCloseRejectLateFetch(t *testing.T) {
	p := &planProvider{snapshot: diffSnapshot(), started: make(chan struct{}, 1), wait: true}
	r := &memoryRepository{}
	s := serviceFixture(t, p, r, nil)
	result := make(chan error, 1)
	go func() {
		_, err := s.PrepareBinding(context.Background(), "cancel-me", "only-playlist", "list", "")
		result <- err
	}()
	<-p.started
	s.Cancel("cancel-me")
	if err := <-result; err == nil {
		t.Fatal("cancelled fetch succeeded")
	}
	go func() {
		_, err := s.PrepareBinding(context.Background(), "close-me", "only-playlist", "list", "")
		result <- err
	}()
	<-p.started
	s.Close()
	if err := <-result; err == nil {
		t.Fatal("closed fetch succeeded")
	}
	if len(s.plans) != 0 || len(s.operations) != 0 || s.bytes != 0 || r.writes != 0 {
		t.Fatal("resources retained")
	}
	if _, err := s.PrepareBinding(context.Background(), "closed", "only-playlist", "other", ""); Code(err) != Cancelled {
		t.Fatal(err)
	}
}
func TestPlanMemoryLimitDoesNotTruncate(t *testing.T) {
	p := &planProvider{snapshot: diffSnapshot("one")}
	r := &memoryRepository{}
	s := serviceFixture(t, p, r, nil)
	// Reserve the existing budget without allocating tens of MiB in every test.
	s.bytes = MaxPlanBytes
	if _, err := s.PrepareBinding(context.Background(), "bytes", "only-playlist", "list", ""); Code(err) != Resource {
		t.Fatal(err)
	}
	if len(s.plans) != 0 || len(s.keys) != 0 || r.writes != 0 {
		t.Fatal("over-limit result retained")
	}
	s.bytes = 0
}
