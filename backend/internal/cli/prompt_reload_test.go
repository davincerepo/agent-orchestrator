package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func reloadPromptServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions/demo-1":
			_, _ = io.WriteString(w, `{"session":{"id":"demo-1","projectId":"demo","kind":"worker","activity":"idle","isTerminated":false,"createdAt":"2026-08-01T00:00:00Z","updatedAt":"2026-08-01T00:00:00Z","status":"idle"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/demo-1/prompt/reload":
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSessionReloadPromptReportsTheReplacementConversation(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := reloadPromptServer(t, http.StatusOK,
		`{"ok":true,"sessionId":"demo-1","providerConversationId":"thread-9","branchId":"branch-7"}`)
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "session", "reload-prompt", "demo-1")
	if err != nil {
		t.Fatalf("session reload-prompt failed: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "session demo-1 prompt reloaded") || !strings.Contains(out, "thread-9") {
		t.Fatalf("unexpected reload output:\n%s", out)
	}
}

func TestSessionReloadPromptJSONOutput(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := reloadPromptServer(t, http.StatusOK,
		`{"ok":true,"sessionId":"demo-1","providerConversationId":"thread-9","branchId":"branch-7"}`)
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "session", "reload-prompt", "demo-1", "--json")
	if err != nil {
		t.Fatalf("session reload-prompt --json failed: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, `"providerConversationId": "thread-9"`) {
		t.Fatalf("unexpected JSON output:\n%s", out)
	}
}

func TestSessionReloadPromptRequiresASessionID(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := reloadPromptServer(t, http.StatusOK, `{}`)
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "session", "reload-prompt")
	if err == nil || !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("missing-arg err = %v, want a usage error", err)
	}
}

func TestSessionReloadPromptSurfacesTheDaemonEnvelope(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := reloadPromptServer(t, http.StatusConflict,
		`{"error":"conflict","code":"CHAT_TURN_RUNNING","message":"A provider turn is still running; retry once it finishes","requestId":"r-1"}`)
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "session", "reload-prompt", "demo-1")
	if err == nil {
		t.Fatal("reload against a busy session unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "A provider turn is still running") &&
		!strings.Contains(errOut, "A provider turn is still running") {
		t.Fatalf("neither the error (%v) nor stderr (%s) carries the daemon message", err, errOut)
	}
}
