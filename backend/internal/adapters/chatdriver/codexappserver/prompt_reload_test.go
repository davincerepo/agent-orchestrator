package codexappserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// openReloadableConversation starts a conversation whose launch context points
// at a scratch CODEX_HOME the test controls. The default scripted driver only
// accepts POSIX-absolute workspaces, so the workspace comes from t.TempDir().
func openReloadableConversation(t *testing.T) (*conversation, *scriptedServer, string) {
	t.Helper()
	d, srv := newTestDriver(t)
	workspace := t.TempDir()
	conv, err := d.Start(context.Background(), ports.ChatStartConfig{WorkspacePath: workspace})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = conv.Close() })
	c := conv.(*conversation)
	c.launch.codexHome = t.TempDir()
	return c, srv, workspace
}

func writeRollout(t *testing.T, codexHome, threadID, content string) {
	t.Helper()
	dir := filepath.Join(codexHome, "sessions", "2026", "09", "27")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	name := "rollout-2026-09-27T00-00-00-" + threadID + ".jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
}

const reloadRolloutFixture = `{"type":"session_meta","payload":{"id":"thread-1","cwd":"/ws"}}
{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"OLD STANDING PROMPT"}]}}
{"type":"turn_context","payload":{"turn_id":"turn-1","model":"gpt-test"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"summarize relativity"}]}}
{"type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking"}]}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"it bends"}]}}
{"type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1"}}
`

func TestRefreshStandingPromptStartsNewThreadAndInjectsDialogue(t *testing.T) {
	conv, srv, workspace := openReloadableConversation(t)
	writeRollout(t, conv.launch.codexHome, "thread-1", reloadRolloutFixture)
	srv.reply("thread/start", `{"thread":{"id":"thread-9"},"model":"gpt-test"}`)
	srv.reply("thread/inject_items", `{}`)

	forked, err := conv.RefreshStandingPrompt(context.Background(), "NEW STANDING PROMPT")
	if err != nil {
		t.Fatalf("RefreshStandingPrompt: %v", err)
	}
	if forked != "thread-9" {
		t.Fatalf("replacement thread = %q, want thread-9", forked)
	}

	// The conversation's own thread/start frame is also in the log; the
	// replacement is the one carrying developer instructions.
	start := srv.awaitFrame(func(f frame) bool {
		return f.Method == "thread/start" && strings.Contains(string(f.Params), "developerInstructions")
	})
	var startParams map[string]any
	if err := json.Unmarshal(start.Params, &startParams); err != nil {
		t.Fatalf("thread/start params: %v", err)
	}
	if startParams["developerInstructions"] != "NEW STANDING PROMPT" {
		t.Errorf("developerInstructions = %#v, want the replacement prompt", startParams["developerInstructions"])
	}
	if startParams["cwd"] != workspace {
		t.Errorf("cwd = %#v, want the recorded workspace %q", startParams["cwd"], workspace)
	}

	inject := srv.awaitFrame(func(f frame) bool { return f.Method == "thread/inject_items" })
	var injectParams struct {
		ThreadID string            `json:"threadId"`
		Items    []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(inject.Params, &injectParams); err != nil {
		t.Fatalf("thread/inject_items params: %v", err)
	}
	if injectParams.ThreadID != "thread-9" {
		t.Errorf("inject threadId = %q, want thread-9", injectParams.ThreadID)
	}
	if len(injectParams.Items) != 3 {
		t.Fatalf("injected items = %d, want the three dialogue items (user, reasoning, assistant): %s",
			len(injectParams.Items), inject.Params)
	}
	var first struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(injectParams.Items[0], &first); err != nil {
		t.Fatalf("first injected item: %v", err)
	}
	if first.Role != "user" {
		t.Errorf("first injected item role = %q, want user", first.Role)
	}
	for _, item := range injectParams.Items {
		if strings.Contains(string(item), "OLD STANDING PROMPT") {
			t.Error("injected history carries the old standing prompt")
		}
	}
}

func TestRefreshStandingPromptNeedsTheSourceRollout(t *testing.T) {
	conv, _, _ := openReloadableConversation(t)

	if _, err := conv.RefreshStandingPrompt(context.Background(), "NEW"); err == nil {
		t.Fatal("replacement succeeded without a source rollout")
	}
}

func TestExtractDialogueItemsDropsOnlyDeveloperMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte(reloadRolloutFixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	items, err := extractDialogueItems(path)
	if err != nil {
		t.Fatalf("extractDialogueItems: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (session_meta, developer message, turn_context, and event_msg excluded)", len(items))
	}
	joined := ""
	for _, item := range items {
		joined += string(item)
	}
	if strings.Contains(joined, "developer") || strings.Contains(joined, "OLD STANDING PROMPT") {
		t.Error("developer instructions leaked into the dialogue items")
	}
	if !strings.Contains(joined, "summarize relativity") || !strings.Contains(joined, "it bends") {
		t.Error("dialogue content was not preserved verbatim")
	}
	if !strings.Contains(joined, `"reasoning"`) {
		t.Error("reasoning items were dropped; they are part of the dialogue record")
	}
}

func TestLocateRolloutFindsOnlyActiveRollouts(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "01", "02")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	archived := filepath.Join(dir, "rollout-2026-01-02T00-00-00-thread-1.jsonl.zst")
	active := filepath.Join(dir, "rollout-2026-01-02T00-00-01-thread-1.jsonl")
	for path, content := range map[string]string{
		archived: "compressed",
		active:   "{}",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	found, err := locateRollout(context.Background(), home, "thread-1")
	if err != nil {
		t.Fatalf("locateRollout: %v", err)
	}
	if found != active {
		t.Errorf("located %q, want the active rollout %q", found, active)
	}

	if _, err := locateRollout(context.Background(), home, "thread-2"); err == nil {
		t.Error("locateRollout found a rollout for an unknown thread")
	}
}
