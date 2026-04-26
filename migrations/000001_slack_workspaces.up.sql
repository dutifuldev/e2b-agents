CREATE TABLE IF NOT EXISTS slack_workspaces (
    id TEXT PRIMARY KEY,
    team_id TEXT NOT NULL,
    slack_team_id TEXT NOT NULL UNIQUE,
    slack_enterprise_id TEXT NOT NULL DEFAULT '',
    slack_team_name TEXT NOT NULL DEFAULT '',
    bot_token_ref TEXT NOT NULL DEFAULT '',
    signing_secret_ref TEXT NOT NULL DEFAULT '',
    bot_user_id TEXT NOT NULL DEFAULT '',
    template_id TEXT NOT NULL,
    current_sandbox_id TEXT NOT NULL DEFAULT '',
    current_acp_session_id TEXT NOT NULL DEFAULT '',
    last_slack_event_id TEXT NOT NULL DEFAULT '',
    last_slack_channel_id TEXT NOT NULL DEFAULT '',
    last_slack_message_ts TEXT NOT NULL DEFAULT '',
    setup_status TEXT NOT NULL DEFAULT 'ready',
    last_activity_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    installed_by_user_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_slack_workspaces_team_id
    ON slack_workspaces (team_id);

CREATE INDEX IF NOT EXISTS idx_slack_workspaces_current_sandbox_id
    ON slack_workspaces (current_sandbox_id)
    WHERE current_sandbox_id <> '';

CREATE INDEX IF NOT EXISTS idx_slack_workspaces_last_activity_at
    ON slack_workspaces (last_activity_at);
