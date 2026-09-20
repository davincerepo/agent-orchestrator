package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

type terminalParametersCommander struct {
	*fakeCommander
	err error
}

func (f *terminalParametersCommander) ReadTerminalModelParameters(context.Context, domain.SessionID) (ports.AgentConfig, bool, error) {
	return ports.AgentConfig{Model: "observed"}, true, f.err
}

func TestFleetTerminalParametersServiceErrors(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{nil, ""}, {sessionmanager.ErrNotFound, "Unknown session"}, {errors.New("private-native-path"), "could not be confirmed"},
	} {
		s := NewWithDeps(Deps{Manager: &terminalParametersCommander{err: test.err}})
		got, supported, err := s.ReadTerminalModelParameters(context.Background(), "worker")
		if !supported {
			t.Fatal("capability lost")
		}
		if test.err == nil {
			if err != nil || got.Model != "observed" {
				t.Fatalf("%+v %v", got, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), test.code) || strings.Contains(err.Error(), "private-native-path") || got.Model != "" {
			t.Fatalf("unexpected error response: %+v %v", got, err)
		}
	}
}
