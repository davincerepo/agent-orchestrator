package codexappserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/codexappserver/codexproto"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Standing-prompt replacement.
//
// Codex persists the developer instructions passed to thread/start as
// developer-role messages inside the rollout (re-injected at each context
// rebuild), which is why neither a resume override, a fork override, nor
// thread/settings/update can replace them: the old instructions replay as
// conversation content. Verified against codex 0.157.0.
//
// The replacement therefore copies the conversation the way Codex's own fork
// cannot: a fresh thread is started with the NEW instructions and the source
// rollout's dialogue is injected with thread/inject_items, omitting only the
// developer-role messages. Every dialogue item — user prompts, assistant
// messages, reasoning, tool calls — is preserved byte-for-byte.

// threadLaunchContext carries what a replacement thread needs from the launch
// that created this conversation. It is recorded when the driver starts or
// resumes the conversation so the copy runs against the same tree and
// permissions even after daemon-level reconnects.
type threadLaunchContext struct {
	workdir     string
	permissions ports.PermissionMode
	// codexHome is where the app-server process for this conversation writes
	// rollouts, resolved from the launch environment.
	codexHome string
}

// resolveCodexHome follows the same chain as the Codex agent plugin: the launch
// environment is authoritative because a managed account home relocates every
// provider write.
func resolveCodexHome(env map[string]string) (string, error) {
	if home := strings.TrimSpace(env["CODEX_HOME"]); home != "" {
		return home, nil
	}
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve rollout root: %w", err)
	}
	return filepath.Join(home, ".codex"), nil
}

// resolveCodexHomeBestEffort keeps launch recording non-fatal: a home that
// cannot be resolved now fails later, at the replacement attempt, with the
// full error instead of blocking an ordinary session start.
func resolveCodexHomeBestEffort(env map[string]string) string {
	home, err := resolveCodexHome(env)
	if err != nil {
		return ""
	}
	return home
}

// RefreshStandingPrompt starts a new thread with the replacement instructions
// and injects this thread's dialogue history into it, then returns the new
// thread id. The caller closes this conversation and resumes the new id; the
// resume re-applies the same system prompt, which a fresh thread honors.
func (c *conversation) RefreshStandingPrompt(ctx context.Context, systemPrompt string) (string, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return "", errors.New("replacement system prompt is empty")
	}

	// Serialized with turn dispatch: the rollout read below must describe the
	// same conversation the provider holds when the new thread is created.
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	c.mu.Lock()
	launch := c.launch
	threadID := c.threadID
	model := c.threadModel
	effort := c.threadEffort
	c.mu.Unlock()
	if threadID == "" {
		return "", errors.New("conversation has no provider thread")
	}
	if launch.workdir == "" {
		return "", errors.New("conversation has no recorded launch workspace")
	}

	rolloutPath, err := locateRollout(ctx, launch.codexHome, threadID)
	if err != nil {
		return "", err
	}
	items, err := extractDialogueItems(rolloutPath)
	if err != nil {
		return "", err
	}

	policy, sandbox := approvalSettings(launch.permissions)
	params := map[string]any{
		"cwd":                   launch.workdir,
		"approvalPolicy":        policy,
		"approvalsReviewer":     approvalReviewer(launch.permissions),
		"sandbox":               sandbox,
		"developerInstructions": systemPrompt,
	}
	if model != "" {
		params["model"] = model
	}
	if effort != "" {
		params["config"] = map[string]any{"model_reasoning_effort": effort}
	}
	var startResp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := c.conn.request(ctx, codexproto.MethodThreadStart, params, &startResp); err != nil {
		return "", asRefusal(fmt.Errorf("thread/start replacement: %w", err))
	}
	if startResp.Thread.ID == "" || startResp.Thread.ID == threadID {
		return "", fmt.Errorf("thread/start replacement returned %q", startResp.Thread.ID)
	}

	if len(items) > 0 {
		if err := c.conn.request(ctx, codexproto.MethodThreadInjectItems, codexproto.ThreadInjectItemsParams{
			ThreadID: startResp.Thread.ID,
			Items:    items,
		}, nil); err != nil {
			return "", asRefusal(fmt.Errorf("thread/inject_items history: %w", err))
		}
	}
	return startResp.Thread.ID, nil
}

// locateRollout finds the active rollout file for a thread below CODEX_HOME.
// Archived (".zst") rollouts are excluded, matching what thread/resume accepts.
func locateRollout(ctx context.Context, codexHome, threadID string) (string, error) {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return "", errors.New("thread id is empty")
	}
	suffix := "-" + id + ".jsonl"
	sessionsDir := filepath.Join(codexHome, "sessions")
	found := ""
	err := filepath.WalkDir(sessionsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "rollout-") || !strings.HasSuffix(entry.Name(), suffix) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Size() > 0 {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no rollout found for thread %s under %s", id, sessionsDir)
		}
		return "", fmt.Errorf("inspect rollouts under %s: %w", sessionsDir, err)
	}
	if found == "" {
		return "", fmt.Errorf("no rollout found for thread %s under %s", id, sessionsDir)
	}
	return found, nil
}

// rolloutRecord is the envelope every rollout line shares.
type rolloutRecord struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// extractDialogueItems returns the rollout's response_item payloads verbatim,
// omitting developer-role messages. Those messages are the persisted form of
// the standing instructions, not dialogue: keeping them would leave the old
// prompt in the model's context alongside the new one.
func extractDialogueItems(path string) ([]json.RawMessage, error) {
	file, err := os.Open(path) //nolint:gosec // Path resolved from CODEX_HOME scan, not provider input.
	if err != nil {
		return nil, fmt.Errorf("open rollout: %w", err)
	}
	defer func() { _ = file.Close() }()

	items := make([]json.RawMessage, 0, 64)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var item struct {
		Type string `json:"type"`
		Role string `json:"role"`
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		var record rolloutRecord
		if err := json.Unmarshal(line, &record); err != nil || record.Type != "response_item" {
			// session_meta, turn_context, and event_msg records are not model
			// context; the new thread reconstructs its own. A line that does not
			// parse is left to Codex rather than guessed at here.
			continue
		}
		item.Type = ""
		item.Role = ""
		if json.Unmarshal(record.Payload, &item) == nil &&
			item.Type == "message" && item.Role == "developer" {
			continue
		}
		// The scanner's buffer is reused per line, so each kept payload needs
		// its own copy before it outlives the loop.
		trimmed := append(json.RawMessage(nil), record.Payload...)
		items = append(items, trimmed)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read rollout: %w", err)
	}
	return items, nil
}
