package claudeacp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Standing-prompt replacement.
//
// This Claude Code build persists the effective system prompt — including the
// appended standing instructions — as a `prompt_snapshot` attachment record on
// every turn of the transcript, so a plain fork (the adapter's session/fork
// copies the transcript verbatim) carries the old prompt into the copy and the
// new instructions passed at session/load lose to the replayed snapshots.
// Verified against claude-agent-acp 0.81.2 / Claude Code 2.1.281.
//
// The replacement therefore copies the transcript the way the adapter's fork
// cannot: every dialogue record is preserved with its fields intact, the
// prompt_snapshot attachments (the persisted prompt, not dialogue) are
// dropped, and each record's sessionId points at the fresh copy. Loading the
// new id with the new standing prompt then has no old prompt left in context.
//
// The copy is driver-level on purpose: it reads and writes durable native
// records only, needs no live ACP connection, and wrapping the live
// conversation would break the controller's feature-detection assertions.

var _ ports.ChatDriverPromptRefresher = (*checkpointDriver)(nil)

// RefreshStandingPrompt writes a prompt-replaced copy of the native transcript
// and returns its session id. The caller loads that id with the same system
// prompt; the original transcript is left untouched.
func (d *checkpointDriver) RefreshStandingPrompt(
	ctx context.Context,
	providerConversationID, systemPrompt string,
	env map[string]string,
) (string, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return "", errors.New("replacement system prompt is empty")
	}
	config, ok := d.plugin.(ports.AgentNativeSessionConfigProvider)
	if !ok {
		return "", errors.New("native config resolver unavailable")
	}
	locator, ok := d.plugin.(ports.AgentTranscriptLocator)
	if !ok {
		return "", errors.New("native transcript locator unavailable")
	}
	dir, err := config.NativeSessionConfigDir(ctx, env)
	if err != nil {
		return "", err
	}
	source, found, err := locator.LocateTranscript(ctx, ports.NativeSessionRef{
		NativeSessionID: providerConversationID, ConfigDir: dir,
	})
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("native transcript for %s was not found", providerConversationID)
	}
	return copyTranscriptWithoutPromptSnapshots(source)
}

// copyTranscriptWithoutPromptSnapshots writes a new transcript next to the
// source under a fresh session id. Dialogue records keep every field;
// prompt_snapshot attachment lines are omitted; other lines are copied
// byte-for-byte.
func copyTranscriptWithoutPromptSnapshots(source string) (string, error) {
	file, err := os.Open(source) //nolint:gosec // Path came from the plugin's locator, not provider input.
	if err != nil {
		return "", fmt.Errorf("open native transcript: %w", err)
	}
	defer func() { _ = file.Close() }()

	newID := uuid.NewString()
	target, err := os.CreateTemp(filepath.Dir(source), "."+newID+"-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create replacement transcript: %w", err)
	}
	targetPath := target.Name()
	defer func() {
		if target != nil {
			_ = target.Close()
			_ = os.Remove(targetPath)
		}
	}()

	dropped := 0
	rewritten := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	writer := bufio.NewWriter(target)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		out, drop, err := rewriteTranscriptLine(line, newID)
		if err != nil {
			return "", fmt.Errorf("rewrite transcript record: %w", err)
		}
		if drop {
			dropped++
			continue
		}
		if out == nil {
			out = line
		} else {
			rewritten++
		}
		if _, err := writer.Write(out); err != nil {
			return "", fmt.Errorf("write replacement transcript: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			return "", fmt.Errorf("write replacement transcript: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read native transcript: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return "", fmt.Errorf("write replacement transcript: %w", err)
	}
	if err := target.Close(); err != nil {
		target = nil
		return "", fmt.Errorf("write replacement transcript: %w", err)
	}
	target = nil
	final := filepath.Join(filepath.Dir(source), newID+".jsonl")
	if err := os.Rename(targetPath, final); err != nil {
		_ = os.Remove(targetPath)
		return "", fmt.Errorf("publish replacement transcript: %w", err)
	}
	if dropped == 0 && rewritten == 0 {
		// Nothing recognizable as either dialogue metadata or prompt snapshots:
		// the file is probably not a transcript this build understands. Say so
		// instead of publishing a silent no-op copy.
		_ = os.Remove(final)
		return "", errors.New("native transcript contains no recognizable records")
	}
	return newID, nil
}

// rewriteTranscriptLine decides what happens to one transcript line in the
// copy. drop=true means the line is a prompt snapshot and is omitted. A
// non-nil return rewrites the line with the replacement session id; nil means
// the original bytes pass through unchanged.
func rewriteTranscriptLine(line []byte, newID string) (out []byte, drop bool, err error) {
	var record struct {
		Type       string `json:"type"`
		Attachment *struct {
			Type string `json:"type"`
		} `json:"attachment"`
		SessionID *string `json:"sessionId"`
	}
	if err := json.Unmarshal(line, &record); err != nil {
		// A line Claude Code itself wrote but this build cannot parse is copied
		// verbatim; the loader remains the authority on its own format.
		return nil, false, nil //nolint:nilerr // an unparseable line is passed through, not failed
	}
	if record.Type == "attachment" && record.Attachment != nil && record.Attachment.Type == "prompt_snapshot" {
		return nil, true, nil
	}
	if record.SessionID == nil || *record.SessionID == newID {
		return nil, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return nil, false, err
	}
	encoded, err := json.Marshal(newID)
	if err != nil {
		return nil, false, err
	}
	fields["sessionId"] = encoded
	rebuilt, err := json.Marshal(fields)
	if err != nil {
		return nil, false, err
	}
	return rebuilt, false, nil
}
