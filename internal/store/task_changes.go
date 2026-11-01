package store

// SetTaskChanged installs a post-commit callback. It carries an integer
// revision even when the count did not change (query, membership, split...).
func (s *Store) SetTaskChanged(fn func(int64)) {
	s.taskChangedMu.Lock()
	s.taskChanged = fn
	s.taskChangedMu.Unlock()
}
func (s *Store) taskCallback() func(int64) {
	s.taskChangedMu.RLock()
	defer s.taskChangedMu.RUnlock()
	return s.taskChanged
}
