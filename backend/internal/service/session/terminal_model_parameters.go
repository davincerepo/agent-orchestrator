package session

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

// ReadTerminalModelParameters exposes an optional read capability independently
// of the launch snapshots in the ordinary session read model.
func (s *Service) ReadTerminalModelParameters(ctx context.Context, id domain.SessionID) (ports.AgentConfig, bool, error) {
	reader, ok := s.manager.(interface {
		ReadTerminalModelParameters(context.Context, domain.SessionID) (ports.AgentConfig, bool, error)
	})
	if !ok {
		return ports.AgentConfig{}, false, nil
	}
	config, supported, err := reader.ReadTerminalModelParameters(ctx, id)
	if errors.Is(err, sessionmanager.ErrNotFound) {
		return ports.AgentConfig{}, supported, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if err != nil {
		// Filesystem errors can contain private native paths. Keep them out of
		// the wire response and never turn a failed observation into defaults.
		return ports.AgentConfig{}, supported, apierr.Conflict("MODEL_PARAMETERS_UNCONFIRMED", "Saved terminal model parameters could not be confirmed. Reopen the terminal to retry.", nil)
	}
	return config, supported, nil
}
