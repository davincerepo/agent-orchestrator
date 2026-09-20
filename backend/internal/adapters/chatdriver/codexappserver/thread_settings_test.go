package codexappserver

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/persistenthost"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const fleetActualSettings = `{"thread":{"id":"thread-survived"},"model":"sol","reasoningEffort":"xhigh","serviceTier":"priority"}`

func fleetReconnectedConversation(t *testing.T) (*conversation, *scriptedServer) {
	t.Helper()
	d, srv := newTestDriver(t)
	srv.reply("thread/loaded/list", `{"data":["thread-survived"]}`)
	srv.reply("thread/resume", fleetActualSettings)
	srv.reply("model/list", `{"data":[{"id":"astra","isDefault":true,"defaultReasoningEffort":"medium"},{"id":"sol","defaultReasoningEffort":"medium","supportedReasoningEfforts":[{"reasoningEffort":"xhigh"}]}]}`)
	proc, err := d.spawn(context.Background(), "codex", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	d.persistent = true
	d.connectHost = func(context.Context, persistenthost.Config) (*persistenthost.Transport, error) {
		return &persistenthost.Transport{Stdin: proc.stdin, Stdout: proc.stdout, Reconnected: true}, nil
	}
	handle, err := d.Resume(context.Background(), ports.ChatResumeConfig{
		SessionID: "worker", ProviderConversationID: "thread-survived",
		DataDir: t.TempDir(), WorkspacePath: t.TempDir(),
		Model: "astra", Effort: "medium", ServiceTier: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if srv.sentMethod("thread/resume") || srv.sentMethod("initialize") {
		t.Fatal("reconnect must not block on native thread recovery")
	}
	return handle.(*conversation), srv
}

func fleetMethodCount(srv *scriptedServer, method string) int {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	n := 0
	for _, f := range srv.seen {
		if f.Method == method {
			n++
		}
	}
	return n
}

func TestFleetThreadSettingsRecoverActualAndCache(t *testing.T) {
	c, srv := fleetReconnectedConversation(t)
	srv.respondSequence("thread/loaded/list", `{"data":["other"],"nextCursor":"next"}`, `{"data":["thread-survived"]}`)
	for range 2 {
		models, err := c.ListModels(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != 2 || models[0].Default || !models[1].Default || models[1].DefaultEffort != "xhigh" || models[1].DefaultServiceTier != "priority" {
			t.Fatalf("did not recover actual settings: %+v", models)
		}
	}
	if fleetMethodCount(srv, "thread/resume") != 1 {
		t.Fatal("confirmed thread was read more than once")
	}
	f := srv.awaitFrame(func(f frame) bool { return f.Method == "thread/resume" })
	var params map[string]any
	if err := json.Unmarshal(f.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params) != 1 || params["threadId"] != "thread-survived" {
		t.Fatalf("read applied overrides: %v", params)
	}
	if srv.sentMethod("turn/start") || srv.sentMethod("config/read") {
		t.Fatal("display repair must not dispatch work or use global config")
	}
}

func TestFleetThreadSettingsFailureDoesNotAdvertiseCatalogDefaults(t *testing.T) {
	for _, failure := range []string{"not-loaded", "unsupported", "wrong-thread", "missing-model", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			c, srv := fleetReconnectedConversation(t)
			ctx := context.Background()
			switch failure {
			case "not-loaded":
				srv.reply("thread/loaded/list", `{"data":["other"]}`)
			case "unsupported":
				srv.replyError("thread/loaded/list", -32601, "unsupported")
			case "wrong-thread":
				srv.reply("thread/resume", `{"thread":{"id":"other"},"model":"sol"}`)
			case "missing-model":
				srv.reply("thread/resume", `{"thread":{"id":"thread-survived"}}`)
			case "timeout":
				srv.mu.Lock()
				delete(srv.responses, "thread/loaded/list")
				srv.mu.Unlock()
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			if models, err := c.ListModels(ctx); err == nil || len(models) != 0 {
				t.Fatalf("unconfirmed settings advertised: %+v, %v", models, err)
			}
			if fleetMethodCount(srv, "thread/loaded/list") != 1 || srv.sentMethod("model/list") {
				t.Fatal("failed read retried or leaked catalog defaults")
			}
			if (failure == "not-loaded" || failure == "unsupported" || failure == "timeout") && srv.sentMethod("thread/resume") {
				t.Fatal("attempted a cold resume without a confirmed loaded thread")
			}
			srv.mu.Lock()
			delete(srv.failures, "thread/loaded/list")
			srv.mu.Unlock()
			srv.reply("thread/loaded/list", `{"data":["thread-survived"]}`)
			srv.reply("thread/resume", fleetActualSettings)
			if models, err := c.ListModels(context.Background()); err != nil || len(models) != 2 || !models[1].Default {
				t.Fatalf("next visit could not retry: %+v, %v", models, err)
			}
		})
	}
}

func TestFleetThreadSettingsConcurrentReadsAndNewerNotification(t *testing.T) {
	c, srv := fleetReconnectedConversation(t)
	srv.mu.Lock()
	delete(srv.responses, "thread/resume")
	srv.mu.Unlock()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := c.ListModels(context.Background()); err != nil {
				t.Errorf("ListModels: %v", err)
			}
		})
	}
	f := srv.awaitFrame(func(f frame) bool { return f.Method == "thread/resume" })
	// A newer settings event must win over the delayed response snapshot.
	c.trackThreadSettings(notification{Method: "thread/settings/updated", Params: json.RawMessage(`{"threadId":"thread-survived","threadSettings":{"model":"sol","effort":"high","serviceTier":null}}`)})
	srv.push(`{"id":` + string(*f.ID) + `,"result":` + fleetActualSettings + `}`)
	wg.Wait()
	models, err := c.ListModels(context.Background())
	if err != nil || models[1].DefaultEffort != "high" || models[1].DefaultServiceTier != "default" {
		t.Fatalf("new settings overwritten: %+v, %v", models, err)
	}
	if fleetMethodCount(srv, "thread/resume") != 1 {
		t.Fatal("concurrent requests did not share the recovery")
	}
}

func TestFleetThreadSettingsNotificationAndTurnInvalidate(t *testing.T) {
	c, srv := fleetReconnectedConversation(t)
	// Foreign thread notifications must not mark this worker confirmed.
	c.trackThreadSettings(notification{Method: "thread/settings/updated", Params: json.RawMessage(`{"threadId":"other","threadSettings":{"model":"astra","effort":"medium"}}`)})
	if _, err := c.ListModels(context.Background()); err != nil || fleetMethodCount(srv, "thread/resume") != 1 {
		t.Fatalf("foreign thread affected recovery: %v", err)
	}
	if _, err := c.SendTurn(context.Background(), ports.ChatUserMessage{Text: "test"}); err != nil {
		t.Fatal(err)
	}
	srv.reply("thread/resume", `{"thread":{"id":"thread-survived"},"model":"custom","reasoningEffort":null,"serviceTier":null}`)
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 3 || models[0].Default || models[1].Default || !models[2].Default || models[2].ID != "custom" || models[2].DefaultEffort != "" || models[2].DefaultServiceTier != "default" {
		t.Fatalf("unknown catalog model/null settings not preserved: %+v", models)
	}
	if fleetMethodCount(srv, "thread/resume") != 2 {
		t.Fatal("turn did not invalidate the cached defaults")
	}
	// Exercise the real notification pump, followed by an observable barrier.
	srv.push(`{"method":"thread/settings/updated","params":{"threadId":"thread-survived","threadSettings":{"model":"sol","effort":"low","serviceTier":"priority"}}}`)
	srv.push(`{"method":"turn/started","params":{"threadId":"thread-survived","turn":{"id":"turn-2","status":"inProgress","items":[]}}}`)
	nextEvent(t, c.Events(), ports.ChatEventTurnStarted)
	models, err = c.ListModels(context.Background())
	if err != nil || !models[1].Default || models[1].DefaultEffort != "low" || fleetMethodCount(srv, "thread/resume") != 2 {
		t.Fatalf("provider update not cached: %+v, %v", models, err)
	}
}
