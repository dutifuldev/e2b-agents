# e2b-agents

> `e2b-agents` is an internal control plane for Slack-accessible agent runtimes running in E2B sandboxes.

`e2b-agents` connects Slack workspaces to long-lived E2B sandbox runtimes. The service receives Slack events, resolves the workspace, creates or reuses the workspace's current sandbox, sends the message to the runtime through an ACP adapter, and posts the final assistant reply back to Slack.

The current production runtime packages OpenClaw in an E2B template and uses Anthropic Claude Sonnet 4.6 through the model identifier `anthropic/claude-sonnet-4-6`. The gateway treats the sandbox as the agent instance and the E2B template as the agent image.

- **Slack gateway**: verifies Slack signatures, handles install callbacks and event delivery, and posts replies.
- **E2B runtime lifecycle**: creates, reconnects, and refreshes sandboxes from configured templates.
- **ACP adapter path**: runs a sandbox-local adapter that exposes the harness ACP session over HTTP to the Go service.
- **Warm direct sends**: sends follow-up messages directly to the cached adapter endpoint when the workspace is ready.
- **Session routing**: maps Slack channels, threads, and direct messages to separate ACP sessions.
- **Workspace state**: stores the current sandbox, ACP session, template, Slack token reference, and setup status in Postgres.
- **Duplicate delivery guard**: records processed Slack event IDs so repeated Slack deliveries do not create duplicate turns.
- **Operational logs**: emits structured `slog` JSON with timing for runtime, Slack post, and database update stages.
- **Development CLI**: includes commands for workspace setup, direct message testing, migrations, and Slack auth checks.

## Production flow

```text
Slack event
  -> Echo HTTP service
  -> Slack signature verification
  -> workspace lookup or creation
  -> Slack surface to ACP session key
  -> warm direct ACP send when possible
  -> E2B ensure/recovery when needed
  -> Slack chat.postMessage reply
  -> workspace state update
```

Warm messages should normally avoid sandbox setup:

```text
Slack message -> DB lookup -> direct ACP adapter request -> Slack reply
```

If the sandbox is expired, missing, or unreachable, the service recovers by ensuring a sandbox from the workspace template, starting the runtime gateway and ACP adapter, retrying the send once, and then updating the workspace row.

## Agent model

`e2b-agents` uses E2B objects directly:

| Product concept | E2B object |
| --- | --- |
| Agent instance | Sandbox |
| Agent image | Template |
| Agent instance ID | Sandbox ID |
| Agent image ID | Template ID or alias |

The MVP is team-owned through Slack workspace mappings. Each Slack workspace stores one current sandbox pointer and one default template ID or alias.

Slack surfaces map to ACP sessions:

| Slack surface | ACP session key shape |
| --- | --- |
| Channel | `slack-v1-<team_id>-<channel_id>-channel` |
| Thread | `slack-v1-<team_id>-<channel_id>-<thread_ts>` |
| Direct message | `slack-v1-<team_id>-<channel_id>-direct` |

Channel messages receive channel replies. Thread messages receive thread replies.

## Install

Install Go and Node dependencies:

```bash
go mod download
npm install
```

Build the TypeScript runtime helper:

```bash
npm run build
```

Build the Go binary:

```bash
go build ./cmd/e2b-agents
```

## Configuration

Local development reads environment variables from the shell. The repo may have a gitignored `.env` file for local secrets:

```bash
set -a
source .env
set +a
```

Required for `serve`:

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | Postgres connection string. |
| `E2B_API_KEY` | E2B API key used to create and connect sandboxes. |
| `ANTHROPIC_API_KEY` | Runtime model provider key passed into the sandbox. |
| `SLACK_SIGNING_SECRET` | Slack request signature verification. |
| `SLACK_BOT_TOKEN` | Default Slack bot token for posting replies. |
| `OPENCLAW_GATEWAY_TOKEN` | Non-default token used between the service and runtime gateway. |

Common optional variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ADDR` | `:8080` | HTTP listen address. |
| `SLACK_CLIENT_ID` | empty | Slack OAuth install flow. |
| `SLACK_CLIENT_SECRET` | empty | Slack OAuth install flow. |
| `SLACK_REDIRECT_URL` | empty | Slack OAuth callback URL. |
| `SLACK_DEFAULT_TEAM_ID` | `default` | Owning team for auto-created Slack workspaces. |
| `SLACK_DEFAULT_TEMPLATE_ID` | `openclaw` | E2B template ID or alias for new workspaces. |
| `E2B_AGENTS_DEFAULT_TEAM_ID` | `default` | Fallback default team when Slack default is unset. |
| `E2B_AGENTS_DEFAULT_TEMPLATE_ID` | `openclaw` | Fallback template when Slack default is unset. |
| `E2B_AGENTS_WORKSPACE_AUTO_CREATE` | `true` | Whether Slack events can create workspace mappings. |
| `E2B_AGENTS_RUNTIME_MODEL` | `anthropic/claude-sonnet-4-6` | Runtime model identifier. |
| `E2B_HELPER_NODE` | `node` | Node executable for the E2B helper. |
| `E2B_HELPER_SCRIPT` | `runtime/e2b-helper/dist/helper.js` | Built helper script path. |
| `OPENCLAW_GATEWAY_PORT` | `18789` | Runtime gateway port inside the sandbox. |
| `E2B_AGENTS_ACP_ADAPTER_PORT` | `18790` | ACP adapter port inside the sandbox. |
| `E2B_SANDBOX_TIMEOUT` | `1h` | E2B sandbox timeout. |
| `E2B_SANDBOX_REQUEST_TIMEOUT` | `5m` | E2B helper and runtime request timeout. |
| `SLACK_PROCESSING_TIMEOUT` | `10m` | End-to-end Slack event processing timeout. |

`OPENCLAW_GATEWAY_TOKEN` must be set to a real non-default secret in production.

## Database

Run migrations before serving:

```bash
go run ./cmd/e2b-agents migrate up
```

Migrations are loaded from `migrations/` by default. Override the directory with:

```bash
export E2B_AGENTS_MIGRATIONS_DIR=/path/to/migrations
```

The database package uses GORM with explicit table names and SQL migrations. Test schema setup uses `AutoMigrate`, but production schema changes should go through SQL migration files.

## Run

Start the HTTP service:

```bash
go run ./cmd/e2b-agents serve
```

Health endpoints:

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
```

Slack routes:

| Route | Method | Purpose |
| --- | --- | --- |
| `/slack/install` | `GET` | Start Slack OAuth install. |
| `/slack/oauth/callback` | `GET` | Store installed Slack workspace mapping. |
| `/slack/events` | `POST` | Receive Slack Events API deliveries. |
| `/slack/interactions` | `POST` | Signature-verified placeholder. |
| `/slack/commands` | `POST` | Signature-verified placeholder. |

## Development commands

Create or update a Slack workspace mapping:

```bash
go run ./cmd/e2b-agents dev ensure-workspace \
  --slack-team-id T123 \
  --team-id default \
  --template-id openclaw \
  --bot-user-id U123
```

Send a message through the gateway without a Slack event:

```bash
go run ./cmd/e2b-agents dev send \
  --slack-team-id T123 \
  --channel-id C123 \
  --user-id U123 \
  --text 'Reply with a short readiness check'
```

Post the direct-send reply back to Slack:

```bash
go run ./cmd/e2b-agents dev send \
  --slack-team-id T123 \
  --channel-id C123 \
  --user-id U123 \
  --text 'Reply in Slack' \
  --post-to-slack
```

Check Slack bot auth metadata:

```bash
go run ./cmd/e2b-agents dev slack-auth
```

## Testing

Run Go tests:

```bash
go test ./...
```

Run Go vet:

```bash
go vet ./...
```

Build and type-check the TypeScript helper:

```bash
npm run build
npm run check
```

The core test coverage includes config validation, migrations, workspace mapping behavior, Slack signature handling, Slack surface session keys, response normalization, and runtime send/ensure behavior.

## Deployment

The production Docker image builds the TypeScript helper first, builds the Go binary, and runs `e2b-agents serve`.

Build locally:

```bash
docker build -t e2b-agents .
```

Run the GCP compose service from the deployment host:

```bash
docker compose -f deploy/gcp/docker-compose.yml up -d --build e2b-agents
```

Run migrations with the compose ops profile:

```bash
docker compose -f deploy/gcp/docker-compose.yml --profile ops run --rm e2b-agents-migrate
```

The current production domain is:

```text
https://e2b-agents.dutiful.dev
```

Production should expose:

```bash
curl -fsS https://e2b-agents.dutiful.dev/healthz
curl -fsS https://e2b-agents.dutiful.dev/readyz
```

## Runtime internals

The Go service calls `runtime/e2b-helper/dist/helper.js` for sandbox ensure work. The helper:

1. Connects to the existing sandbox or creates one from the workspace template.
2. Writes runtime identity and model configuration files.
3. Starts the OpenClaw gateway inside the sandbox.
4. Writes and starts the ACP adapter script.
5. Waits for gateway and adapter readiness.
6. Returns the public gateway and ACP adapter URLs to the Go service.

Warm sends do not run this ensure path. They use the cached adapter URL and call:

```text
POST /prompt
Authorization: Bearer <OPENCLAW_GATEWAY_TOKEN>
```

The adapter owns ACP initialization, session creation or loading, prompt serialization per session, response collection, and session persistence inside the sandbox.

## Operational notes

- Slack Events API deliveries are acknowledged quickly by `/slack/events`; processing continues in a goroutine.
- Only availability-style runtime failures trigger sandbox recovery.
- Invalid prompts, model/provider errors, malformed runtime responses, and Slack post failures are reported without recreating the sandbox.
- The workspace lock prevents concurrent messages for the same workspace from racing to recreate the runtime.
- Structured logs include `runtime_duration_ms`, `slack_post_duration_ms`, `database_update_duration_ms`, and `total_duration_ms` on successful Slack event handling.
- Direct-send success is logged as `runtime direct send succeeded`.
- Recovery is logged as `runtime direct send unavailable; ensuring runtime`, followed by `runtime ensure succeeded` and `runtime send after ensure succeeded`.

## Reference docs

- [Initial product and system spec](docs/INITIAL_SPEC.md)
- [ACP runtime architecture](docs/ACP_RUNTIME_ARCHITECTURE.md)
- [Warm runtime latency](docs/WARM_RUNTIME_LATENCY.md)
- [E2B terminology](docs/E2B_TERMINOLOGY.md)
- [E2B CLI guide](docs/E2B_CLI_GUIDE.md)
- [Provided access and configuration](docs/PROVIDED_ACCESS_AND_CONFIG.md)
