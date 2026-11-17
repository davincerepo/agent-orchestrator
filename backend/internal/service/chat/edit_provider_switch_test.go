package chat_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

func TestEditedMessageSwitchesProviderWithSelectedModel(t *testing.T) {
	for _, editFirst := range []bool{true, false} {
		name := "middle prompt"
		if editFirst {
			name = "first prompt"
		}
		t.Run(name, func(t *testing.T) {
			h, _, driver := newEditHarness(t, true)
			ctx := context.Background()
			if err := h.svc.SwitchProvider(ctx, testSession, domain.ProviderSwitchRequest{
				TargetHarness: domain.HarnessClaudeCode, Model: "glm-5.3", Effort: "max",
			}); err != nil {
				t.Fatal(err)
			}
			h.conv = driver.fresh
			first := completeTurn(t, h, "KEEP_FIRST_CONTEXT", "provider-turn-101")
			h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool { return len(s.Messages) == 2 })
			middle := completeTurn(t, h, "REPLACED_PROMPT", "provider-turn-102")
			h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool { return len(s.Messages) == 4 })
			completeTurn(t, h, "EXCLUDED_LATER_PROMPT", "provider-turn-103")
			h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
				return len(s.Messages) == 6 && turnStateByText(t, s)["EXCLUDED_LATER_PROMPT"] == domain.TurnStateCompleted
			})
			source, _ := h.st.ConversationForSession(ctx, testSession)
			sourceBranch, _ := h.st.ConversationBranch(ctx, source.ID, source.ActiveBranchID)
			turnID := middle
			if editFirst {
				turnID = first
			}
			message := ports.ChatUserMessage{
				Text: "REPLACEMENT", ClientMessageID: "edited-target", Origin: domain.MessageOriginHuman,
				ProviderSwitch: &domain.ProviderSwitchRequest{
					TargetHarness: domain.HarnessCodex, Model: "gpt-6.1-sol", Effort: "xhigh",
				},
			}
			result, err := h.svc.EditMessage(ctx, testSession, turnID, message)
			if err != nil {
				t.Fatalf("EditMessage: %v", err)
			}
			started := lastStart(driver)
			if started.Model != "gpt-6.1-sol" || started.Effort != "xhigh" {
				t.Fatalf("target start = %+v", started)
			}
			if strings.Contains(started.SystemPrompt, "REPLACED_PROMPT") || strings.Contains(started.SystemPrompt, "EXCLUDED_LATER_PROMPT") {
				t.Fatalf("target received superseded context: %s", started.SystemPrompt)
			}
			if strings.Contains(started.SystemPrompt, "KEEP_FIRST_CONTEXT") == editFirst {
				t.Fatalf("wrong context before edited prompt: %s", started.SystemPrompt)
			}
			if got := driver.fresh.sentTexts(); len(got) != 1 || got[0] != message.Text {
				t.Fatalf("target prompts = %v", got)
			}
			settings := driver.fresh.sentMessages()[0].Settings
			if settings.Model != "gpt-6.1-sol" || settings.Effort != "xhigh" {
				t.Fatalf("provider request settings = %+v", settings)
			}
			record, _, _ := h.st.GetSession(ctx, testSession)
			if record.Harness != domain.HarnessCodex || record.Metadata.Model != "gpt-6.1-sol" || record.Metadata.Effort != "xhigh" {
				t.Fatalf("switched session = %+v", record)
			}
			branch, err := h.st.ConversationBranch(ctx, source.ID, result.ActiveBranchID)
			if err != nil || branch.ReplacedTurnID != "" || branch.ParentBranchID != sourceBranch.ID || branch.ReplacementTurnID != result.Turn.ID || branch.ProviderBindingID == sourceBranch.ProviderBindingID {
				t.Fatalf("edited target branch = %+v, %v", branch, err)
			}
			snapshot, err := h.st.LoadConversationSnapshot(ctx, source.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range snapshot.Messages {
				if strings.Contains(item.Text, "EXCLUDED_LATER_PROMPT") {
					t.Fatal("later prompt remains on the edited path")
				}
			}
			if len(snapshot.BranchPoints) != 0 {
				t.Fatalf("retired-provider versions must not point at an empty target boundary: %+v", snapshot.BranchPoints)
			}
			repeated, err := h.svc.EditMessage(ctx, testSession, turnID, message)
			if err != nil || repeated.Turn.ID != result.Turn.ID || driver.fresh.sendCallCount() != 1 {
				t.Fatalf("retry = %+v, %v; sends = %d", repeated, err, driver.fresh.sendCallCount())
			}
			changed := *message.ProviderSwitch
			changed.Effort = "low"
			message.ProviderSwitch = &changed
			if _, err := h.svc.EditMessage(ctx, testSession, turnID, message); !errors.Is(err, chatsvc.ErrEditIdempotencyConflict) {
				t.Fatalf("changed retry = %v", err)
			}
			driver.fresh.emit(ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: result.Turn.ProviderTurnID, TurnState: domain.TurnStateCompleted})
			h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
				return turnStateByText(t, s)["REPLACEMENT"] == domain.TurnStateCompleted
			})
			message.ClientMessageID = "second-edit"
			message.Text = "SECOND_REPLACEMENT"
			message.ProviderSwitch.Effort = "xhigh"
			second, err := h.svc.EditMessage(ctx, testSession, result.Turn.ID, message)
			if err != nil {
				t.Fatalf("edit again on the selected provider: %v", err)
			}
			updated, _ := h.st.LoadConversationSnapshot(ctx, source.ID)
			if len(updated.BranchPoints) != 2 || second.Turn.ID == result.Turn.ID {
				t.Fatalf("same-provider versions were not retained: %+v", updated.BranchPoints)
			}
		})
	}
}

func TestEditedProviderCommitFailureRestoresSource(t *testing.T) {
	h, _, _ := newEditHarnessWithStore(t, true, func(st *store.Store) chatsvc.Store {
		return failingProviderSwitchStore{Store: st, err: errors.New("commit unavailable")}
	})
	ctx := context.Background()
	first := completeTurn(t, h, "original", "provider-turn-1")
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return turnStateByText(t, s)["original"] == domain.TurnStateCompleted
	})
	original, _ := h.st.ConversationForSession(ctx, testSession)
	_, err := h.svc.EditMessage(ctx, testSession, first, ports.ChatUserMessage{
		Text: "replacement", ClientMessageID: "failed-commit",
		ProviderSwitch: &domain.ProviderSwitchRequest{TargetHarness: domain.HarnessClaudeCode},
	})
	if !errors.Is(err, chatsvc.ErrEditDeliveryRejected) {
		t.Fatalf("publication failure = %v", err)
	}
	current, _ := h.st.ConversationForSession(ctx, testSession)
	if current.ActiveBranchID != original.ActiveBranchID {
		t.Fatal("failed switch committed a partial branch")
	}
	if _, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "source still works"}); err != nil {
		t.Fatal(err)
	}
}

func TestEditedProviderRecoversLostAcceptanceWithoutSendingAgain(t *testing.T) {
	h, _, driver := newEditHarnessWithStore(t, true, func(st *store.Store) chatsvc.Store {
		return &failOnceEditCompletionStore{Store: st}
	})
	ctx := context.Background()
	first := completeTurn(t, h, "original", "provider-turn-1")
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return turnStateByText(t, s)["original"] == domain.TurnStateCompleted
	})
	message := ports.ChatUserMessage{
		Text: "replacement", ClientMessageID: "recover-switched-edit",
		ProviderSwitch: &domain.ProviderSwitchRequest{TargetHarness: domain.HarnessClaudeCode, Model: "target-model", Effort: "high"},
	}
	result, err := h.svc.EditMessage(ctx, testSession, first, message)
	if !errors.Is(err, chatsvc.ErrEditDeliveryUncertain) || result.Turn.ID == "" {
		t.Fatalf("unsettled receipt = %+v, %v", result, err)
	}
	if _, err := h.svc.EditMessage(ctx, testSession, first, message); !errors.Is(err, chatsvc.ErrEditDeliveryUncertain) || driver.fresh.sendCallCount() != 1 {
		t.Fatalf("retry before completion = %v", err)
	}
	driver.fresh.emit(ports.ChatEvent{Kind: ports.ChatEventTurnCompleted, ProviderTurnID: result.Turn.ProviderTurnID, TurnState: domain.TurnStateCompleted})
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return turnStateByText(t, s)["replacement"] == domain.TurnStateCompleted
	})
	recovered, err := h.svc.EditMessage(ctx, testSession, first, message)
	if err != nil || recovered.SourceBranchID != result.SourceBranchID || recovered.ActiveBranchID != result.ActiveBranchID || recovered.Turn.ID != result.Turn.ID {
		t.Fatalf("recovered edit = %+v, %v; original = %+v", recovered, err, result)
	}
	if driver.fresh.sendCallCount() != 1 {
		t.Fatal("receipt recovery sent the prompt again")
	}
}

type failOnceEditCompletionStore struct {
	chatsvc.Store
	failed bool
}

func (s *failOnceEditCompletionStore) CompleteEditDelivery(ctx context.Context, conversationID, clientMessageID, sourceBranchID, activeBranchID string, turn domain.ConversationTurn, now time.Time) error {
	if !s.failed {
		s.failed = true
		return errors.New("injected edit completion failure")
	}
	return s.Store.CompleteEditDelivery(ctx, conversationID, clientMessageID, sourceBranchID, activeBranchID, turn, now)
}

func TestEditedProviderStartFailureRestoresSource(t *testing.T) {
	h, _, driver := newEditHarness(t, true)
	ctx := context.Background()
	first := completeTurn(t, h, "original prompt", "provider-turn-1")
	h.awaitSnapshot(t, func(s store.ConversationSnapshot) bool {
		return len(s.Messages) == 2 && turnStateByText(t, s)["original prompt"] == domain.TurnStateCompleted
	})
	original, _ := h.st.ConversationForSession(ctx, testSession)
	driver.startErr = errors.New("target unavailable")
	message := ports.ChatUserMessage{
		Text: "replacement", ClientMessageID: "failed-switch",
		ProviderSwitch: &domain.ProviderSwitchRequest{TargetHarness: domain.HarnessClaudeCode, Model: "target-model", Effort: "high"},
	}
	if _, err := h.svc.EditMessage(ctx, testSession, first, message); !errors.Is(err, chatsvc.ErrEditDeliveryRejected) {
		t.Fatalf("failed edit = %v", err)
	}
	record, _, _ := h.st.GetSession(ctx, testSession)
	current, _ := h.st.ConversationForSession(ctx, testSession)
	if record.Harness != domain.HarnessCodex || current.ActiveBranchID != original.ActiveBranchID {
		t.Fatalf("source was not restored: harness=%s branch=%s", record.Harness, current.ActiveBranchID)
	}
	if _, err := h.svc.Send(ctx, testSession, ports.ChatUserMessage{Text: "source still works"}); err != nil {
		t.Fatalf("send after failed edit: %v", err)
	}
}
