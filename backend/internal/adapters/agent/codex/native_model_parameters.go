package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const nativeSettingsTailLimit = 16 << 20

// ReadNativeModelParameters reads the newest durable settings in this exact
// rollout. Modern Codex persists thread_settings_applied even without a turn;
// older histories only prove model/effort from turn_context. In particular,
// absence of service_tier in turn_context is not evidence that Fast is off.
func (p *Plugin) ReadNativeModelParameters(ctx context.Context, ref ports.NativeSessionRef) (ports.AgentConfig, error) {
	path, found, err := p.LocateTranscript(ctx, ref)
	if err != nil || !found {
		return ports.AgentConfig{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return ports.AgentConfig{}, fmt.Errorf("read Codex thread settings: %w", err)
	}
	defer f.Close()
	header, err := bufio.NewReader(io.LimitReader(f, 1<<20)).ReadBytes('\n')
	if err != nil && err != io.EOF {
		return ports.AgentConfig{}, err
	}
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			ID        string `json:"id"`
			SessionID string `json:"session_id"`
		} `json:"payload"`
	}
	_ = json.Unmarshal(header, &meta)
	if meta.Type != "session_meta" {
		return ports.AgentConfig{}, fmt.Errorf("Codex rollout has no session metadata header")
	}
	fileID := meta.Payload.ID
	if fileID == "" {
		fileID = meta.Payload.SessionID
	}
	if fileID != "" && fileID != ref.NativeSessionID {
		return ports.AgentConfig{}, fmt.Errorf("Codex rollout belongs to a different thread")
	}
	info, err := f.Stat()
	if err != nil {
		return ports.AgentConfig{}, err
	}
	// Bound IO/memory even for very large histories. Never consume a partial
	// record at either end or treat message/tool text as configuration.
	offset := max(int64(0), info.Size()-nativeSettingsTailLimit)
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ports.AgentConfig{}, err
	}
	reader := bufio.NewReader(io.LimitReader(f, info.Size()-offset))
	if offset > 0 {
		if _, err := reader.ReadBytes('\n'); err != nil {
			return ports.AgentConfig{}, fmt.Errorf("read Codex settings tail: %w", err)
		}
	}
	var latest ports.AgentConfig
	for {
		if err := ctx.Err(); err != nil {
			return ports.AgentConfig{}, err
		}
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return ports.AgentConfig{}, err
		}
		if !bytes.Contains(line, []byte(`"thread_settings_applied"`)) && !bytes.Contains(line, []byte(`"turn_context"`)) {
			continue
		}
		var envelope struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			return ports.AgentConfig{}, fmt.Errorf("invalid Codex settings record: %w", err)
		}
		if envelope.Type != "event_msg" && envelope.Type != "turn_context" {
			continue
		}
		var record struct {
			Payload struct {
				Type     string `json:"type"`
				ThreadID string `json:"thread_id"`
				Model    string `json:"model"`
				Effort   string `json:"effort"`
				Settings struct {
					Model  string `json:"model"`
					Effort string `json:"reasoning_effort"`
					Tier   string `json:"service_tier"`
				} `json:"thread_settings"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return ports.AgentConfig{}, fmt.Errorf("invalid Codex settings record: %w", err)
		}
		switch {
		case envelope.Type == "event_msg" && record.Payload.Type == "thread_settings_applied":
			if record.Payload.ThreadID != "" && record.Payload.ThreadID != ref.NativeSessionID {
				continue // copied parent/subagent snapshot is not this thread's state
			}
			if record.Payload.ThreadID == "" && fileID != ref.NativeSessionID {
				continue
			}
			s := record.Payload.Settings
			if s.Model == "" {
				return ports.AgentConfig{}, fmt.Errorf("Codex thread settings have no model")
			}
			tier := s.Tier
			if tier == "" {
				tier = "default" // full snapshot: absent/null means Fast off
			}
			latest = ports.AgentConfig{Model: s.Model, Effort: s.Effort, ServiceTier: tier}
		case envelope.Type == "turn_context" && fileID == ref.NativeSessionID && record.Payload.Model != "":
			latest.Model, latest.Effort = record.Payload.Model, record.Payload.Effort
		}
	}
	if err := latest.Validate(); err != nil {
		return ports.AgentConfig{}, fmt.Errorf("invalid Codex model parameters: %w", err)
	}
	return latest, nil
}
