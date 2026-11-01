package app

import (
	"context"
	"log"
	"sync"

	"github.com/norhua/sonexa/internal/library"
)

// reviewWorker serializes bounded local passes. The channel coalesces wakes;
// durable dirty/hint rows, not the channel, are the source of pending work.
type reviewWorker struct {
	wake   chan struct{}
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (w *reviewWorker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}
func (w *reviewWorker) stop() { w.once.Do(w.cancel); <-w.done }
func (a *App) startReviewWorker(ctx context.Context) error {
	service, err := a.libraryService()
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.reviewWorker != nil {
		a.mu.Unlock()
		return nil
	}
	life, cancel := context.WithCancel(ctx)
	w := &reviewWorker{wake: make(chan struct{}, 1), cancel: cancel, done: make(chan struct{})}
	a.reviewWorker = w
	a.mu.Unlock()
	go func() { defer close(w.done); a.runReviewWorker(life, w, service) }()
	w.signal()
	return nil
}
func (a *App) wakeReviewWorker() {
	a.mu.RLock()
	w := a.reviewWorker
	a.mu.RUnlock()
	if w != nil {
		w.signal()
	}
}
func (a *App) runReviewWorker(ctx context.Context, w *reviewWorker, service *library.Service) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		}
		changed, err := a.store.ScheduleLibraryRechecks(ctx, 32)
		if err == nil {
			var processed int
			processed, err = service.RecheckPending(ctx, 16)
			if changed == 32 || processed == 16 {
				w.signal()
			}
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("复核问题重评失败: %v", err)
		}
	}
}
func (a *App) stopReviewWorker() {
	a.mu.RLock()
	w := a.reviewWorker
	a.mu.RUnlock()
	if w != nil {
		w.stop()
	}
}
