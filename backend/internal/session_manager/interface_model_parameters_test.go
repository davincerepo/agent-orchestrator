package sessionmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type modelTransitionStore struct {
	*transitionStore
	settings domain.ConversationSettings
}

func (s *modelTransitionStore) ConversationForSession(context.Context, domain.SessionID) (domain.ConversationRecord, error) {
	return domain.ConversationRecord{Settings: s.settings}, nil
}

type modelTransitionAgent struct {
	transitionAgent
	parameters ports.AgentConfig
	err        error
	launches   chan ports.AgentConfig
}

func (a *modelTransitionAgent) NativeSessionConfigDir(context.Context, map[string]string) (string, error) {
	return "fixture-home", nil
}
func (a *modelTransitionAgent) ReadNativeModelParameters(context.Context, ports.NativeSessionRef) (ports.AgentConfig, error) {
	return a.parameters, a.err
}
func (a *modelTransitionAgent) GetRestoreCommand(ctx context.Context, cfg ports.RestoreConfig) ([]string, bool, error) {
	a.launches <- cfg.Config
	return a.transitionAgent.GetRestoreCommand(ctx, cfg)
}

func TestFleetInterfaceParametersFollowSourceMode(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeChat, domain.SessionModeTUI} {
		t.Run(string(mode), func(t *testing.T) {
			m, st, _, chat, _ := newTransitionManager(t, mode)
			store := &modelTransitionStore{transitionStore: st, settings: domain.ConversationSettings{Model: "chat-latest", ReasoningEffort: "xhigh", ServiceTier: "default"}}
			m.store = store
			agent := &modelTransitionAgent{parameters: ports.AgentConfig{Model: "terminal-latest", Effort: "low", ServiceTier: "priority"}, launches: make(chan ports.AgentConfig, 4)}
			m.agents = singleAgent{agent: agent}
			rec := st.sessions["session-1"]
			rec.Harness = domain.HarnessCodex
			rec.Metadata.Model = "stale-launch-model"
			rec.Metadata.ReasoningEffort = "medium"
			rec.Metadata.ServiceTier = "default"
			st.sessions[rec.ID] = rec
			target := domain.SessionModeChat
			want := agent.parameters
			if mode == domain.SessionModeChat {
				target = domain.SessionModeTUI
				want = ports.AgentConfig{Model: "chat-latest", Effort: "xhigh", ServiceTier: "default"}
			}
			tr, err := m.StartInterfaceTransition(context.Background(), rec.ID, target, domain.SessionInterfaceTransitionInterrupt, domain.SessionInterfaceTransitionHistoryStrict)
			if err != nil {
				t.Fatal(err)
			}
			settled := awaitTransition(t, st, tr.ID)
			if settled.Phase != domain.SessionInterfaceTransitionCompleted {
				t.Fatalf("transition: %+v", settled)
			}
			var got ports.AgentConfig
			if target == domain.SessionModeChat {
				got = ports.AgentConfig{Model: chat.start.Model, Effort: chat.start.Effort, ServiceTier: chat.start.ServiceTier}
			} else {
				for len(agent.launches) > 0 {
					got = <-agent.launches
				}
			}
			if got.Model != want.Model || got.Effort != want.Effort || got.ServiceTier != want.ServiceTier {
				t.Fatalf("target used %+v want %+v", got, want)
			}
		})
	}
}

func TestFleetInterfaceParameterReadFailureRollsBack(t *testing.T) {
	m, st, _, _, _ := newTransitionManager(t, domain.SessionModeChat)
	m.store = &modelTransitionStore{transitionStore: st}
	m.agents = singleAgent{agent: &modelTransitionAgent{err: errors.New("unreadable native settings"), launches: make(chan ports.AgentConfig, 4)}}
	rec := st.sessions["session-1"]
	rec.Harness = domain.HarnessCodex
	st.sessions[rec.ID] = rec
	tr, err := m.StartInterfaceTransition(context.Background(), rec.ID, domain.SessionModeTUI, domain.SessionInterfaceTransitionInterrupt, domain.SessionInterfaceTransitionHistoryStrict)
	if err != nil {
		t.Fatal(err)
	}
	settled := awaitTransition(t, st, tr.ID)
	if settled.Phase != domain.SessionInterfaceTransitionFailed || settled.ErrorCode != "MODEL_PARAMETERS_UNAVAILABLE" {
		t.Fatalf("transition: %+v", settled)
	}
	if st.sessions[rec.ID].Mode != domain.SessionModeChat {
		t.Fatal("failed read committed target mode")
	}
}
