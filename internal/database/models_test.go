package database

import (
	"path/filepath"
	"testing"
)

func TestTableNames(t *testing.T) {
	cases := map[string]string{
		"slack workspace":       SlackWorkspace{}.TableName(),
		"slack processed event": SlackProcessedEvent{}.TableName(),
	}
	for name, table := range cases {
		if table == "" {
			t.Fatalf("%s table name is empty", name)
		}
	}
	if (SlackWorkspace{}).TableName() != TableSlackWorkspaces {
		t.Fatalf("SlackWorkspace table = %q, want %q", (SlackWorkspace{}).TableName(), TableSlackWorkspaces)
	}
	if (SlackProcessedEvent{}).TableName() != TableSlackProcessedEvents {
		t.Fatalf("SlackProcessedEvent table = %q, want %q", (SlackProcessedEvent{}).TableName(), TableSlackProcessedEvents)
	}
}

func TestApplyMigrationsSQLite(t *testing.T) {
	db, err := Open("sqlite://"+filepath.Join(t.TempDir(), "test.db"), PoolConfig{})
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	if err := ApplyMigrations(t.Context(), db); err != nil {
		t.Fatalf("ApplyMigrations() returned error: %v", err)
	}

	var count int64
	if err := db.Table(TableSlackWorkspaces).Count(&count).Error; err != nil {
		t.Fatalf("query %s returned error: %v", TableSlackWorkspaces, err)
	}
	if err := db.Table(TableSlackProcessedEvents).Count(&count).Error; err != nil {
		t.Fatalf("query %s returned error: %v", TableSlackProcessedEvents, err)
	}
}
