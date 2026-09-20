package sessionmanager

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ReadTerminalModelParameters observes the current TUI's durable native settings.
// It never resumes a provider, writes configuration, or uses launch defaults.
func (m *Manager) ReadTerminalModelParameters(ctx context.Context, id domain.SessionID) (ports.AgentConfig, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rec, found, err := m.store.GetSession(ctx, id)
	if err != nil {
		return ports.AgentConfig{}, false, err
	}
	if !found {
		return ports.AgentConfig{}, false, ErrNotFound
	}
	if rec.Harness != domain.HarnessCodex || domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeTUI {
		return ports.AgentConfig{}, false, nil
	}
	agent, available := m.agents.Agent(rec.Harness)
	if _, supported := agent.(ports.AgentNativeModelParametersReader); !available || !supported {
		return ports.AgentConfig{}, false, nil
	}
	// A resume hint can survive a failed launch. Only an identity proven for
	// this runtime generation may be presented as the visible terminal's state.
	if rec.IsTerminated || rec.Metadata.AgentSessionID == "" || rec.Metadata.RuntimeLaunchID == "" ||
		rec.Metadata.AgentSessionIDLaunchID != rec.Metadata.RuntimeLaunchID {
		return ports.AgentConfig{}, true, ErrNativeConversationUnverified
	}
	parameters, err := m.readNativeModelParameters(ctx, rec, rec.Metadata.AgentSessionID)
	if err != nil {
		return ports.AgentConfig{}, true, err
	}
	current, found, err := m.store.GetSession(ctx, id)
	if err != nil {
		return ports.AgentConfig{}, true, err
	}
	if !found || current.IsTerminated || current.Harness != rec.Harness || current.Mode != rec.Mode ||
		current.ProjectID != rec.ProjectID || current.Metadata.RuntimeLaunchID != rec.Metadata.RuntimeLaunchID ||
		current.Metadata.RuntimeHandleID != rec.Metadata.RuntimeHandleID ||
		current.Metadata.AgentSessionID != rec.Metadata.AgentSessionID ||
		current.Metadata.AgentSessionIDLaunchID != rec.Metadata.AgentSessionIDLaunchID {
		return ports.AgentConfig{}, true, ErrNativeConversationUnverified
	}
	if err := ctx.Err(); err != nil {
		return ports.AgentConfig{}, true, err
	}
	return parameters, true, nil
}
