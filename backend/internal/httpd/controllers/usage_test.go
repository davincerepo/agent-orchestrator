package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
)

type fakeUsageSummaryService struct {
	projectID domain.ProjectID
	items     []domain.CompactSessionUsage
	hourly    []domain.SessionHourlyUsage
	err       error
}

func (f *fakeUsageSummaryService) ListCompact(_ context.Context, projectID domain.ProjectID) ([]domain.CompactSessionUsage, error) {
	f.projectID = projectID
	return f.items, f.err
}

func (f *fakeUsageSummaryService) ListProjectSessionHourly(_ context.Context, projectID domain.ProjectID) ([]domain.SessionHourlyUsage, error) {
	f.projectID = projectID
	return f.hourly, f.err
}

func newUsageTestServer(t *testing.T, svc *fakeUsageSummaryService) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newUsageTestServerWithLogger(t, svc, log)
}

func newUsageTestServerWithLogger(t *testing.T, svc *fakeUsageSummaryService, log *slog.Logger) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{UsageSummary: svc}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestUsageAPIListsCompactProjectUsage(t *testing.T) {
	inputCost := int64(300000000)
	processed := int64(12300)
	unavailableProcessed := int64(3)
	svc := &fakeUsageSummaryService{items: []domain.CompactSessionUsage{
		{
			SessionID: "reverb-12", ProcessedTokens: &processed, Incomplete: true,
			EstimatedCost: &domain.EstimatedCost{
				TotalNanos: 420000000, InputNanos: &inputCost,
				Coverage:            domain.EstimatedCostCoveragePartial,
				ProviderAttribution: domain.EstimatedCostProviderAttributionInferred,
			},
		},
		{SessionID: "unavailable", ProcessedTokens: &unavailableProcessed},
	}}
	srv := newUsageTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/usage/sessions?projectId=reverb", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	if svc.projectID != "reverb" {
		t.Fatalf("project id = %q, want reverb", svc.projectID)
	}
	var got struct {
		Sessions []struct {
			SessionID       string          `json:"sessionId"`
			ProcessedTokens int64           `json:"processedTokens"`
			TotalTokens     int64           `json:"totalTokens"`
			Incomplete      bool            `json:"incomplete"`
			EstimatedCost   json.RawMessage `json:"estimatedCost"`
		} `json:"sessions"`
	}
	mustJSON(t, body, &got)
	if len(got.Sessions) != 2 || got.Sessions[0].SessionID != "reverb-12" ||
		got.Sessions[0].ProcessedTokens != 12300 || got.Sessions[0].TotalTokens != 12300 ||
		!got.Sessions[0].Incomplete {
		t.Fatalf("response = %+v", got)
	}
	var cost struct {
		TotalNanos          int64  `json:"totalNanos"`
		InputNanos          *int64 `json:"inputNanos"`
		CachedInputNanos    *int64 `json:"cachedInputNanos"`
		Coverage            string `json:"coverage"`
		ProviderAttribution string `json:"providerAttribution"`
	}
	mustJSON(t, got.Sessions[0].EstimatedCost, &cost)
	if cost.TotalNanos != 420000000 || cost.InputNanos == nil || *cost.InputNanos != 300000000 ||
		cost.CachedInputNanos != nil || cost.Coverage != "partial" || cost.ProviderAttribution != "inferred" {
		t.Fatalf("estimated cost = %+v", cost)
	}
	if string(got.Sessions[1].EstimatedCost) != "null" {
		t.Fatalf("unavailable estimatedCost = %s, want explicit null", got.Sessions[1].EstimatedCost)
	}
}

func TestUsageAPILogsServiceErrors(t *testing.T) {
	var logs bytes.Buffer
	srv := newUsageTestServerWithLogger(t, &fakeUsageSummaryService{err: errors.New("usage unavailable")}, slog.New(slog.NewTextHandler(&logs, nil)))

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/usage/sessions?projectId=reverb", "")
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", status, body)
	}
	if !strings.Contains(logs.String(), "failed to list compact session usage") {
		t.Fatalf("logs = %q, want the list failure", logs.String())
	}
}

func TestUsageAPIListsProjectSessionHourlyUsage(t *testing.T) {
	input := int64(300)
	cached := int64(280)
	output := int64(20)
	zero := int64(0)
	svc := &fakeUsageSummaryService{hourly: []domain.SessionHourlyUsage{{
		SessionID: "reverb-12", Num: 12, Kind: domain.KindWorker, IsTerminated: true,
		Incomplete: true,
		Models: []domain.SessionModelHourlyUsage{{
			Harness: domain.HarnessClaudeCode, ModelID: "glm-5.3",
			Buckets: []domain.SessionHourlyUsageBucket{
				{HourUTC: "2026-11-08T09", Tokens: domain.UsageTokenMetrics{
					InputTokens: &input, CachedInputTokens: &cached, UncachedInputTokens: &zero, OutputTokens: &output,
				}},
				{HourUTC: "2026-11-08T10"},
			},
		}},
	}}}
	srv := newUsageTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/usage/projects/reverb/sessions", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	if svc.projectID != "reverb" {
		t.Fatalf("project id = %q, want reverb", svc.projectID)
	}
	var got struct {
		Sessions []struct {
			SessionID    string `json:"sessionId"`
			Num          int64  `json:"num"`
			Kind         string `json:"kind"`
			IsTerminated bool   `json:"isTerminated"`
			Incomplete   bool   `json:"incomplete"`
			Models       []struct {
				Harness string `json:"harness"`
				ModelID string `json:"modelId"`
				Buckets []struct {
					Hour         string `json:"hour"`
					InputTokens  *int64 `json:"inputTokens"`
					OutputTokens *int64 `json:"outputTokens"`
				} `json:"buckets"`
			} `json:"models"`
		} `json:"sessions"`
	}
	mustJSON(t, body, &got)
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions = %+v, want one", got)
	}
	row := got.Sessions[0]
	if row.SessionID != "reverb-12" || row.Num != 12 || row.Kind != "worker" ||
		!row.IsTerminated || !row.Incomplete || len(row.Models) != 1 {
		t.Fatalf("session row = %+v", row)
	}
	model := row.Models[0]
	if model.Harness != "claude-code" || model.ModelID != "glm-5.3" || len(model.Buckets) != 2 {
		t.Fatalf("model = %+v", model)
	}
	if model.Buckets[0].Hour != "2026-11-08T09" || model.Buckets[0].InputTokens == nil ||
		*model.Buckets[0].InputTokens != 300 || *model.Buckets[0].OutputTokens != 20 {
		t.Fatalf("first bucket = %+v", model.Buckets[0])
	}
	if model.Buckets[1].InputTokens != nil {
		t.Fatalf("unknown bucket = %+v, want null counters", model.Buckets[1])
	}
}
