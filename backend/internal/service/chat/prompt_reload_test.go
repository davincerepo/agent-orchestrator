package chat_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"

	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
)

// reloadRecorder is a live conversation whose provider can copy itself under
// replacement standing instructions.
type reloadRecorder struct {
	*fakeConversation

	mu         sync.Mutex
	prompts    []string
	refreshErr error
}

func (r *reloadRecorder) RefreshStandingPrompt(_ context.Context, systemPrompt string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, systemPrompt)
	if r.refreshErr != nil {
		return "", r.refreshErr
	}
	return "thread-replaced", nil
}

func (r *reloadRecorder) lastPrompt() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.prompts) == 0 {
		return ""
	}
	return r.prompts[len(r.prompts)-1]
}

// driverRefresher is a driver-level refresher, the Claude Code shape: the copy
// is prepared from durable records with no live conversation involved.
type driverRefresher struct {
	mu           sync.Mutex
	copies       int
	conversation string
	prompt       string
	env          map[string]string
}

func (d *driverRefresher) RefreshStandingPrompt(_ context.Context, providerConversationID, systemPrompt string, env map[string]string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.copies++
	d.conversation = providerConversationID
	d.prompt = systemPrompt
	d.env = env
	return "transcript-replaced", nil
}

func (d *driverRefresher) observed() (string, string, map[string]string, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conversation, d.prompt, d.env, d.copies
}

type reloadState struct {
	mu            sync.Mutex
	resumes       []ports.ChatResumeConfig
	failResumeFor string
}

type reloadHarness struct {
	svc       *chatsvc.Service
	st        *sqlite.Store
	source    *reloadRecorder
	state     *reloadState
	replaced  *fakeConversation
	restored  *fakeConversation
	refresher *driverRefresher
}

func newReloadHarness(t *testing.T, withDriverRefresher bool) *reloadHarness {
	t.Helper()
	st := openStore(t)
	source := &reloadRecorder{fakeConversation: newFakeConversation()}
	source.providerConversationID = "thread-1"
	replaced := newFakeConversation()
	replaced.providerConversationID = "thread-replaced"
	restored := newFakeConversation()
	restored.providerConversationID = "thread-1"
	state := &reloadState{}
	refresher := &driverRefresher{}
	// A driver-level refresher is only reached when the live conversation
	// cannot copy itself, so the initial conversation is the plain fake.
	initial := ports.ChatConversation(source)
	if withDriverRefresher {
		initial = source.fakeConversation
	}
	driver := fakeDriver{conv: initial}
	driver.start = func(cfg ports.ChatStartConfig) (ports.ChatConversation, error) {
		return initial, nil
	}
	driver.resume = func(cfg ports.ChatResumeConfig) (ports.ChatConversation, error) {
		state.mu.Lock()
		defer state.mu.Unlock()
		state.resumes = append(state.resumes, cfg)
		if state.failResumeFor != "" && cfg.ProviderConversationID == state.failResumeFor {
			return nil, errors.New("resume refused")
		}
		switch cfg.ProviderConversationID {
		case "thread-replaced", "transcript-replaced":
			return replaced, nil
		case "thread-1":
			return restored, nil
		}
		return nil, errors.New("unexpected provider conversation: " + cfg.ProviderConversationID)
	}
	var (
		idMu    sync.Mutex
		nextID  int
		clockMu sync.Mutex
		clock   = time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	)
	svc := chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st,
		Drivers:  fakeRegistry{driver: registryDriver(driver, refresher, withDriverRefresher)},
		Log:      slog.New(slog.DiscardHandler),
		Activity: &recordingActivity{},
		NewID: func() string {
			idMu.Lock()
			defer idMu.Unlock()
			nextID++
			return fmt.Sprintf("reload-id-%d", nextID)
		},
		Now: func() time.Time {
			clockMu.Lock()
			defer clockMu.Unlock()
			return clock
		},
	})
	workspace := t.TempDir()
	if _, err := svc.Start(context.Background(), chatsvc.StartConfig{
		SessionID: testSession, ProjectID: testProject, Kind: domain.KindWorker,
		Harness: domain.HarnessCodex, WorkspacePath: workspace,
		SystemPrompt: "old standing prompt", Env: map[string]string{"CODEX_HOME": "/opt/codex"},
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), testSession) })
	return &reloadHarness{svc: svc, st: st, source: source, state: state, replaced: replaced, restored: restored, refresher: refresher}
}

// refresherDriver lets a driver-level refresher ride along the fake driver.
type refresherDriver struct {
	fakeDriver
	refresher *driverRefresher
}

func (d refresherDriver) RefreshStandingPrompt(ctx context.Context, providerConversationID, systemPrompt string, env map[string]string) (string, error) {
	return d.refresher.RefreshStandingPrompt(ctx, providerConversationID, systemPrompt, env)
}

// registryDriver picks the plain fake or the refresher-decorated fake.
func registryDriver(driver fakeDriver, refresher *driverRefresher, withRefresher bool) ports.ChatDriver {
	if withRefresher {
		return refresherDriver{fakeDriver: driver, refresher: refresher}
	}
	return driver
}

func TestReloadStandingPromptRebindsThroughConversationRefresher(t *testing.T) {
	h := newReloadHarness(t, false)

	result, err := h.svc.ReloadChatPrompt(context.Background(), testSession, "new standing prompt")
	if err != nil {
		t.Fatalf("ReloadChatPrompt: %v", err)
	}
	if result.ProviderConversationID != "thread-replaced" {
		t.Errorf("result conversation = %q, want thread-replaced", result.ProviderConversationID)
	}
	if got := h.source.lastPrompt(); got != "new standing prompt" {
		t.Errorf("refresher prompt = %q, want the replacement prompt", got)
	}
	if len(h.state.resumes) != 1 {
		t.Fatalf("resumes = %d, want 1", len(h.state.resumes))
	}
	resume := h.state.resumes[0]
	if resume.ProviderConversationID != "thread-replaced" {
		t.Errorf("resume conversation = %q, want thread-replaced", resume.ProviderConversationID)
	}
	if resume.SystemPrompt != "new standing prompt" {
		t.Errorf("resume system prompt = %q, want the replacement prompt", resume.SystemPrompt)
	}

	conversation, err := h.st.ConversationForSession(context.Background(), testSession)
	if err != nil {
		t.Fatalf("ConversationForSession: %v", err)
	}
	if conversation.ActiveBranchID != result.BranchID {
		t.Errorf("active branch = %q, want %q", conversation.ActiveBranchID, result.BranchID)
	}
	branch, err := h.st.ConversationBranch(context.Background(), conversation.ID, result.BranchID)
	if err != nil {
		t.Fatalf("ConversationBranch: %v", err)
	}
	if branch.ProviderConversationID != "thread-replaced" {
		t.Errorf("branch conversation = %q, want thread-replaced", branch.ProviderConversationID)
	}
	if branch.ReplacedTurnID != "" {
		t.Errorf("branch replaced turn = %q, want empty: a reload replaces no turn", branch.ReplacedTurnID)
	}
	if branch.ParentBranchID == "" {
		t.Errorf("branch parent = empty, want the source branch")
	}
}

func TestReloadStandingPromptUsesDriverRefresherForFilePreparedCopies(t *testing.T) {
	h := newReloadHarness(t, true)

	result, err := h.svc.ReloadChatPrompt(context.Background(), testSession, "new standing prompt")
	if err != nil {
		t.Fatalf("ReloadChatPrompt: %v", err)
	}
	if result.ProviderConversationID != "transcript-replaced" {
		t.Errorf("result conversation = %q, want transcript-replaced", result.ProviderConversationID)
	}
	conversation, prompt, env, copies := h.refresher.observed()
	if copies != 1 {
		t.Fatalf("driver copies = %d, want 1", copies)
	}
	if conversation != "thread-1" {
		t.Errorf("driver source conversation = %q, want thread-1", conversation)
	}
	if prompt != "new standing prompt" {
		t.Errorf("driver prompt = %q, want the replacement prompt", prompt)
	}
	if env["CODEX_HOME"] != "/opt/codex" {
		t.Errorf("driver env CODEX_HOME = %q, want the launch environment", env["CODEX_HOME"])
	}
	if len(h.state.resumes) != 1 || h.state.resumes[0].ProviderConversationID != "transcript-replaced" {
		t.Fatalf("resume calls = %#v, want one resume of the copied conversation", h.state.resumes)
	}
}

func TestReloadStandingPromptRefusesUnsupportedProvider(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.ReloadChatPrompt(context.Background(), testSession, "new prompt")
	if !errors.Is(err, chatsvc.ErrPromptReloadUnsupported) {
		t.Fatalf("err = %v, want ErrPromptReloadUnsupported", err)
	}
	if got := h.ctrl.ProviderConversationID(); got == "" {
		t.Errorf("source controller lost its conversation after refusal")
	}
}

func TestReloadStandingPromptRefusesBusyControllerAndDoesNotCopy(t *testing.T) {
	h := newReloadHarness(t, false)
	if _, err := h.svc.Send(context.Background(), testSession, ports.ChatUserMessage{
		Text: "running", Origin: domain.MessageOriginHuman,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	_, err := h.svc.ReloadChatPrompt(context.Background(), testSession, "new prompt")
	if !errors.Is(err, chatsvc.ErrTurnRunning) {
		t.Fatalf("busy reload err = %v, want ErrTurnRunning", err)
	}
	if got := h.source.lastPrompt(); got != "" {
		t.Errorf("busy reload reached the provider with prompt %q", got)
	}
}

func TestReloadStandingPromptReportsCopyFailureAndKeepsSource(t *testing.T) {
	h := newReloadHarness(t, false)
	h.source.mu.Lock()
	h.source.refreshErr = errors.New("rollout not found")
	h.source.mu.Unlock()

	_, err := h.svc.ReloadChatPrompt(context.Background(), testSession, "new prompt")
	if err == nil {
		t.Fatalf("reload with failing copy unexpectedly succeeded")
	}
	if len(h.state.resumes) != 0 {
		t.Errorf("resumes after copy failure = %d, want 0", len(h.state.resumes))
	}
	conversation, convErr := h.st.ConversationForSession(context.Background(), testSession)
	if convErr != nil {
		t.Fatalf("ConversationForSession: %v", convErr)
	}
	if conversation.ActiveBranchID == "" {
		t.Errorf("active branch cleared by a failed reload")
	}
}

func TestReloadStandingPromptRestoresSourceWhenReplacementResumeFails(t *testing.T) {
	h := newReloadHarness(t, false)
	h.state.mu.Lock()
	h.state.failResumeFor = "thread-replaced"
	h.state.mu.Unlock()

	if _, err := h.svc.ReloadChatPrompt(context.Background(), testSession, "new prompt"); err == nil {
		t.Fatalf("reload with failing resume unexpectedly succeeded")
	}
	// The restore path resumes the original conversation and reactivates its
	// branch; the source id must be live again, not the failed replacement.
	var lastResume ports.ChatResumeConfig
	for _, resume := range h.state.resumes {
		lastResume = resume
	}
	if lastResume.ProviderConversationID != "thread-1" {
		t.Errorf("final resume = %q, want the restored source thread-1", lastResume.ProviderConversationID)
	}
	controller, err := h.svc.Controller(testSession)
	if err != nil {
		t.Fatalf("Controller after failed reload: %v", err)
	}
	if got := controller.ProviderConversationID(); got != "thread-1" {
		t.Errorf("live controller conversation = %q, want restored thread-1", got)
	}
}
