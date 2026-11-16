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
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
)

type messageLimitChatService struct {
	*fakeConversationService
	sent string
}

func (s *messageLimitChatService) SteerOrSend(_ context.Context, _ domain.SessionID, msg ports.ChatUserMessage, _ bool) (chatsvc.SteerOrSendResult, error) {
	s.sent = msg.Text
	return chatsvc.SteerOrSendResult{Steered: true}, nil
}

func (s *messageLimitChatService) Steer(_ context.Context, _ domain.SessionID, msg ports.ChatUserMessage) (chatsvc.SteerResult, error) {
	s.sent = msg.Text
	return chatsvc.SteerResult{}, nil
}

func TestSessionsAPI_SendMessageByteLimit(t *testing.T) {
	const limit = 1 << 20
	utf8AtLimit := strings.Repeat("中", limit/3) + "a"
	for _, route := range []string{"send", "conversation/steer-or-send", "conversation/steer"} {
		for _, tc := range []struct {
			name, message string
			tooLong       bool
		}{
			{"past old limit", strings.Repeat("a", 4097), false},
			{"ASCII at limit", strings.Repeat("a", limit), false},
			{"UTF-8 at limit", utf8AtLimit, false},
			{"JSON escaping at limit", strings.Repeat("<", limit), false},
			{"ASCII over limit", strings.Repeat("a", limit+1), true},
			{"UTF-8 over limit", utf8AtLimit + "b", true},
			{"sender prefix counts", "[from caller] " + strings.Repeat("a", limit), true},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				sessions := newFakeSessionService()
				chat := &messageLimitChatService{fakeConversationService: &fakeConversationService{}}
				router := httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil,
					httpd.APIDeps{Sessions: sessions, Conversations: chat}, httpd.ControlDeps{})
				payload, err := json.Marshal(map[string]string{"message": tc.message, "text": tc.message, "clientMessageId": "delivery-1"})
				if err != nil {
					t.Fatal(err)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/sessions/demo-1/"+route, strings.NewReader(string(payload))))
				if tc.tooLong {
					assertErrorCode(t, w.Body.Bytes(), w.Code, http.StatusBadRequest, "MESSAGE_TOO_LONG")
					for _, text := range []string{"1048576 bytes", "1 MiB", "file", "path"} {
						if !strings.Contains(w.Body.String(), text) {
							t.Errorf("error does not include %q: %s", text, w.Body.String())
						}
					}
					if sessions.sent != "" || chat.sent != "" {
						t.Fatal("oversized message reached service")
					}
					return
				}
				wantStatus, sent := http.StatusAccepted, chat.sent
				if route == "send" {
					wantStatus, sent = http.StatusOK, sessions.sent
				}
				if w.Code != wantStatus || sent != tc.message {
					t.Fatalf("status=%d, sent=%d bytes, want status=%d, %d bytes", w.Code, len(sent), wantStatus, len(tc.message))
				}
			})
		}
	}
}
