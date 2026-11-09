package playlists

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

var (
	ErrPlanStale = errors.New("playlist_plan_stale: 同步计划已失效，请重新读取")
	ErrBusy      = errors.New("playlist_busy: 此歌单已有读取或待确认计划")
)

type AlreadyBoundError struct{ PlaylistID int64 }

func (e *AlreadyBoundError) Error() string { return "该歌单已绑定" }

const PlanTTL = 15 * time.Minute
const MaxPlans = 8
const MaxPlanBytes = 64 << 20

type Plan struct {
	ID           string     `json:"id"`
	Hash         string     `json:"hash"`
	PlaylistID   int64      `json:"playlistId"`
	BaseRevision int64      `json:"baseRevision"`
	Alias        string     `json:"alias"`
	Snapshot     Snapshot   `json:"snapshot"`
	Difference   Difference `json:"difference"`
	ExpiresAt    time.Time  `json:"expiresAt"`
}

// Preview excludes authoritative raw snapshots. Only the server retains Plan.
type Preview struct {
	ID         string     `json:"id"`
	Hash       string     `json:"hash"`
	PlaylistID int64      `json:"playlistId"`
	Name       string     `json:"name"`
	Alias      string     `json:"alias"`
	ExpiresAt  string     `json:"expiresAt"`
	Difference Difference `json:"difference"`
}
type CommitResult struct {
	PlaylistID int64 `json:"playlistId"`
	Revision   int64 `json:"revision"`
	BatchID    int64 `json:"batchId"`
}

// Repository is owned by the consumer. No platform protocol reaches storage.
type Repository interface {
	ReadPlaylistState(int64) (*State, error)
	FindSyncedPlaylist(Ref) (*Binding, error)
	ApplyPlaylist(context.Context, Plan, []int64) (*CommitResult, error)
	PlaylistOperationResult(string, string) (*CommitResult, error)
	DeleteSyncedPlaylist(int64) error
}
type Clock interface{ Now() time.Time }
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

type retainedPlan struct {
	plan        Plan
	key         string
	operationID string
	bytes       int
}
type activeOperation struct {
	key    string
	cancel context.CancelFunc
}
type Service struct {
	mu         sync.Mutex
	registry   *Registry
	repository Repository
	fetcher    *Fetcher
	clock      Clock
	closed     bool
	life       context.Context
	cancel     context.CancelFunc
	work       sync.WaitGroup
	operations map[string]activeOperation
	plans      map[string]retainedPlan
	keys       map[string]bool
	bytes      int
}

func NewService(registry *Registry, repository Repository, fetcher *Fetcher, clock Clock) (*Service, error) {
	if registry == nil || repository == nil || fetcher == nil {
		return nil, fmt.Errorf("歌单服务缺少来源、存储或读取器")
	}
	if clock == nil {
		clock = wallClock{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{registry: registry, repository: repository, fetcher: fetcher, clock: clock, life: ctx, cancel: cancel, operations: map[string]activeOperation{}, plans: map[string]retainedPlan{}, keys: map[string]bool{}}, nil
}
func (s *Service) SetRegistry(registry *Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = registry
}
func (s *Service) Sources() []Source { s.mu.Lock(); defer s.mu.Unlock(); return s.registry.List() }
func planToken() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func PlanHash(plan Plan) (string, error) {
	plan.Hash = ""
	raw, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (s *Service) forgetLocked(id string) {
	p, ok := s.plans[id]
	if ok {
		delete(s.plans, id)
		delete(s.keys, p.key)
		s.bytes -= p.bytes
	}
}
func (s *Service) expireLocked() {
	for id, p := range s.plans {
		if !s.clock.Now().Before(p.plan.ExpiresAt) {
			s.forgetLocked(id)
		}
	}
}

func SnapshotDigest(snapshot Snapshot) (string, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// operationID is a caller correlation token, used only for cancellation. It is
// never a path or upstream identity, and does not authorize a commit.
func (s *Service) beginWork() (*Registry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, Failure(Cancelled, "歌单服务已关闭")
	}
	s.work.Add(1)
	return s.registry, nil
}

func (s *Service) PrepareBinding(ctx context.Context, operationID, source, input, alias string) (*Preview, error) {
	registry, err := s.beginWork()
	if err != nil {
		return nil, err
	}
	defer s.work.Done()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if source == "" {
		return nil, Failure(Input, "请选择歌单来源")
	}
	p, err := registry.Get(provider.ProviderID(source))
	if err != nil {
		return nil, err
	}
	ref, err := p.ParsePlaylist(input)
	if err != nil {
		return nil, err
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if ref.Provider != p.ID() {
		return nil, Failure(Input, "歌单来源身份不一致")
	}
	existing, err := s.repository.FindSyncedPlaylist(ref)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, &AlreadyBoundError{PlaylistID: existing.ID}
	}
	return s.prepare(ctx, operationID, p, ref, strings.TrimSpace(alias), nil)
}
func (s *Service) PrepareSync(ctx context.Context, operationID string, playlistID int64) (*Preview, error) {
	registry, err := s.beginWork()
	if err != nil {
		return nil, err
	}
	defer s.work.Done()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := s.repository.ReadPlaylistState(playlistID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, ErrPlanStale
	}
	p, err := registry.Get(state.Binding.Ref.Provider)
	if err != nil {
		return nil, err
	}
	return s.prepare(ctx, operationID, p, state.Binding.Ref, state.Binding.Alias, state)
}
func (s *Service) prepare(ctx context.Context, operationID string, p Provider, ref Ref, alias string, state *State) (*Preview, error) {
	if !identity(operationID) || len(alias) > 4096 {
		return nil, Failure(Input, "操作标识或别名无效")
	}
	if a := p.PlaylistAvailability(); !a.Available {
		return nil, Failure(RuntimeUnavailable, a.Reason)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := string(ref.Provider) + "\x00" + ref.ID
	s.mu.Lock()
	s.expireLocked()
	if s.closed {
		s.mu.Unlock()
		return nil, Failure(Cancelled, "歌单服务已关闭")
	}
	if state != nil {
		current, err := s.repository.ReadPlaylistState(state.Binding.ID)
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		if current == nil || current.Binding.Revision != state.Binding.Revision {
			s.mu.Unlock()
			return nil, ErrPlanStale
		}
	}
	if s.keys[key] {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	if _, ok := s.operations[operationID]; ok {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	for _, plan := range s.plans {
		if plan.operationID == operationID {
			s.mu.Unlock()
			return nil, ErrBusy
		}
	}
	if len(s.operations)+len(s.plans) >= MaxPlans {
		s.mu.Unlock()
		return nil, Failure(Resource, "待确认歌单计划已达上限")
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.life, cancel)
	s.operations[operationID] = activeOperation{key: key, cancel: cancel}
	s.keys[key] = true
	s.mu.Unlock()
	defer cancel()
	defer stop()
	retained := false
	defer func() {
		s.mu.Lock()
		delete(s.operations, operationID)
		if !retained {
			delete(s.keys, key)
		}
		s.mu.Unlock()
	}()
	snapshot, err := s.fetcher.Fetch(ctx, p, ref)
	if err != nil {
		return nil, err
	}
	// Detach provider-owned pointers without parsing platform response bytes.
	immutable := cloneSnapshot(*snapshot)
	difference, err := Diff(state, immutable)
	if err != nil {
		return nil, err
	}
	id, err := planToken()
	if err != nil {
		return nil, err
	}
	plan := Plan{ID: "playlist-" + id, Alias: alias, Snapshot: immutable, Difference: difference, ExpiresAt: s.clock.Now().Add(PlanTTL)}
	if state != nil {
		plan.PlaylistID = state.Binding.ID
		plan.BaseRevision = state.Binding.Revision
	}
	plan.Hash, err = PlanHash(plan)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	// Charge both the snapshot and the diff representation, with conservative
	// room for decoded objects. Reject rather than silently evicting previews.
	retainedBytes := len(encoded) * 2
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || ctx.Err() != nil {
		return nil, Failure(Cancelled, "歌单读取已取消")
	}
	if retainedBytes > MaxPlanBytes-s.bytes {
		return nil, Failure(Resource, "歌单确认计划超过内存上限")
	}
	s.plans[plan.ID] = retainedPlan{plan: plan, key: key, operationID: operationID, bytes: retainedBytes}
	s.bytes += retainedBytes
	retained = true
	return &Preview{ID: plan.ID, Hash: plan.Hash, PlaylistID: plan.PlaylistID, Name: immutable.Name, Alias: alias, ExpiresAt: plan.ExpiresAt.UTC().Format(time.RFC3339), Difference: cloneDifference(plan.Difference)}, nil
}

func (s *Service) Commit(ctx context.Context, id, hash string, restoreIDs []int64) (*CommitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	if s.closed {
		return nil, Failure(Cancelled, "歌单服务已关闭")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	previous, err := s.repository.PlaylistOperationResult(id, hash)
	if err != nil || previous != nil {
		return previous, err
	}
	p, ok := s.plans[id]
	if !ok || p.plan.Hash != hash {
		return nil, ErrPlanStale
	}
	result, err := s.repository.ApplyPlaylist(ctx, p.plan, restoreIDs)
	if err == nil {
		s.forgetLocked(id)
	}
	return result, err
}

// Cancel accepts either a preparation correlation token or a returned plan ID.
func (s *Service) Cancel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if op, ok := s.operations[id]; ok {
		op.cancel()
	}
	if _, ok := s.plans[id]; ok {
		s.forgetLocked(id)
	}
	for key, p := range s.plans {
		if p.operationID == id {
			s.forgetLocked(key)
		}
	}
}

// CancelFetches stops in-flight reads when outbound access is disabled. Confirmed
// database state and already prepared offline confirmation plans are untouched.
func (s *Service) CancelFetches() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, op := range s.operations {
		op.cancel()
	}
}

// Delete serializes against commits and registration of reads for this binding.
// A preparation that read its baseline before deletion cannot register afterward.
func (s *Service) Delete(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Failure(Cancelled, "歌单服务已关闭")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state, err := s.repository.ReadPlaylistState(id)
	if err != nil {
		return err
	}
	if state == nil {
		return ErrPlanStale
	}
	if err := s.repository.DeleteSyncedPlaylist(id); err != nil {
		return err
	}
	key := string(state.Binding.Ref.Provider) + "\x00" + state.Binding.Ref.ID
	for _, op := range s.operations {
		if op.key == key {
			op.cancel()
		}
	}
	for id, p := range s.plans {
		if p.key == key {
			s.forgetLocked(id)
		}
	}
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	for id := range s.plans {
		s.forgetLocked(id)
	}
	s.mu.Unlock()
	s.work.Wait()
}
