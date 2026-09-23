package provider

import "context"

type requestGateKey struct{}

// WithRequestGate 在每次真实请求（包括限流等待后的重试）前检查，缓存命中不受影响。
func WithRequestGate(ctx context.Context, gate func() error) context.Context {
	return context.WithValue(ctx, requestGateKey{}, gate)
}

func checkRequestGate(ctx context.Context) error {
	if gate, ok := ctx.Value(requestGateKey{}).(func() error); ok {
		return gate()
	}
	return nil
}
