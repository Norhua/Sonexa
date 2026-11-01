package app

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/library"
	"github.com/norhua/sonexa/internal/provider"
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
	if err := a.store.RestoreProblemRechecks(ctx); err != nil {
		a.mu.Unlock()
		return err
	}
	life, cancel := context.WithCancel(ctx)
	w := &reviewWorker{wake: make(chan struct{}, 1), cancel: cancel, done: make(chan struct{})}
	a.reviewWorker = w
	a.store.SetReviewWake(w.signal)
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
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-w.wake:
		}
		changed, err := a.store.ScheduleLibraryRechecks(ctx, 32)
		if err == nil {
			var processed int
			processed, err = service.RecheckPending(ctx, 16)
			if err == nil {
				err = a.resumeRecheckNetwork(ctx)
			}
			if changed == 32 || processed == 16 {
				w.signal()
			}
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("复核问题重评失败: %v", err)
		}
		delay, deadlineErr := a.store.NextRecheckDelay(ctx)
		if err != nil || deadlineErr != nil {
			delay = time.Second
		}
		if delay <= 0 {
			delay = time.Hour
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}
func (a *App) resumeRecheckNetwork(ctx context.Context) error {
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil
	}
	a.mu.RLock()
	runner := a.runner
	a.mu.RUnlock()
	if runner == nil {
		return nil
	}
	if err := a.store.PruneRecheckNetwork(); err != nil {
		return err
	}
	batches, err := a.store.RecheckNetworkBatches(ctx)
	if err != nil {
		return err
	}
	for _, batch := range batches {
		requestCtx := provider.WithRequestGate(ctx, func() error {
			if gate := a.CheckOutbound(); !gate.Allowed {
				return fmt.Errorf("%s", gate.Reason)
			}
			return nil
		})
		if err := runner.Start(requestCtx, batch); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) stopReviewWorker() {
	a.mu.RLock()
	w := a.reviewWorker
	a.mu.RUnlock()
	if w != nil {
		a.store.SetReviewWake(nil)
		w.stop()
	}
}
