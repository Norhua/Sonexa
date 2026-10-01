// Package library 编排归组、准入与编辑计划，来源只提供事实，存储执行原子提交。
package library

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

const PlanTTL = 15 * time.Minute

type Service struct {
	mu       sync.Mutex
	registry *provider.Registry
	store    *store.Store
	files    *covers.LibraryImages
	clock    Clock
	secret   [32]byte
	plans    map[string]domain.AdmissionPlan
}

func New(registry *provider.Registry, st *store.Store, files *covers.LibraryImages, clock Clock) (*Service, error) {
	if registry == nil || st == nil {
		return nil, fmt.Errorf("library 缺少注册表或存储")
	}
	if clock == nil {
		clock = systemClock{}
	}
	service := &Service{registry: registry, store: st, files: files, clock: clock, plans: map[string]domain.AdmissionPlan{}}
	if _, err := rand.Read(service.secret[:]); err != nil {
		return nil, err
	}
	st.SetAdmissionVerifier(service.verify)
	return service, nil
}
func (s *Service) SetRegistry(registry *provider.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = registry
}
func (s *Service) provider(id domain.ProviderID) (provider.Provider, error) {
	s.mu.Lock()
	registry := s.registry
	s.mu.Unlock()
	return registry.Get(id)
}
func clone[T any](value T) (T, error) {
	var copy T
	raw, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	err = json.Unmarshal(raw, &copy)
	return copy, err
}
func token() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
func (s *Service) sign(prefix, id, hash string) string {
	mac := hmac.New(sha256.New, s.secret[:])
	mac.Write([]byte(prefix + ":" + id + ":" + hash))
	return hex.EncodeToString(mac.Sum(nil))
}
func (s *Service) verify(plan domain.AdmissionPlan, approval domain.AdmissionApproval) bool {
	expires, err := time.Parse(time.RFC3339Nano, plan.ExpiresAt)
	if err != nil || !s.clock.Now().Before(expires) {
		return false
	}
	if !hmac.Equal([]byte(plan.Seal), []byte(s.sign("plan", plan.ID, plan.Hash))) {
		return false
	}
	if approval.Credential == "" {
		return true
	}
	return approval.PlanID == plan.ID && approval.Hash == plan.Hash && hmac.Equal([]byte(approval.Credential), []byte(s.sign("confirmed", plan.ID, plan.Hash)))
}

func (s *Service) PrepareAdmission(ctx context.Context, seed domain.AdmissionSeed) (*domain.AdmissionPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seed, err := clone(seed)
	if err != nil {
		return nil, err
	}
	baseline, err := s.store.PreviewAdmission(seed)
	if err != nil {
		return nil, err
	}
	id, err := token()
	if err != nil {
		return nil, err
	}
	plan := domain.AdmissionPlan{ID: id, ExpiresAt: s.clock.Now().Add(PlanTTL).UTC().Format(time.RFC3339Nano), Seed: seed, Baseline: baseline}
	plan.Hash, err = store.AdmissionHash(plan)
	if err != nil {
		return nil, err
	}
	plan.Seal = s.sign("plan", plan.ID, plan.Hash)
	s.mu.Lock()
	for key, p := range s.plans {
		expires, _ := time.Parse(time.RFC3339Nano, p.ExpiresAt)
		if !s.clock.Now().Before(expires) {
			delete(s.plans, key)
		}
	}
	s.plans[id] = plan
	s.mu.Unlock()
	copy, err := clone(plan)
	return &copy, err
}
func (s *Service) CandidateSeed(candidate provider.Candidate, itemID int64) (domain.AdmissionSeed, error) {
	p, err := s.provider(candidate.Provider)
	if err != nil {
		return domain.AdmissionSeed{}, err
	}
	snapshot, err := p.SnapshotFromCandidate(candidate)
	if err != nil {
		return domain.AdmissionSeed{}, err
	}
	if snapshot == nil || snapshot.Track == nil || snapshot.Track.Identity.Ref == "" {
		return domain.AdmissionSeed{}, store.ErrSnapshotTarget
	}
	if snapshot.Release == nil && (candidate.AlbumTitle != nil || candidate.AlbumCredit != nil || candidate.PrimaryType != nil) {
		snapshot.Release = &domain.ReleaseSnapshot{Title: provider.FieldFromPointer(candidate.AlbumTitle), PrimaryType: provider.FieldFromPointer(candidate.PrimaryType)}
		if candidate.AlbumCredit != nil {
			snapshot.Release.Credit = domain.Known(domain.Credit{Text: *candidate.AlbumCredit})
		}
		if snapshot.Group != nil {
			snapshot.Release.Group = &snapshot.Group.Identity
		}
	}
	return domain.AdmissionSeed{Snapshot: *snapshot, ImportItemID: itemID, ItemStatus: "imported"}, nil
}
func (s *Service) PrepareCandidate(ctx context.Context, candidate provider.Candidate, itemID int64) (*domain.AdmissionPlan, error) {
	seed, err := s.CandidateSeed(candidate, itemID)
	if err != nil {
		return nil, err
	}
	return s.PrepareAdmission(ctx, seed)
}
func (s *Service) plan(id, hash string) (domain.AdmissionPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.plans[id]
	if !ok || plan.Hash != hash || !s.verify(plan, domain.AdmissionApproval{}) {
		return domain.AdmissionPlan{}, store.ErrPlanStale
	}
	return clone(plan)
}

// ConfirmReleaseAdditions 只批准当前计划的具体成员动作；切换候选必须产生新计划。
func (s *Service) ConfirmReleaseAdditions(id, hash string) (*domain.AdmissionApproval, error) {
	plan, err := s.plan(id, hash)
	if err != nil {
		return nil, err
	}
	baseline, err := s.store.PreviewAdmission(plan.Seed)
	if err != nil {
		return nil, err
	}
	before, _ := json.Marshal(plan.Baseline)
	after, _ := json.Marshal(baseline)
	if string(before) != string(after) {
		return nil, store.ErrPlanStale
	}
	if baseline.Conflict {
		return nil, store.ErrGroupConflict
	}
	return &domain.AdmissionApproval{PlanID: id, Hash: hash, Credential: s.sign("confirmed", id, hash)}, nil
}
func (s *Service) CommitAdmission(ctx context.Context, id, hash string, approval domain.AdmissionApproval) (*domain.AdmissionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	previous, err := s.store.AdmissionResult(id, hash)
	if err != nil || previous != nil {
		return previous, err
	}
	plan, err := s.plan(id, hash)
	if err != nil {
		return nil, err
	}
	if plan.Baseline.RequiresConfirmation && approval.Credential == "" {
		return nil, store.ErrConfirmationRequired
	}
	return s.store.CommitAdmission(plan, approval)
}
func (s *Service) ResolveGroup(snapshot domain.Snapshot) (domain.GroupResolution, error) {
	return s.store.ResolveGroup(snapshot)
}
