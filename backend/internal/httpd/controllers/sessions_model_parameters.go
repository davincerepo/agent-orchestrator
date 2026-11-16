package controllers

import (
	"context"
	"net/http"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func (c *SessionsController) terminalModelParameters(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	reader, ok := c.Svc.(interface {
		ReadTerminalModelParameters(context.Context, domain.SessionID) (ports.AgentConfig, bool, error)
	})
	if !ok {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/sessions/{sessionId}/terminal-model-parameters")
		return
	}
	config, supported, err := reader.ReadTerminalModelParameters(r.Context(), sessionID(r))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, TerminalModelParametersResponse{
		Supported: supported, Model: config.Model, ReasoningEffort: config.Effort, ServiceTier: config.ServiceTier,
	})
}
