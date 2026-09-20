package sessionmanager

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type terminalParametersAgent struct {
	modelTransitionAgent
	read func(context.Context, ports.NativeSessionRef) (ports.AgentConfig, error)
}

func (a *terminalParametersAgent) ReadNativeModelParameters(ctx context.Context, ref ports.NativeSessionRef) (ports.AgentConfig, error) {
	return a.read(ctx, ref)
}

func TestFleetTerminalParametersReadOnlyAndIdentity(t *testing.T) {
	for _, test := range []struct {
		name          string
		change        func(*domain.SessionRecord)
		wantRead      bool
		wantSupported bool
		wantError     bool
	}{
		{name: "current terminal", wantRead: true, wantSupported: true},
		{name: "orchestrator", change: func(r *domain.SessionRecord) { r.Kind = domain.KindOrchestrator }, wantRead: true, wantSupported: true},
		{name: "chat", change: func(r *domain.SessionRecord) { r.Mode = domain.SessionModeChat }},
		{name: "other provider", change: func(r *domain.SessionRecord) { r.Harness = domain.HarnessClaudeCode }},
		{name: "terminated", change: func(r *domain.SessionRecord) { r.IsTerminated = true }, wantSupported: true, wantError: true},
		{name: "old resume hint", change: func(r *domain.SessionRecord) { r.Metadata.AgentSessionIDLaunchID = "previous-launch" }, wantSupported: true, wantError: true},
		{name: "missing thread", change: func(r *domain.SessionRecord) { r.Metadata.AgentSessionID = "" }, wantSupported: true, wantError: true},
		{name: "missing generation", change: func(r *domain.SessionRecord) { r.Metadata.RuntimeLaunchID = ""; r.Metadata.AgentSessionIDLaunchID = "" }, wantSupported: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, store, _, _, operations := newTransitionManager(t, domain.SessionModeTUI)
			rec := store.sessions["session-1"]
			rec.Harness = domain.HarnessCodex
			rec.Metadata.Model = "wrong-launch-default"
			rec.Metadata.ServiceTier = "priority"
			if test.change != nil {
				test.change(&rec)
			}
			store.sessions[rec.ID] = rec
			reads := 0
			want := ports.AgentConfig{Model: "actual-native-model", Effort: "high", ServiceTier: "default"}
			m.agents = singleAgent{agent: &terminalParametersAgent{read: func(ctx context.Context, ref ports.NativeSessionRef) (ports.AgentConfig, error) {
				reads++
				if ref.NativeSessionID != "native-1" || ref.ConfigDir != "fixture-home" {
					t.Fatalf("wrong native identity: %+v", ref)
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("native read is not bounded")
				}
				return want, nil
			}}}
			got, supported, err := m.ReadTerminalModelParameters(context.Background(), rec.ID)
			if supported != test.wantSupported || (err != nil) != test.wantError || (reads != 0) != test.wantRead {
				t.Fatalf("got %+v supported=%v err=%v reads=%d", got, supported, err, reads)
			}
			if test.wantRead && got != want {
				t.Fatalf("got %+v want %+v", got, want)
			}
			if !reflect.DeepEqual(store.sessions[rec.ID], rec) || len(*operations) != 0 || len(store.transitions) != 0 {
				t.Fatal("observation mutated or restarted the session")
			}
		})
	}
}

func TestFleetTerminalParametersUnknownFailureAndConcurrentChange(t *testing.T) {
	for _, scenario := range []string{"unknown fields", "read failure", "runtime replaced", "thread changed", "cancelled", "missing session"} {
		t.Run(scenario, func(t *testing.T) {
			m, store, _, _, _ := newTransitionManager(t, domain.SessionModeTUI)
			rec := store.sessions["session-1"]
			rec.Harness = domain.HarnessCodex
			rec.Metadata.Model, rec.Metadata.ReasoningEffort, rec.Metadata.ServiceTier = "default-model", "medium", "priority"
			store.sessions[rec.ID] = rec
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m.agents = singleAgent{agent: &terminalParametersAgent{read: func(context.Context, ports.NativeSessionRef) (ports.AgentConfig, error) {
				switch scenario {
				case "read failure":
					return ports.AgentConfig{}, errors.New("private-path")
				case "runtime replaced":
					changed := store.sessions[rec.ID]
					changed.Metadata.RuntimeLaunchID = "new-runtime"
					store.sessions[rec.ID] = changed
				case "thread changed":
					changed := store.sessions[rec.ID]
					changed.Metadata.AgentSessionID = "new-thread"
					store.sessions[rec.ID] = changed
				case "cancelled":
					cancel()
				}
				return ports.AgentConfig{Model: "native-only"}, nil
			}}}
			if scenario == "missing session" {
				delete(store.sessions, rec.ID)
			}
			got, _, err := m.ReadTerminalModelParameters(ctx, rec.ID)
			if scenario == "unknown fields" {
				if err != nil || got != (ports.AgentConfig{Model: "native-only"}) {
					t.Fatalf("unknown values were filled: %+v %v", got, err)
				}
			} else if err == nil || got != (ports.AgentConfig{}) {
				t.Fatalf("unconfirmed parameters escaped: %+v %v", got, err)
			}
		})
	}
}
