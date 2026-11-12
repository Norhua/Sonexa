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
	"strings"
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
	editMu    sync.Mutex
	mu        sync.Mutex
	registry  *provider.Registry
	store     *store.Store
	files     *covers.LibraryImages
	clock     Clock
	secret    [32]byte
	plans     map[string]domain.AdmissionPlan
	previews  map[string]storedEditPreview
	additions map[string]retainedAddition
}

func New(registry *provider.Registry, st *store.Store, files *covers.LibraryImages, clock Clock) (*Service, error) {
	if registry == nil || st == nil {
		return nil, fmt.Errorf("library 缺少注册表或存储")
	}
	if clock == nil {
		clock = systemClock{}
	}
	service := &Service{registry: registry, store: st, files: files, clock: clock, plans: map[string]domain.AdmissionPlan{}, previews: map[string]storedEditPreview{}}
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
func (s *Service) provider(id domain.ProviderID) (provider.MetadataProvider, error) {
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
	// 两种凭证按用途用不同前缀签名：普通成员确认不能冒充输入确认，反之亦然。
	prefix := "confirmed"
	switch approval.Kind {
	case domain.ApprovalMembership:
	case domain.ApprovalTyped:
		prefix = "typed"
	default:
		return false
	}
	return approval.PlanID == plan.ID && approval.Hash == plan.Hash && hmac.Equal([]byte(approval.Credential), []byte(s.sign(prefix, plan.ID, plan.Hash)))
}

func (s *Service) PrepareAdmission(ctx context.Context, seed domain.AdmissionSeed) (*domain.AdmissionPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seed, err := clone(seed)
	if err != nil {
		return nil, err
	}
	seed.Destinations = nil // never accept caller-supplied destinations
	baseline, err := s.store.PreviewAdmission(seed)
	if err != nil {
		return nil, err
	}
	seed.Destinations = baseline.Destinations
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

// currentBaseline 取服务端保存的计划并确认基线未变、没有组冲突。
func (s *Service) currentBaseline(id, hash string) (domain.AdmissionPlan, error) {
	plan, err := s.plan(id, hash)
	if err != nil {
		return plan, err
	}
	baseline, err := s.store.PreviewAdmission(plan.Seed)
	if err != nil {
		return plan, err
	}
	before, _ := json.Marshal(plan.Baseline)
	after, _ := json.Marshal(baseline)
	if string(before) != string(after) {
		return plan, store.ErrPlanStale
	}
	if baseline.Conflict {
		return plan, store.ErrGroupConflict
	}
	return plan, nil
}

// ConfirmReleaseAdditions 只批准当前计划的具体成员动作；切换候选必须产生新计划。
// 需要输入确认的计划不签发这种凭证。
func (s *Service) ConfirmReleaseAdditions(id, hash string) (*domain.AdmissionApproval, error) {
	plan, err := s.currentBaseline(id, hash)
	if err != nil {
		return nil, err
	}
	if plan.Baseline.NeedsTypedConfirm {
		return nil, store.ErrTypedConfirmationRequired
	}
	return &domain.AdmissionApproval{PlanID: id, Hash: hash, Credential: s.sign("confirmed", id, hash)}, nil
}

// TypedConfirmWord 是允许同一首歌进入另一个发行时必须输入的确认词。
const TypedConfirmWord = "Yes"

// ConfirmTypedAdmission 在服务端校验确认词（去首尾空白，区分大小写），通过才签发
// 输入确认类型的凭证。前端的输入校验只用于启用按钮，不是闸门。
func (s *Service) ConfirmTypedAdmission(id, hash, word string) (*domain.AdmissionApproval, error) {
	plan, err := s.currentBaseline(id, hash)
	if err != nil {
		return nil, err
	}
	if !plan.Baseline.NeedsTypedConfirm {
		return nil, fmt.Errorf("当前计划不需要输入确认")
	}
	if strings.TrimSpace(word) != TypedConfirmWord {
		return nil, fmt.Errorf("确认词不正确，请输入 %s", TypedConfirmWord)
	}
	return &domain.AdmissionApproval{PlanID: id, Hash: hash, Kind: domain.ApprovalTyped, Credential: s.sign("typed", id, hash)}, nil
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
	if plan.Baseline.NeedsTypedConfirm && (approval.Credential == "" || approval.Kind != domain.ApprovalTyped) {
		return nil, store.ErrTypedConfirmationRequired
	}
	if plan.Baseline.RequiresConfirmation && approval.Credential == "" {
		return nil, store.ErrConfirmationRequired
	}
	return s.store.CommitAdmission(plan, approval)
}
func (s *Service) ResolveGroup(snapshot domain.Snapshot) (domain.GroupResolution, error) {
	return s.store.ResolveGroup(snapshot)
}

// PrepareGhostAdmission 从正式发行快照定位清单位置，不接受客户端拼装的缺失上下文。
func (s *Service) PrepareGhostAdmission(ctx context.Context, releaseID int64, disc, position int) (*domain.AdmissionPlan, error) {
	release, err := s.store.GetReleaseSnapshot(releaseID)
	if err != nil {
		return nil, err
	}
	if release == nil || release.Tracklist.State != domain.Value {
		return nil, fmt.Errorf("该发行没有可用清单")
	}
	for _, entry := range release.Tracklist.Value {
		if entry.DiscNumber != disc || entry.TrackNumber != position {
			continue
		}
		snapshot, err := tracklistSnapshot(release, entry)
		if err != nil {
			return nil, err
		}
		return s.PrepareAdmission(ctx, domain.AdmissionSeed{Snapshot: snapshot})
	}
	return nil, fmt.Errorf("该发行清单位置不存在")
}

// ReplanAdmissionGroup 只修改服务端已保存计划的合组意图，候选与原始条目保持不变。
func (s *Service) ReplanAdmissionGroup(ctx context.Context, id, hash string, target int64) (*domain.AdmissionPlan, error) {
	plan, err := s.plan(id, hash)
	if err != nil {
		return nil, err
	}
	// 合组目标由基线校验：必须存在于库中；命中组之外也允许（把先前判为
	// 「不是同一组」的两组显式合并回来）。
	plan.Seed.MergeTargetID, plan.Seed.JoinGroupID, plan.Seed.KeepSeparate = target, 0, false
	return s.PrepareAdmission(ctx, plan.Seed)
}

// 判组动作：合并到某组、加入某组但不合并其余、保持独立成组。
const (
	GroupActionMerge    = "merge"
	GroupActionJoin     = "join"
	GroupActionSeparate = "separate"
)

// ReplanAdmissionGroupChoice 在服务端已保存的计划上改写判组动作并重新准备。
// 后两种动作会在提交时留下「不是同一组」的否定决定。
func (s *Service) ReplanAdmissionGroupChoice(ctx context.Context, id, hash, action string, target int64) (*domain.AdmissionPlan, error) {
	if action == GroupActionMerge {
		return s.ReplanAdmissionGroup(ctx, id, hash, target)
	}
	plan, err := s.plan(id, hash)
	if err != nil {
		return nil, err
	}
	plan.Seed.MergeTargetID, plan.Seed.JoinGroupID, plan.Seed.KeepSeparate = 0, 0, false
	switch action {
	case GroupActionJoin:
		plan.Seed.JoinGroupID = target
	case GroupActionSeparate:
		plan.Seed.KeepSeparate = true
	default:
		return nil, fmt.Errorf("未知的判组动作")
	}
	return s.PrepareAdmission(ctx, plan.Seed)
}

func (s *Service) SetFiles(files *covers.LibraryImages) {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	s.files = files
}
