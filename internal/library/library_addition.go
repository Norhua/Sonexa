package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/norhua/sonexa/internal/store"
)

type LibraryAdditionPlan struct {
	ID               string  `json:"id"`
	Hash             string  `json:"hash"`
	ExpiresAt        string  `json:"expiresAt"`
	WillAdd          []int64 `json:"willAdd"`
	AlreadyInLibrary []int64 `json:"alreadyInLibrary"`
}
type retainedAddition struct {
	ids     []int64
	hash    string
	expires time.Time
}

func (s *Service) PrepareLibraryAddition(ctx context.Context, ids []int64) (*LibraryAdditionPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > 20000 {
		return nil, store.ErrPlanStale
	}
	preview, err := s.store.PreviewLibraryAddition(ids)
	if err != nil {
		return nil, err
	}
	canonical := append(append([]int64{}, preview.WillAdd...), preview.AlreadyInLibrary...)
	sort.Slice(canonical, func(i, j int) bool { return canonical[i] < canonical[j] })
	id, err := token()
	if err != nil {
		return nil, err
	}
	id = "library-add-" + id
	expires := s.clock.Now().Add(PlanTTL)
	raw, err := json.Marshal(struct {
		ID      string
		IDs     []int64
		Expires time.Time
	}{id, canonical, expires})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.additions == nil {
		s.additions = map[string]retainedAddition{}
	}
	for id, p := range s.additions {
		if !s.clock.Now().Before(p.expires) {
			delete(s.additions, id)
		}
	}
	if len(s.additions) >= 8 {
		return nil, store.ErrPlanStale
	}
	s.additions[id] = retainedAddition{ids: canonical, hash: hash, expires: expires}
	return &LibraryAdditionPlan{ID: id, Hash: hash, ExpiresAt: expires.UTC().Format(time.RFC3339), WillAdd: preview.WillAdd, AlreadyInLibrary: preview.AlreadyInLibrary}, nil
}
func (s *Service) CommitLibraryAddition(ctx context.Context, id, hash string) (*store.LibraryAddition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.additions[id]
	if !ok || p.hash != hash || !s.clock.Now().Before(p.expires) {
		return nil, store.ErrPlanStale
	}
	return s.store.CommitLibraryAddition(ctx, id, hash, p.ids)
}
func (s *Service) CancelLibraryAddition(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.additions, id)
}
