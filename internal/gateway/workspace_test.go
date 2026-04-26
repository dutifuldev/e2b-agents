package gateway

import (
	"path/filepath"
	"testing"

	"github.com/dutifuldev/e2b-agents/internal/database"
)

func TestEnsureWorkspaceClearsSandboxOnTemplateChange(t *testing.T) {
	db, err := database.Open("sqlite://"+filepath.Join(t.TempDir(), "test.db"), database.PoolConfig{})
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	if err := database.ApplyTestSchema(db); err != nil {
		t.Fatalf("ApplyTestSchema() returned error: %v", err)
	}

	service := NewWorkspaceService(db)
	workspace, err := service.EnsureWorkspace(t.Context(), EnsureWorkspaceInput{
		SlackTeamID: "T123",
		TeamID:      "default",
		TemplateID:  "openclaw-a",
	})
	if err != nil {
		t.Fatalf("EnsureWorkspace() returned error: %v", err)
	}
	if err := service.UpdateAfterMessage(t.Context(), workspace.ID, map[string]any{
		"current_sandbox_id":     "sandbox-old",
		"current_acp_session_id": "session-old",
		"last_error":             "old error",
	}); err != nil {
		t.Fatalf("UpdateAfterMessage() returned error: %v", err)
	}

	workspace, err = service.EnsureWorkspace(t.Context(), EnsureWorkspaceInput{
		SlackTeamID: "T123",
		TeamID:      "default",
		TemplateID:  "openclaw-b",
	})
	if err != nil {
		t.Fatalf("EnsureWorkspace() returned error: %v", err)
	}
	if workspace.CurrentSandboxID != "" {
		t.Fatalf("CurrentSandboxID = %q, want empty", workspace.CurrentSandboxID)
	}
	if workspace.CurrentACPSessionID != "" {
		t.Fatalf("CurrentACPSessionID = %q, want empty", workspace.CurrentACPSessionID)
	}
	if workspace.LastError != "" {
		t.Fatalf("LastError = %q, want empty", workspace.LastError)
	}
}
