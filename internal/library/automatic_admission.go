package library

import (
	"context"
	"github.com/norhua/sonexa/internal/domain"
)

type automaticAdmissionKey struct{}
type automaticScopeKey struct{}

func (s *Service) automaticScope(ctx context.Context, itemID, revision int64) (context.Context, error) {
	if itemID == 0 {
		return ctx, nil
	}
	scope, err := s.store.ReadSingleInputScope(itemID, revision)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, automaticScopeKey{}, scope), nil
}
func (s *Service) checkedAutomaticScope(ctx context.Context) (*domain.InputScope, error) {
	scope, _ := ctx.Value(automaticScopeKey{}).(*domain.InputScope)
	if scope == nil {
		return nil, nil
	}
	if err := s.store.CheckInputScope(scope); err != nil {
		return nil, err
	}
	// The plan itself also checks the interaction in its write transaction.
	return scope, nil
}

func automaticAdmissionContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, automaticAdmissionKey{}, true)
}
func isAutomaticAdmission(ctx context.Context) bool {
	value, _ := ctx.Value(automaticAdmissionKey{}).(bool)
	return value
}
