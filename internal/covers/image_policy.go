package covers

import (
	"context"
	"github.com/norhua/sonexa/internal/provider"
)

type imagePoliciesKey struct{}
type imagePolicyKey struct{}

// SetImagePolicies atomically replaces the validated snapshot for new work.
func (s *Service) SetImagePolicies(policies *provider.ImagePolicies) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.imagePolicies = policies
}

// SnapshotContext pins policies for a session/operation without freezing the
// dynamic outbound gate. Reusing the context preserves an older snapshot.
func (s *Service) SnapshotContext(ctx context.Context) context.Context {
	if _, ok := ctx.Value(imagePoliciesKey{}).(*provider.ImagePolicies); ok {
		return ctx
	}
	s.mu.Lock()
	policies := s.imagePolicies
	s.mu.Unlock()
	return context.WithValue(ctx, imagePoliciesKey{}, policies)
}

func imageContext(ctx context.Context, source string) context.Context {
	policies, _ := ctx.Value(imagePoliciesKey{}).(*provider.ImagePolicies)
	return context.WithValue(ctx, imagePolicyKey{}, policies.For(source))
}
func imagePolicy(ctx context.Context) *provider.ImagePolicy {
	policy, _ := ctx.Value(imagePolicyKey{}).(*provider.ImagePolicy)
	return policy
}
