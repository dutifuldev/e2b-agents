package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/dutifuldev/e2b-agents/internal/database"
	"gorm.io/gorm"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func testSlackClient() *SlackClient {
	return &SlackClient{
		token: "test-token",
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		})},
	}
}

type fakeRuntime struct {
	mu           sync.Mutex
	ensureCalls  []EnsureRuntimeInput
	sendCalls    []SendRuntimeInput
	ensureOutput EnsureRuntimeOutput
	ensureErr    error
	sendOutputs  []SendRuntimeOutput
	sendErrs     []error
}

func (r *fakeRuntime) Ensure(_ context.Context, input EnsureRuntimeInput) (EnsureRuntimeOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureCalls = append(r.ensureCalls, input)
	return r.ensureOutput, r.ensureErr
}

func (r *fakeRuntime) Send(_ context.Context, input SendRuntimeInput) (SendRuntimeOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendCalls = append(r.sendCalls, input)
	callIndex := len(r.sendCalls) - 1
	if callIndex < len(r.sendErrs) && r.sendErrs[callIndex] != nil {
		return SendRuntimeOutput{}, r.sendErrs[callIndex]
	}
	if callIndex < len(r.sendOutputs) {
		return r.sendOutputs[callIndex], nil
	}
	return SendRuntimeOutput{Text: "ok", SessionKey: input.SessionKey}, nil
}

func (r *fakeRuntime) ensureCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ensureCalls)
}

func (r *fakeRuntime) sendCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sendCalls)
}

func newTestGatewayService(t *testing.T, runtime Runtime) (*Service, *gorm.DB) {
	t.Helper()
	db, err := database.Open(":memory:", database.PoolConfig{MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.ApplyTestSchema(db); err != nil {
		t.Fatalf("apply test schema: %v", err)
	}
	return NewService(db, Options{
		Runtime:         runtime,
		Slack:           testSlackClient(),
		AutoCreate:      true,
		DefaultTeamID:   "default",
		DefaultTemplate: "openclaw",
	}), db
}

func TestHandleSlackEnvelopeDirectSendsReadyWorkspace(t *testing.T) {
	runtime := &fakeRuntime{}
	service, db := newTestGatewayService(t, runtime)
	workspaces := NewWorkspaceService(db)
	workspace, err := workspaces.EnsureWorkspace(context.Background(), EnsureWorkspaceInput{
		SlackTeamID: "T123",
		TeamID:      "default",
		TemplateID:  "openclaw",
	})
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	if err := workspaces.UpdateAfterMessage(context.Background(), workspace.ID, map[string]any{
		"current_sandbox_id": "sandbox-ready",
		"setup_status":       SetupStatusReady,
		"bot_token_ref":      "",
	}); err != nil {
		t.Fatalf("seed ready workspace: %v", err)
	}

	envelope := SlackEventEnvelope{
		TeamID:  "T123",
		EventID: "EvDirect",
		Event: SlackEvent{
			Type:        "app_mention",
			ChannelType: "channel",
			Channel:     "C123",
			User:        "U123",
			Text:        "<@BOT> hello",
			TS:          "1777220000.000100",
		},
	}
	if err := service.handleSlackEnvelope(context.Background(), envelope); err != nil {
		t.Fatalf("handleSlackEnvelope() returned error: %v", err)
	}
	if len(runtime.ensureCalls) != 0 {
		t.Fatalf("Ensure calls = %d, want 0", len(runtime.ensureCalls))
	}
	if len(runtime.sendCalls) != 1 {
		t.Fatalf("Send calls = %d, want 1", len(runtime.sendCalls))
	}
	send := runtime.sendCalls[0]
	if send.SandboxID != "sandbox-ready" {
		t.Fatalf("Send sandbox = %q, want sandbox-ready", send.SandboxID)
	}
	if send.SessionKey != "slack:v1:T123:C123:channel" {
		t.Fatalf("Send session = %q, want channel session", send.SessionKey)
	}

	updated, err := workspaces.GetBySlackTeamID(context.Background(), "T123")
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	if updated.CurrentSandboxID != "sandbox-ready" {
		t.Fatalf("current sandbox = %q, want sandbox-ready", updated.CurrentSandboxID)
	}
	if updated.CurrentACPSessionID != "slack:v1:T123:C123:channel" {
		t.Fatalf("current session = %q, want channel session", updated.CurrentACPSessionID)
	}
}

func TestHandleSlackEnvelopeEnsuresAndRetriesUnavailableRuntime(t *testing.T) {
	runtime := &fakeRuntime{
		ensureOutput: EnsureRuntimeOutput{
			SandboxID:  "sandbox-recovered",
			TemplateID: "openclaw",
			Host:       "localhost",
			BaseURL:    "http://localhost",
			SessionKey: "slack:v1:T123:C123:channel",
		},
		sendErrs: []error{errors.New("runtime helper send failed: connection refused")},
	}
	service, db := newTestGatewayService(t, runtime)
	workspaces := NewWorkspaceService(db)
	workspace, err := workspaces.EnsureWorkspace(context.Background(), EnsureWorkspaceInput{
		SlackTeamID: "T123",
		TeamID:      "default",
		TemplateID:  "openclaw",
	})
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	if err := workspaces.UpdateAfterMessage(context.Background(), workspace.ID, map[string]any{
		"current_sandbox_id": "sandbox-stale",
		"setup_status":       SetupStatusReady,
		"bot_token_ref":      "",
	}); err != nil {
		t.Fatalf("seed ready workspace: %v", err)
	}

	envelope := SlackEventEnvelope{
		TeamID:  "T123",
		EventID: "EvRecover",
		Event: SlackEvent{
			Type:        "app_mention",
			ChannelType: "channel",
			Channel:     "C123",
			User:        "U123",
			Text:        "<@BOT> hello",
			TS:          "1777220000.000100",
		},
	}
	if err := service.handleSlackEnvelope(context.Background(), envelope); err != nil {
		t.Fatalf("handleSlackEnvelope() returned error: %v", err)
	}
	if len(runtime.sendCalls) != 2 {
		t.Fatalf("Send calls = %d, want 2", len(runtime.sendCalls))
	}
	if runtime.sendCalls[0].SandboxID != "sandbox-stale" {
		t.Fatalf("first Send sandbox = %q, want sandbox-stale", runtime.sendCalls[0].SandboxID)
	}
	if runtime.sendCalls[1].SandboxID != "sandbox-recovered" {
		t.Fatalf("second Send sandbox = %q, want sandbox-recovered", runtime.sendCalls[1].SandboxID)
	}
	if len(runtime.ensureCalls) != 1 {
		t.Fatalf("Ensure calls = %d, want 1", len(runtime.ensureCalls))
	}
	if runtime.ensureCalls[0].SandboxID != "sandbox-stale" {
		t.Fatalf("Ensure sandbox = %q, want sandbox-stale", runtime.ensureCalls[0].SandboxID)
	}

	updated, err := workspaces.GetBySlackTeamID(context.Background(), "T123")
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	if updated.CurrentSandboxID != "sandbox-recovered" {
		t.Fatalf("current sandbox = %q, want sandbox-recovered", updated.CurrentSandboxID)
	}
	if updated.CurrentACPSessionID != "slack:v1:T123:C123:channel" {
		t.Fatalf("current session = %q, want channel session", updated.CurrentACPSessionID)
	}
}

func TestHandleSlackEnvelopeDoesNotRecoverNonAvailabilityError(t *testing.T) {
	runtime := &fakeRuntime{
		sendErrs: []error{errors.New("runtime helper send failed: runtime HTTP 401: unauthorized")},
	}
	service, db := newTestGatewayService(t, runtime)
	workspaces := NewWorkspaceService(db)
	workspace, err := workspaces.EnsureWorkspace(context.Background(), EnsureWorkspaceInput{
		SlackTeamID: "T123",
		TeamID:      "default",
		TemplateID:  "openclaw",
	})
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	if err := workspaces.UpdateAfterMessage(context.Background(), workspace.ID, map[string]any{
		"current_sandbox_id": "sandbox-ready",
		"setup_status":       SetupStatusReady,
		"bot_token_ref":      "",
	}); err != nil {
		t.Fatalf("seed ready workspace: %v", err)
	}

	envelope := SlackEventEnvelope{
		TeamID:  "T123",
		EventID: "EvNoRecover",
		Event: SlackEvent{
			Type:        "app_mention",
			ChannelType: "channel",
			Channel:     "C123",
			User:        "U123",
			Text:        "<@BOT> hello",
			TS:          "1777220000.000100",
		},
	}
	if err := service.handleSlackEnvelope(context.Background(), envelope); err == nil {
		t.Fatal("handleSlackEnvelope() returned nil error, want failure")
	}
	if len(runtime.ensureCalls) != 0 {
		t.Fatalf("Ensure calls = %d, want 0", len(runtime.ensureCalls))
	}
	if len(runtime.sendCalls) != 1 {
		t.Fatalf("Send calls = %d, want 1", len(runtime.sendCalls))
	}

	updated, err := workspaces.GetBySlackTeamID(context.Background(), "T123")
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	if updated.SetupStatus != SetupStatusFailed {
		t.Fatalf("setup status = %q, want failed", updated.SetupStatus)
	}
}

func TestHandleSlackEnvelopeDedupesConcurrentRetry(t *testing.T) {
	runtime := &fakeRuntime{
		ensureOutput: EnsureRuntimeOutput{
			SandboxID:  "sandbox-1",
			TemplateID: "openclaw",
			Host:       "localhost",
			BaseURL:    "http://localhost",
			SessionKey: "session-1",
		},
		sendOutputs: []SendRuntimeOutput{{Text: "ok", SessionKey: "session-1"}},
	}
	service, _ := newTestGatewayService(t, runtime)

	envelope := SlackEventEnvelope{
		TeamID:  "T123",
		EventID: "Ev123",
		Event: SlackEvent{
			Type:        "app_mention",
			ChannelType: "channel",
			Channel:     "",
			User:        "U123",
			Text:        "<@BOT> hello",
			TS:          "1777220000.000100",
		},
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			if err := service.handleSlackEnvelope(context.Background(), envelope); err != nil {
				t.Errorf("handleSlackEnvelope() returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := runtime.sendCallCount(); got != 1 {
		t.Fatalf("runtime send count = %d, want one", got)
	}
}

func TestHandleSlackEnvelopeRemembersOlderProcessedEvents(t *testing.T) {
	runtime := &fakeRuntime{
		ensureOutput: EnsureRuntimeOutput{
			SandboxID:  "sandbox-1",
			TemplateID: "openclaw",
			Host:       "localhost",
			BaseURL:    "http://localhost",
			SessionKey: "session-1",
		},
		sendOutputs: []SendRuntimeOutput{
			{Text: "ok", SessionKey: "session-1"},
			{Text: "ok", SessionKey: "session-1"},
		},
	}
	service, _ := newTestGatewayService(t, runtime)

	first := SlackEventEnvelope{
		TeamID:  "T123",
		EventID: "EvFirst",
		Event: SlackEvent{
			Type:        "app_mention",
			ChannelType: "channel",
			Channel:     "",
			User:        "U123",
			Text:        "<@BOT> first",
			TS:          "1777220000.000100",
		},
	}
	second := first
	second.EventID = "EvSecond"
	second.Event.Text = "<@BOT> second"
	second.Event.TS = "1777220001.000100"

	if err := service.handleSlackEnvelope(context.Background(), first); err != nil {
		t.Fatalf("handle first event: %v", err)
	}
	if err := service.handleSlackEnvelope(context.Background(), second); err != nil {
		t.Fatalf("handle second event: %v", err)
	}
	if err := service.handleSlackEnvelope(context.Background(), first); err != nil {
		t.Fatalf("handle retried first event: %v", err)
	}

	if got := runtime.sendCallCount(); got != 2 {
		t.Fatalf("runtime send count = %d, want two", got)
	}
}

func TestHandleSlackEnvelopeRespondsToDirectMention(t *testing.T) {
	db, err := database.Open(":memory:", database.PoolConfig{MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.ApplyTestSchema(db); err != nil {
		t.Fatalf("apply test schema: %v", err)
	}
	workspaces := NewWorkspaceService(db)
	if _, err := workspaces.EnsureWorkspace(context.Background(), EnsureWorkspaceInput{
		SlackTeamID: "T123",
		TeamID:      "default",
		TemplateID:  "openclaw",
		BotUserID:   "BOT",
	}); err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}

	runtime := &fakeRuntime{
		ensureOutput: EnsureRuntimeOutput{
			SandboxID:  "sandbox-1",
			TemplateID: "openclaw",
			Host:       "localhost",
			BaseURL:    "http://localhost",
			SessionKey: "session-1",
		},
		sendOutputs: []SendRuntimeOutput{{Text: "ok", SessionKey: "session-1"}},
	}
	service := NewService(db, Options{
		Runtime:         runtime,
		AutoCreate:      true,
		DefaultTeamID:   "default",
		DefaultTemplate: "openclaw",
	})

	envelope := SlackEventEnvelope{
		TeamID:  "T123",
		EventID: "EvDM123",
		Event: SlackEvent{
			Type:        "message",
			ChannelType: "im",
			Channel:     "",
			User:        "U123",
			Text:        "<@BOT> hello",
			TS:          "1777220000.000100",
		},
	}
	if err := service.handleSlackEnvelope(context.Background(), envelope); err != nil {
		t.Fatalf("handleSlackEnvelope() returned error: %v", err)
	}

	if got := runtime.sendCallCount(); got != 1 {
		t.Fatalf("runtime send count = %d, want one", got)
	}
}
