package playlists

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

type waitingPlaylist struct {
	playlistOnly
	started chan<- provider.ProviderID
	release <-chan struct{}
}

func (p *waitingPlaylist) FetchPlaylist(ctx context.Context, ref Ref) (*Snapshot, error) {
	p.started <- p.ID()
	select {
	case <-p.release:
		s := validSnapshot()
		s.Ref = ref
		return s, nil
	case <-ctx.Done():
		return nil, Failure(Cancelled, "cancelled")
	}
}
func TestFetchPoolConcurrencyAndClose(t *testing.T) {
	f := NewFetcher()
	defer f.Close()
	started := make(chan provider.ProviderID, 5)
	release := make(chan struct{})
	var work sync.WaitGroup
	for _, id := range []provider.ProviderID{"a", "a", "b", "c"} {
		work.Add(1)
		go func(id provider.ProviderID) {
			defer work.Done()
			p := &waitingPlaylist{playlistOnly: playlistOnly{id: id}, started: started, release: release}
			_, _ = f.Fetch(context.Background(), p, Ref{Provider: id, ID: "list"})
		}(id)
	}
	first, second := <-started, <-started
	if first == second {
		t.Fatal("same source ran concurrently")
	}
	select {
	case id := <-started:
		t.Fatal("more than two fetches", id)
	case <-time.After(20 * time.Millisecond):
	}
	f.Close()
	work.Wait()
	if _, err := f.Fetch(context.Background(), &playlistOnly{id: "a"}, Ref{Provider: "a", ID: "list"}); Code(err) != Cancelled {
		t.Fatal(err)
	}
}
