package sessionmanager

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Called only after source shutdown has flushed its native history. The
// destination receives these choices in the same transaction as mode ownership.
// Chat's explicit next-turn choices take precedence over its last native turn;
// Terminal has no AO next-turn overrides, so stale Chat choices must not win.
func (m *Manager) interfaceModelParameters(ctx context.Context, rec domain.SessionRecord, nativeID string) ([]domain.AgentConfig, error) {
	if rec.Harness != domain.HarnessCodex {
		return nil, nil
	}
	var config domain.AgentConfig
	if nativeID != "" {
		var err error
		config, err = m.readNativeModelParameters(ctx, rec, nativeID)
		if err != nil {
			return nil, err
		}
	} else {
		config.Model, config.Effort, config.ServiceTier = rec.Metadata.Model, rec.Metadata.ReasoningEffort, rec.Metadata.ServiceTier
	}
	if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat {
		store, ok := m.store.(interface {
			ConversationForSession(context.Context, domain.SessionID) (domain.ConversationRecord, error)
		})
		if !ok {
			return nil, fmt.Errorf("Chat model parameter storage is unavailable")
		}
		conversation, err := store.ConversationForSession(ctx, rec.ID)
		if err != nil {
			return nil, err
		}
		config = applySpawnAgentConfig(config, ports.AgentConfig{
			Model: conversation.Settings.Model, Effort: conversation.Settings.ReasoningEffort,
			ServiceTier: conversation.Settings.ServiceTier,
		})
	}
	return []domain.AgentConfig{config}, nil
}

// readNativeModelParameters only observes native history, including while a TUI is live.
func (m *Manager) readNativeModelParameters(ctx context.Context, rec domain.SessionRecord, nativeID string) (ports.AgentConfig, error) {
	agent, ok := m.agents.Agent(rec.Harness)
	reader, supported := agent.(ports.AgentNativeModelParametersReader)
	homes, hasHome := agent.(ports.AgentNativeSessionConfigProvider)
	if !ok || !supported || !hasHome {
		return ports.AgentConfig{}, fmt.Errorf("Codex native model parameter reader is unavailable")
	}
	project, err := m.loadProject(ctx, rec.ProjectID)
	if err != nil {
		return ports.AgentConfig{}, err
	}
	env := m.runtimeEnv(rec.ID, rec.ProjectID, rec.IssueID, project.Config.Env)
	home, err := homes.NativeSessionConfigDir(ctx, env)
	if err != nil {
		return ports.AgentConfig{}, err
	}
	return reader.ReadNativeModelParameters(ctx, ports.NativeSessionRef{NativeSessionID: nativeID, ConfigDir: home})
}
