package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("SLACK_DEFAULT_TEMPLATE_ID", "")
	t.Setenv("E2B_AGENTS_DEFAULT_TEMPLATE_ID", "")
	t.Setenv("SLACK_DEFAULT_TEAM_ID", "")
	t.Setenv("E2B_AGENTS_DEFAULT_TEAM_ID", "")

	cfg := Load()
	if cfg.WorkspaceDefaultTemplate != "openclaw" {
		t.Fatalf("WorkspaceDefaultTemplate = %q, want openclaw", cfg.WorkspaceDefaultTemplate)
	}
	if cfg.WorkspaceDefaultTeamID != "default" {
		t.Fatalf("WorkspaceDefaultTeamID = %q, want default", cfg.WorkspaceDefaultTeamID)
	}
	if cfg.RuntimeModel != DefaultModel {
		t.Fatalf("RuntimeModel = %q, want %q", cfg.RuntimeModel, DefaultModel)
	}
	if cfg.ACPAdapterPort != 18790 {
		t.Fatalf("ACPAdapterPort = %d, want 18790", cfg.ACPAdapterPort)
	}
}

func TestValidateServeRejectsDefaultGatewayToken(t *testing.T) {
	cfg := Config{
		DatabaseURL:          "sqlite://test.db",
		E2BAPIKey:            "e2b",
		AnthropicAPIKey:      "anthropic",
		SlackSigningSecret:   "signing",
		SlackBotToken:        "slack",
		E2BHelperScript:      "helper.js",
		OpenClawGatewayPort:  18789,
		ACPAdapterPort:       18790,
		OpenClawGatewayToken: DefaultOpenClawGatewayToken,
		SandboxTimeout:       1,
	}
	if err := cfg.ValidateServe(); err == nil {
		t.Fatal("expected default gateway token to be rejected")
	}
}

func TestValidateServeRejectsACPAdapterGatewayPortCollision(t *testing.T) {
	cfg := Config{
		DatabaseURL:          "sqlite://test.db",
		E2BAPIKey:            "e2b",
		AnthropicAPIKey:      "anthropic",
		SlackSigningSecret:   "signing",
		SlackBotToken:        "slack",
		E2BHelperScript:      "helper.js",
		OpenClawGatewayPort:  18789,
		ACPAdapterPort:       18789,
		OpenClawGatewayToken: "secret-token",
		SandboxTimeout:       1,
	}
	if err := cfg.ValidateServe(); err == nil {
		t.Fatal("expected ACP adapter and gateway port collision to be rejected")
	}
}
