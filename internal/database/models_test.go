package database

import "testing"

func TestTableNames(t *testing.T) {
	cases := map[string]string{
		"slack workspace":  SlackWorkspace{}.TableName(),
		"schema migration": SchemaMigration{}.TableName(),
	}
	for name, table := range cases {
		if table == "" {
			t.Fatalf("%s table name is empty", name)
		}
	}
	if (SlackWorkspace{}).TableName() != TableSlackWorkspaces {
		t.Fatalf("SlackWorkspace table = %q, want %q", (SlackWorkspace{}).TableName(), TableSlackWorkspaces)
	}
}
