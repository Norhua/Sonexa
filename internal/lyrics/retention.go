package lyrics

import (
	"errors"
	"sync"
)

var ErrRetentionLimit = errors.New("歌词会话内存已达上限，请关闭旧搜索后重试")

// RetentionBudget counts all retained document projections, not wire size.
// Sessions reject new documents instead of evicting an acknowledged version.
// Callers serialize their session accounting; this lock covers all sessions.
type RetentionBudget struct {
	mu                               sync.Mutex
	sessionLimit, processLimit, used int
}

func NewRetentionBudget(sessionLimit, processLimit int) *RetentionBudget {
	return &RetentionBudget{sessionLimit: sessionLimit, processLimit: processLimit}
}

func (b *RetentionBudget) Replace(sessionBytes, oldBytes, newBytes int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delta := newBytes - oldBytes
	if sessionBytes+delta > b.sessionLimit || b.used+delta > b.processLimit {
		return ErrRetentionLimit
	}
	b.used += delta
	return nil
}

func (b *RetentionBudget) Release(bytes int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used -= bytes
}

func (b *RetentionBudget) Used() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}
