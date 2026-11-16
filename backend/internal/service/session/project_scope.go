package session

import (
	"context"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/dispatchscope"
)

// WithCallerSession carries optional AO caller context, not authentication.
func WithCallerSession(ctx context.Context, id domain.SessionID) context.Context {
	return dispatchscope.WithCallerSession(ctx, id)
}

func (s *Service) checkDispatchProject(ctx context.Context, project domain.ProjectID) error {
	return dispatchscope.CheckProject(ctx, s.store, project)
}

func (s *Service) checkDispatchSession(ctx context.Context, id domain.SessionID) error {
	return dispatchscope.CheckSession(ctx, s.store, id)
}
