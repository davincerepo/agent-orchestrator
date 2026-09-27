package claudeacp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The surgery must copy a transcript the way the adapter's own fork cannot:
// dialogue records keep their fields, prompt_snapshot attachments (the
// persisted standing prompt) are dropped, and every record points at the fresh
// session id.
func TestCopyTranscriptWithoutPromptSnapshots(t *testing.T) {
	dir := t.TempDir()
	const oldID = "11111111-1111-4111-8111-111111111111"
	source := filepath.Join(dir, oldID+".jsonl")
	lines := []string{
		`{"type":"summary","summary":"relativity chat","leafUuid":"u1"}`,
		`{"type":"user","uuid":"u1","sessionId":"` + oldID + `","message":{"content":"summarize relativity"}}`,
		`{"type":"attachment","sessionId":"` + oldID + `","attachment":{"type":"prompt_snapshot","systemPrompt":["base","OLD STANDING PROMPT"],"reminderFold":false}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"` + oldID + `","message":{"content":"it bends"}}`,
		`{"type":"attachment","sessionId":"` + oldID + `","attachment":{"type":"total_tokens_reminder"}}`,
		`not json at all`,
	}
	if err := os.WriteFile(source, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write source transcript: %v", err)
	}

	newID, err := copyTranscriptWithoutPromptSnapshots(source)
	if err != nil {
		t.Fatalf("copyTranscriptWithoutPromptSnapshots: %v", err)
	}
	if newID == oldID {
		t.Fatalf("replacement id %q equals the source id", newID)
	}
	raw, err := os.ReadFile(filepath.Join(dir, newID+".jsonl"))
	if err != nil {
		t.Fatalf("read replacement transcript: %v", err)
	}
	text := string(raw)

	if strings.Contains(text, "prompt_snapshot") || strings.Contains(text, "OLD STANDING PROMPT") {
		t.Errorf("replacement still carries the persisted prompt:\n%s", text)
	}
	if !strings.Contains(text, "summarize relativity") || !strings.Contains(text, "it bends") {
		t.Errorf("dialogue records were not preserved:\n%s", text)
	}
	if !strings.Contains(text, "total_tokens_reminder") {
		t.Errorf("non-prompt attachments were dropped:\n%s", text)
	}
	if strings.Contains(text, oldID) {
		t.Errorf("replacement still references the source session id:\n%s", text)
	}
	if !strings.Contains(text, `"type":"summary"`) || !strings.Contains(text, "not json at all") {
		t.Errorf("records without a sessionId must pass through byte-for-byte:\n%s", text)
	}

	// Every rewritten record decodes and points at the replacement id.
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var record struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		if record.SessionID == "" {
			continue
		}
		count++
		if record.SessionID != newID {
			t.Errorf("record session id = %q, want %q", record.SessionID, newID)
		}
	}
	if count < 3 {
		t.Errorf("rewrote %d session-scoped records, want at least 3", count)
	}

	// The source transcript is untouched.
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read source transcript: %v", err)
	}
	if !strings.Contains(string(original), "OLD STANDING PROMPT") {
		t.Error("the source transcript was modified")
	}
}

func TestCopyTranscriptRefusesAFileWithNoRecognizableRecords(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "22222222-2222-4222-8222-222222222222.jsonl")
	if err := os.WriteFile(source, []byte("garbage\nmore garbage\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if _, err := copyTranscriptWithoutPromptSnapshots(source); err == nil {
		t.Fatal("copy accepted a file with no recognizable records")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("left %d files behind, want only the source", len(entries))
	}
}
