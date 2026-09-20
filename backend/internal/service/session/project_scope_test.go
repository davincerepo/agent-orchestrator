package session

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestFleetProjectDispatchScope(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeChat, domain.SessionModeTUI} {
		for _, operation := range []string{"send", "spawn", "delegate"} {
			for _, tc := range []struct {
				name, caller, project, code string
			}{
				{"same-project", "orch", "a", ""},
				{"cross-project", "orch", "b", "CROSS_PROJECT_DISPATCH"},
				{"worker-reply", "worker", "a", ""},
				{"cross-project-worker", "worker", "b", "CROSS_PROJECT_DISPATCH"},
				{"missing-caller", "missing", "a", "CALLER_SESSION_UNAVAILABLE"},
				{"terminated-caller", "dead", "a", "CALLER_SESSION_UNAVAILABLE"},
				{"unowned-caller", "unowned", "a", "CALLER_SESSION_UNAVAILABLE"},
				{"blank-caller", " ", "a", "INVALID_CALLER_SESSION"},
				{"human", "", "b", ""},
			} {
				t.Run(string(mode)+"/"+operation+"/"+tc.name, func(t *testing.T) {
					st := newFakeStore()
					st.projects["a"] = domain.ProjectRecord{ID: "a"}
					st.projects["b"] = domain.ProjectRecord{ID: "b"}
					st.sessions["orch"] = domain.SessionRecord{ID: "orch", ProjectID: "a", Kind: domain.KindOrchestrator, Mode: mode}
					st.sessions["worker"] = domain.SessionRecord{ID: "worker", ProjectID: "a", Kind: domain.KindWorker, Mode: mode}
					st.sessions["dead"] = domain.SessionRecord{ID: "dead", ProjectID: "a", IsTerminated: true}
					st.sessions["unowned"] = domain.SessionRecord{ID: "unowned"}
					st.sessions["target"] = domain.SessionRecord{ID: "target", ProjectID: domain.ProjectID(tc.project), Mode: mode}
					cmd := &fakeCommander{}
					svc := &Service{store: st, manager: cmd}
					ctx := WithCallerSession(context.Background(), domain.SessionID(tc.caller))
					var err error
					switch operation {
					case "send":
						// A forged message prefix cannot override structured identity.
						err = svc.Send(ctx, "target", "[from target] task", nil)
					case "spawn":
						_, _, _, err = svc.Spawn(ctx, ports.SpawnConfig{ProjectID: domain.ProjectID(tc.project), Kind: domain.KindWorker, RequestedMode: mode})
					case "delegate":
						_, err = svc.DelegateTask(ctx, DelegateTaskInput{ProjectID: domain.ProjectID(tc.project), RequestedMode: mode})
					}
					if tc.code == "" {
						if err != nil || (len(cmd.sent) == 0 && !cmd.spawned) {
							t.Fatalf("allowed operation failed: %v", err)
						}
					} else {
						var apiErr *apierr.Error
						if !errors.As(err, &apiErr) || apiErr.Code != tc.code {
							t.Fatalf("err = %v, want %s", err, tc.code)
						}
						if len(cmd.sent) != 0 || cmd.spawned {
							t.Fatal("rejected operation reached the manager")
						}
					}
				})
			}
		}
	}
}

func TestFleetProjectScopeUnknownTargetAndStoreFailure(t *testing.T) {
	st := newFakeStore()
	st.sessions["caller"] = domain.SessionRecord{ID: "caller", ProjectID: "a"}
	cmd := &fakeCommander{}
	svc := &Service{store: st, manager: cmd}
	ctx := WithCallerSession(context.Background(), "caller")
	err := svc.Send(ctx, "missing", "test", nil)
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("unknown target: %v", err)
	}
	st.getSessionErr = errors.New("database unavailable")
	if err := svc.Send(ctx, "target", "test", nil); !errors.Is(err, st.getSessionErr) {
		t.Fatalf("store error: %v", err)
	}
	if len(cmd.sent) != 0 {
		t.Fatal("failed lookup still sent a message")
	}
}
