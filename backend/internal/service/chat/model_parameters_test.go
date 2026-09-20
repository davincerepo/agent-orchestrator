package chat_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
)

type fleetModelConversation struct{ *fakeConversation }

func (c *fleetModelConversation) ListModels(context.Context) ([]ports.ChatModel, error) {
	return []ports.ChatModel{
		{ID: "fast-model", Default: true, Efforts: []string{"low", "high", "ultra"}, ServiceTiers: []ports.ModelServiceTier{{ID: "priority", Name: "Fast"}}},
		{ID: "plain-model", Efforts: []string{"low"}},
	}, nil
}

func TestFleetInitialTurnAndResumeParameters(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	conv := &fleetModelConversation{newFakeConversation()}
	var started ports.ChatStartConfig
	var resumed ports.ChatResumeConfig
	var sequence atomic.Int32
	svc := chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st,
		Drivers: fakeRegistry{driver: fakeDriver{conv: conv, startCfg: &started, resumeCfg: &resumed}},
		Log:     slog.New(slog.DiscardHandler), NewID: func() string { return fmt.Sprintf("params-%d", sequence.Add(1)) },
	})
	t.Cleanup(func() { _ = svc.Stop(ctx, testSession) })
	cfg := chatsvc.StartConfig{SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(), Model: "fast-model", Effort: "ultra", ServiceTier: "priority"}
	controller, err := svc.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if started.Effort != "ultra" || started.ServiceTier != "priority" {
		t.Fatalf("initial launch = %+v", started)
	}
	if settings := controller.Settings(); settings.ReasoningEffort != "ultra" || settings.ServiceTier != "priority" {
		t.Fatalf("initial durable settings = %+v", settings)
	}
	if _, err := svc.SetTurnSettings(ctx, testSession, domain.ConversationSettings{Model: "plain-model", ServiceTier: "priority"}); err == nil {
		t.Fatal("accepted Fast for unsupported model")
	}
	if _, err := svc.SetTurnSettings(ctx, testSession, domain.ConversationSettings{Model: "fast-model", ReasoningEffort: "invented"}); err == nil {
		t.Fatal("accepted unadvertised effort")
	}
	settings := domain.ConversationSettings{Model: "fast-model", ReasoningEffort: "low", ServiceTier: "default"}
	if err := svc.ArmChatHandoff(ctx, testSession, domain.SessionInterfaceTransitionDrain); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTurnSettings(ctx, testSession, settings); !errors.Is(err, chatsvc.ErrControllerHandoff) {
		t.Fatalf("settings changed during handoff: %v", err)
	}
	svc.AbortChatHandoff(testSession)
	if _, err := svc.SetTurnSettings(ctx, testSession, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Send(ctx, ports.ChatUserMessage{Text: "test settings"}); err != nil {
		t.Fatal(err)
	}
	sent := conv.sentMessages()
	if len(sent) != 1 || sent[0].Settings.ServiceTier != "default" || sent[0].Settings.Effort != "low" {
		t.Fatalf("turn settings = %+v", sent)
	}
	providerID := conv.ProviderConversationID()
	if err := svc.Stop(ctx, testSession); err != nil {
		t.Fatal(err)
	}
	// A replacement service simulates a daemon restart. Its launch defaults must
	// lose to the user's last durable settings, including explicit Fast off.
	resumedConv := newFakeConversation()
	resumedConv.providerConversationID = providerID
	svc = chatsvc.New(chatsvc.Options{Store: st, Sessions: st, Drivers: fakeRegistry{driver: fakeDriver{conv: resumedConv, resumeCfg: &resumed}}, Log: slog.New(slog.DiscardHandler), NewID: func() string { return fmt.Sprintf("params-%d", sequence.Add(1)) }})
	cfg.ProviderConversationID = providerID
	if _, err := svc.Start(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if resumed.Effort != "low" || resumed.ServiceTier != "default" {
		t.Fatalf("resume replaced durable choices: %+v", resumed)
	}
}
