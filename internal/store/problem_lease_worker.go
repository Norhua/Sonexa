package store

import (
	"context"
	"time"
)

// RunProblemLeaseReaper is lifecycle-owned. It only expires protection and
// queues coalesced hints; it does not read UI data or perform network matching.
func (s *Store) RunProblemLeaseReaper(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.ReapProblemLeases(100); err != nil && report != nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
