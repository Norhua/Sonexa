package store

// SetReviewWake installs a non-blocking lifecycle callback. It is a hint only;
// durable database work remains authoritative after lost wakes or restarts.
func (s *Store) SetReviewWake(fn func()) {
	s.reviewWakeMu.Lock()
	s.reviewWake = fn
	s.reviewWakeMu.Unlock()
}
func (s *Store) signalReviewWork() {
	s.reviewWakeMu.RLock()
	fn := s.reviewWake
	s.reviewWakeMu.RUnlock()
	if fn != nil {
		fn()
	}
}
