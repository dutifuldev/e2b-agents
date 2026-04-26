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
}
