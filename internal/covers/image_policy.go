package covers

import (
	"context"
	"github.com/norhua/sonexa/internal/provider"
)

type imagePoliciesKey struct{}
type imagePolicyKey struct{}
type sourceSnapshot struct {
	images  *provider.ImagePolicies
	artwork map[string]provider.ArtworkStrategy
	detail  func(context.Context, provider.Ref) (*provider.Detail, error)
}

// SetImagePolicies atomically replaces the validated snapshot for new work.
func (s *Service) SetImagePolicies(policies *provider.ImagePolicies) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.imagePolicies = policies
}

// SnapshotContext pins policies for a session/operation without freezing the
// dynamic outbound gate. Reusing the context preserves an older snapshot.
func (s *Service) SnapshotContext(ctx context.Context) context.Context {
	if _, ok := ctx.Value(imagePoliciesKey{}).(*sourceSnapshot); ok {
		return ctx
	}
	s.mu.Lock()
	snapshot := &sourceSnapshot{images: s.imagePolicies, artwork: make(map[string]provider.ArtworkStrategy, len(s.artwork)), detail: s.detailLoader}
	for source, strategy := range s.artwork {
		snapshot.artwork[source] = strategy
	}
	s.mu.Unlock()
	return context.WithValue(ctx, imagePoliciesKey{}, snapshot)
}

func imageContext(ctx context.Context, source string) context.Context {
	snapshot, _ := ctx.Value(imagePoliciesKey{}).(*sourceSnapshot)
	var policy *provider.ImagePolicy
	if snapshot != nil {
		policy = snapshot.images.For(source)
	}
	return context.WithValue(ctx, imagePolicyKey{}, policy)
}

// WithPolicySnapshot combines an application context with an atomically captured
// service snapshot. Neither cancellation nor the dynamic gate is replaced.
func WithPolicySnapshot(ctx, snapshot context.Context) context.Context {
	if value, ok := snapshot.Value(imagePoliciesKey{}).(*sourceSnapshot); ok {
		return context.WithValue(ctx, imagePoliciesKey{}, value)
	}
	return ctx
}

func artworkIn(ctx context.Context, source string) (provider.ArtworkStrategy, bool) {
	snapshot, _ := ctx.Value(imagePoliciesKey{}).(*sourceSnapshot)
	if snapshot == nil {
		return provider.ArtworkStrategy{}, false
	}
	strategy, ok := snapshot.artwork[source]
	return strategy, ok
}

func imagePolicy(ctx context.Context) *provider.ImagePolicy {
	policy, _ := ctx.Value(imagePolicyKey{}).(*provider.ImagePolicy)
	return policy
}
