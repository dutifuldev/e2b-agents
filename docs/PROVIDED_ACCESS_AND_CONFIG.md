# Provided Access And Configuration

This document records the access, configuration, and external resources already provided for implementing and deploying `e2b-agents`.

Do not add secret values to this file.

## Local Environment

The repo contains a local `.env` file. It is gitignored and should remain uncommitted.

Known configured values:

- `E2B_API_KEY`
- `SLACK_CLIENT_ID`
- `SLACK_CLIENT_SECRET`
- `SLACK_SIGNING_SECRET`
- `SLACK_BOT_TOKEN`
- `SLACK_APP_TOKEN`
- `SLACK_REDIRECT_URL`
- `SLACK_EVENTS_URL`
- `SLACK_INTERACTIONS_URL`
- `SLACK_COMMANDS_URL`
- `SLACK_DEFAULT_TEAM_ID`
- `SLACK_DEFAULT_TEMPLATE_ID`
- `ANTHROPIC_API_KEY`

The implementation should load these through typed config, not by reading `.env` directly in production.

## E2B

An E2B API key has been provided in `.env`.

The app should use the hosted E2B API through this key. The MVP should treat:

- E2B sandbox ID as the agent ID
- E2B template ID as the agent image ID

No separate agent or agent image database tables should be introduced for the first implementation.

## Agent Runtime Harness

The initial runtime harness should be OpenClaw.

The E2B template used by `e2b-agents` should package and start an OpenClaw-based
agent runtime. `e2b-agents` should treat OpenClaw as the sandbox runtime behind
the ACP bridge, not as a separate product identity layer.

Local OpenClaw material exists outside this repo and can be used as a reference
for runtime shape, Docker packaging, ACP behavior, and configuration style. Do
not copy personal workspace content, identity files, private prompts, or other
personal material from that reference into this repo.

The control plane should remain responsible for:

- Slack request verification and identity resolution
- resolving the Slack workspace's current E2B sandbox ID
- creating an E2B sandbox from the configured OpenClaw template when needed
- connecting Slack messages to the sandbox runtime through ACP
- tracking the workspace's current sandbox pointer and setup status

OpenClaw should remain responsible for the agent runtime inside the sandbox.

## LLM Model

The initial OpenClaw runtime should use Anthropic Claude Sonnet 4.6.

Use the model identifier:

```text
anthropic/claude-sonnet-4-6
```

The Anthropic API key is already present in `.env` as:

```text
ANTHROPIC_API_KEY
```

Do not commit the key. The deploy/runtime environment should pass it into the
OpenClaw template or runtime process as a secret environment variable.

## Slack

Slack app credentials and URLs have been provided in `.env`.

The app should support:

- Slack OAuth redirect handling
- Slack event subscriptions
- Slack interactivity callbacks
- Slack slash commands
- Slack signature verification
- Slack browser testing through the already logged-in desktop Slack session

Slack workspace/channel/user concepts are gateway state. They should not become the center of the core E2B runtime model.

## Browser Testing

The machine has a logged-in Chromium Slack session.

For direct Slack UI testing, the working path from this machine is documented in:

- `/home/bob/tmp_slack_browser_automation.md`

Important details:

- The graphical session may need to be unlocked with `loginctl unlock-session`.
- The active X11 display is `:0`.
- Xauthority is `/run/user/1000/gdm/Xauthority`.
- `xdotool` can control the existing Chromium Slack window when `DISPLAY` and `XAUTHORITY` are set.

`agent-browser` exists on the machine, but in a prior test it hung even on simple pages. Use the direct existing-browser path if that remains true.

## GCP

GCloud is configured for the `dutiful` project:

- Project ID: `dutiful-20260414`
- Active account: `bob-gcloud@dutiful-20260414.iam.gserviceaccount.com`

The relevant VM is:

- Name: `ghreplica`
- Zone: `europe-west1-b`
- External IP: `34.77.214.194`

SSH access to the VM has been verified with `gcloud compute ssh`.

## Domain

The production domain is:

- `e2b-agents.dutiful.dev`

It already resolves to the VM external IP:

- `34.77.214.194`

The VM currently runs Caddy in Docker and already serves other services through the same host.

The Caddy config should be extended to route:

```text
e2b-agents.dutiful.dev -> e2b-agents service
```

## Database

Cloud SQL access is available from the VM through the Cloud SQL proxy.

The Cloud SQL instance is:

- Project: `horse-460221`
- Region: `europe-west1`
- Instance: `horse-pg`
- Connection name: `horse-460221:europe-west1:horse-pg`

The VM already runs Cloud SQL proxy containers.

The existing database to use is:

- `e2b`

Verified databases through the proxy include:

- `e2b`
- `ghreplica`
- `prtags`

The app should wire its DB config to the existing `e2b` database. Follow the existing IAM database connection pattern used by `ghreplica`/`prtags` on the VM.

## Existing VM Services

The VM currently runs Docker containers for:

- Caddy
- `ghreplica`
- `prtags`
- Cloud SQL proxy

Deployment should avoid disrupting those services.

## Implementation Readiness

With the provided inputs, implementation can proceed.

The initial implementation should include:

- Go module scaffold
- typed config loader
- GORM database package
- explicit `TableName()` methods
- SQL migrations
- Echo HTTP server
- health/readiness endpoints
- Slack OAuth/event/interaction/command endpoints
- Slack signature verification
- E2B client wrapper
- minimal sandbox create/list/connect command flow
- Docker deployment files
- Caddy route for `e2b-agents.dutiful.dev`
