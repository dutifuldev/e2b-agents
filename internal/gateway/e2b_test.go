package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRuntimeClientSendUsesCachedACPAdapter(t *testing.T) {
	var seenAuth string
	var seenSession string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prompt" {
			t.Fatalf("path = %q, want /prompt", r.URL.Path)
		}
		seenAuth = r.Header.Get("Authorization")
		var payload SendRuntimeInput
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		seenSession = payload.SessionKey
		_, _ = w.Write([]byte(`{"text":"hello\n\nworld","sessionKey":"slack:v1:T:C:channel","acpSessionId":"sess-1"}`))
	}))
	defer server.Close()

	client := NewRuntimeClient(RuntimeOptions{
		APIKey:         "e2b-key",
		AnthropicKey:   "anthropic-key",
		GatewayToken:   "secret-token",
		RequestTimeout: time.Second,
	})
	client.rememberEndpoint(EnsureRuntimeOutput{
		SandboxID:  "sandbox-1",
		ACPBaseURL: server.URL,
	})

	out, err := client.Send(context.Background(), SendRuntimeInput{
		SandboxID:  "sandbox-1",
		SessionKey: "slack:v1:T:C:channel",
		Prompt:     "hi",
	})
	if err != nil {
		t.Fatalf("Send() returned error: %v", err)
	}
	if out.Text != "hello\n\nworld" {
		t.Fatalf("text = %q, want normalized reply", out.Text)
	}
	if out.ACPSessionID != "sess-1" {
		t.Fatalf("acp session = %q, want sess-1", out.ACPSessionID)
	}
	if seenAuth != "Bearer secret-token" {
		t.Fatalf("authorization = %q, want bearer token", seenAuth)
	}
	if seenSession != "slack:v1:T:C:channel" {
		t.Fatalf("session = %q, want Slack session key", seenSession)
	}
}

func TestRuntimeClientSendWithoutCachedEndpointIsUnavailable(t *testing.T) {
	client := NewRuntimeClient(RuntimeOptions{
		APIKey:       "e2b-key",
		AnthropicKey: "anthropic-key",
	})
	_, err := client.Send(context.Background(), SendRuntimeInput{
		SandboxID:  "sandbox-missing-cache",
		SessionKey: "slack:v1:T:C:channel",
		Prompt:     "hi",
	})
	if err == nil {
		t.Fatal("Send() returned nil error, want cache miss")
	}
	if !isRuntimeUnavailableError(err) {
		t.Fatalf("cache miss error was not classified unavailable: %v", err)
	}
}
