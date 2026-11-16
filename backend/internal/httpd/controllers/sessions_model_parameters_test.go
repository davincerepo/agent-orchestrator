package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/go-chi/chi/v5"
)

type terminalParametersService struct {
	*fakeSessionService
	supported bool
	err       error
	id        domain.SessionID
}

func (s *terminalParametersService) ReadTerminalModelParameters(_ context.Context, id domain.SessionID) (ports.AgentConfig, bool, error) {
	s.id = id
	if !s.supported {
		return ports.AgentConfig{}, false, nil
	}
	return ports.AgentConfig{Model: "native-sol", Effort: "high"}, true, s.err
}

func TestFleetTerminalParametersHTTP(t *testing.T) {
	for _, test := range []struct {
		name      string
		supported bool
		err       error
		status    int
	}{
		{"observed with unknown Fast", true, nil, 200},
		{"unsupported", false, nil, 200},
		{"unconfirmed", true, apierr.Conflict("MODEL_PARAMETERS_UNCONFIRMED", "Unconfirmed", nil), 409},
		{"missing", true, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session"), 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := &terminalParametersService{supported: test.supported, err: test.err}
			r := chi.NewRouter()
			(&controllers.SessionsController{Svc: svc}).Register(r)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sessions/worker/terminal-model-parameters", nil))
			if response.Code != test.status || svc.id != "worker" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("response %d %s", response.Code, response.Body.String())
			}
			if test.status == 200 {
				var got controllers.TerminalModelParametersResponse
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Supported != test.supported || got.ServiceTier != "" {
					t.Fatalf("%+v", got)
				}
			}
		})
	}
}
