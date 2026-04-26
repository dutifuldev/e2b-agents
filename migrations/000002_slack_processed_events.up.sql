CREATE TABLE IF NOT EXISTS slack_processed_events (
    event_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    slack_team_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_slack_processed_events_workspace_id
    ON slack_processed_events (workspace_id);

CREATE INDEX IF NOT EXISTS idx_slack_processed_events_slack_team_id
    ON slack_processed_events (slack_team_id);

CREATE INDEX IF NOT EXISTS idx_slack_processed_events_created_at
    ON slack_processed_events (created_at);
