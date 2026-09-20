package controllers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

type fleetScopeStore struct {
	sessionsvc.Store
	sessions map[domain.SessionID]domain.SessionRecord
}

func (s *fleetScopeStore) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	rec, ok := s.sessions[id]
	return rec, ok, nil
}

func TestFleetHTTPRejectsCrossProjectDispatch(t *testing.T) {
	st := &fleetScopeStore{sessions: map[domain.SessionID]domain.SessionRecord{
		"caller": {ID: "caller", ProjectID: "a", Kind: domain.KindOrchestrator},
		"target": {ID: "target", ProjectID: "b", Kind: domain.KindWorker},
	}}
	// The real service is essential: this verifies caller context survives the
	// HTTP boundary and rejects before touching a runtime or project workspace.
	svc := sessionsvc.New(nil, st)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Sessions: svc, Conversations: chatsvc.New(chatsvc.Options{Sessions: st})}, httpd.ControlDeps{})
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/sessions/target/send", `{"callerSessionId":"caller","message":"[from target] test"}`},
		{"/api/v1/sessions/target/conversation/steer-or-send", `{"callerSessionId":"caller","text":"test","clientMessageId":"delivery-1"}`},
		{"/api/v1/sessions/target/conversation/steer", `{"callerSessionId":"caller","text":"test","clientMessageId":"delivery-2"}`},
		{"/api/v1/sessions", `{"callerSessionId":"caller","projectId":"b","kind":"worker","harness":"codex"}`},
		{"/api/v1/orchestrators/delegate", `{"callerSessionId":"caller","projectId":"b","brief":"test"}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, r)
			var body struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if w.Code != http.StatusForbidden || body.Code != "CROSS_PROJECT_DISPATCH" {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
