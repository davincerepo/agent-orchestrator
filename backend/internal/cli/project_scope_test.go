package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFleetSessionListProjectScope(t *testing.T) {
	for _, tc := range []struct {
		name, caller, want string
		args               []string
		fail               bool
	}{
		{name: "agent-default", caller: "opaque-id", want: "a"},
		{name: "explicit-read", caller: "opaque-id", args: []string{"--project", "b"}, want: "b"},
		{name: "all-projects", caller: "opaque-id", args: []string{"--all-projects"}},
		{name: "all-includes-orchestrators-only", caller: "opaque-id", args: []string{"--all"}, want: "a"},
		{name: "human-global"},
		{name: "human-explicit", args: []string{"--project", "b"}, want: "b"},
		{name: "unknown-caller", caller: "missing", fail: true},
		{name: "unknown-caller-explicit", caller: "missing", args: []string{"--project", "b"}, fail: true},
		{name: "conflicting-flags", caller: "opaque-id", args: []string{"--project", "b", "--all-projects"}, fail: true},
		{name: "invalid-caller", caller: "../oops", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := setConfigEnv(t)
			t.Setenv("AO_SESSION_ID", tc.caller)
			t.Setenv("AO_PROJECT_ID", "wrong-env-project")
			var scopes []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/sessions/opaque-id":
					_, _ = io.WriteString(w, `{"session":{"id":"opaque-id","projectId":"a"}}`)
				case "/api/v1/sessions":
					scopes = append(scopes, r.URL.Query().Get("project"))
					_, _ = io.WriteString(w, `{"sessions":[]}`)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(srv.Close)
			writeRunFileFor(t, cfg, srv)
			_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, append([]string{"session", "ls", "--json"}, tc.args...)...)
			if tc.fail {
				if err == nil || len(scopes) != 0 {
					t.Fatalf("invalid context queried sessions: %v, %v", scopes, err)
				}
				return
			}
			if err != nil || len(scopes) != 2 {
				t.Fatalf("list/count requests = %v, err = %v", scopes, err)
			}
			for _, scope := range scopes {
				if scope != tc.want {
					t.Fatalf("project = %q, want %q", scope, tc.want)
				}
			}
		})
	}
}

func TestFleetCLIDispatchProjectScope(t *testing.T) {
	for _, operation := range []string{"send", "steer", "recover", "spawn"} {
		for _, tc := range []struct {
			name, caller, project string
			fail                  bool
		}{
			{"own-project", "caller", "a", false},
			{"agent-default", "caller", "a", false},
			{"cross-project", "caller", "b", true},
			{"unknown-caller", "missing", "a", true},
			{"human", "", "b", false},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				cfg := setConfigEnv(t)
				t.Setenv("AO_SESSION_ID", tc.caller)
				t.Setenv("AO_PROJECT_ID", "wrong-env-project")
				var writes []map[string]any
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch {
					case r.Method == "GET" && r.URL.Path == "/api/v1/sessions/caller":
						_, _ = io.WriteString(w, `{"session":{"id":"caller","projectId":"a"}}`)
					case r.Method == "GET" && r.URL.Path == "/api/v1/sessions/target":
						_, _ = io.WriteString(w, `{"session":{"id":"target","projectId":"`+tc.project+`"}}`)
					case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/projects/"):
						_, _ = io.WriteString(w, `{"project":{"id":"`+tc.project+`","config":{"worker":{"agent":"codex"}}}}`)
					case r.Method == "POST" && (r.URL.Path == "/api/v1/sessions" || r.URL.Path == "/api/v1/sessions/target/send" || r.URL.Path == "/api/v1/sessions/target/conversation/steer-or-send"):
						var body map[string]any
						_ = json.NewDecoder(r.Body).Decode(&body)
						writes = append(writes, body)
						_, _ = io.WriteString(w, `{"ok":true,"outcome":"steered","providerTurnId":"turn-1","session":{"id":"new","status":"idle"}}`)
					default:
						http.NotFound(w, r)
					}
				}))
				t.Cleanup(srv.Close)
				writeRunFileFor(t, cfg, srv)
				args := []string{"send", "--session", "target", "--message", "test"}
				if operation == "steer" {
					args = append(args, "--steer")
				}
				if operation == "recover" {
					args = []string{"send", "--session", "target", "--steer", "--recover-only", "--client-message-id", "delivery-1"}
				}
				if operation == "spawn" {
					args = []string{"spawn", "--project", tc.project, "--agent", "codex", "--name", "worker", "--skip-agent-check"}
					if tc.name == "agent-default" {
						args = []string{"spawn", "--agent", "codex", "--name", "worker", "--skip-agent-check"}
					}
				}
				_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...)
				if tc.fail {
					if err == nil || len(writes) != 0 {
						t.Fatalf("rejected dispatch wrote: %v, %v", writes, err)
					}
					return
				}
				if err != nil || len(writes) != 1 {
					t.Fatalf("writes = %v, err = %v", writes, err)
				}
				if operation == "recover" && writes[0]["text"] != "" {
					t.Fatalf("recovery must not resend text: %v", writes[0])
				}
				caller, _ := writes[0]["callerSessionId"].(string)
				if caller != tc.caller {
					t.Fatalf("caller not transmitted: %v", writes[0])
				}
			})
		}
	}
}
