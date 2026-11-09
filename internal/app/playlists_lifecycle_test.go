package app

import (
	"context"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
)

type shutdownPlaylist struct {
	started chan struct{}
	stopped chan struct{}
}

func (*shutdownPlaylist) ID() provider.ProviderID { return "shutdown-stub" }
func (*shutdownPlaylist) DisplayName() string     { return "shutdown stub" }
func (*shutdownPlaylist) PlaylistAvailability() playlists.Availability {
	return playlists.Availability{Available: true}
}
func (p *shutdownPlaylist) ParsePlaylist(id string) (playlists.Ref, error) {
	return playlists.Ref{Provider: p.ID(), ID: id}, nil
}
func (p *shutdownPlaylist) FetchPlaylist(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
	close(p.started)
	<-ctx.Done()
	close(p.stopped)
	return nil, ctx.Err()
}

func TestShutdownWaitsForPlaylistFetches(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	p := &shutdownPlaylist{started: make(chan struct{}), stopped: make(chan struct{})}
	finished := make(chan struct{})
	go func() {
		_, _ = a.playlistFetcher.Fetch(context.Background(), p, playlists.Ref{Provider: p.ID(), ID: "list"})
		close(finished)
	}()
	<-p.started
	a.shutdown(context.Background())
	select {
	case <-p.stopped:
	default:
		t.Fatal("shutdown did not cancel playlist provider")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("shutdown left playlist work")
	}
}
