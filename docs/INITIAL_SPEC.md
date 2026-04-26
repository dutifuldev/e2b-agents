# e2b-agents Product And System Spec

Status: initial planning spec

## Purpose

e2b-agents is a control plane for disposable, owned agent runtimes.

It gives humans and automation systems a consistent way to create, open, chat
with, inspect, and retire isolated agent instances. Each instance runs in an
E2B sandbox. The control plane owns identity, authorization, routing, lifecycle
policy, metadata, and user experience.

E2B owns isolation, process execution, the sandbox filesystem, networking,
sandbox timeouts, pause/resume behavior, snapshots, volumes, and sandbox
shutdown.

## Core Product Model

e2b-agents manages many short-lived agent instances. An instance is an E2B
sandbox.

e2b-agents stores an instance record for each sandbox so it can track ownership,
request source, auth policy, setup progress, routing, and audit history. The
record does not represent a second runtime. It is metadata that e2b-agents
stores for the E2B sandbox.

An instance can be owned by a team or by a user. The MVP starts with team-owned
instances because the first channel gateway resolves messages into a team. User
ownership should remain part of the product model and schema so the control
plane can later support personal agents and direct user workflows without a
schema rewrite.

The requester is the actor that caused the instance to be created or reused.
For Slack, the requester is a Slack user or Slack automation event after it is
resolved to an e2b-agents user and team membership. The requester is audit
context, not the ownership boundary.

## Identity And Ownership Model

e2b-agents should follow the standalone E2B ownership pattern:

- users are human identities
- teams are the first account and ownership boundary
- team memberships connect users to teams
- service credentials resolve to a team
- user credentials resolve to a user, then the user selects or defaults to a
  team

The first product surface is Slack, so Slack identity should map into the same
model:

- one Slack workspace maps to one e2b-agents team
- one Slack user maps to one e2b-agents user
- a Slack user/team mapping creates or reuses team membership
- every MVP instance is team-owned
- the Slack app may request an instance, but it does not own the instance
- each team has one configured agent for the MVP
- Slack workspace configuration points to that agent
- the agent points to one active E2B sandbox owned by that team
- the server uses E2B credentials to create sandboxes; users never receive E2B
  credentials

Initial create flow:

1. Verify the Slack request signature.
2. Resolve the Slack workspace to a team.
3. Resolve the Slack user to a user.
4. Create the user, team membership, and Slack identity link if policy allows.
5. Resolve the agent configured for that Slack workspace.
6. Create or reuse the agent's active instance.
7. Create the E2B sandbox using the server-side E2B API key when no active
   instance exists.
8. Store the E2B sandbox ID on the e2b-agents instance record.
9. Store the requester as the Slack actor and the owner as the team.
10. Route Slack messages from that workspace to the configured agent.

This mirrors E2B's team-first authorization while keeping Slack-specific logic
outside the sandbox runtime.

## E2B Standalone Identity References

This model is based on E2B standalone infra behavior:

- `e2b-dev/infra/DEV-LOCAL.md` documents local seeding of one user, one team,
  and API tokens.
- `e2b-dev/infra/packages/db/scripts/seed/postgres/seed-db.go` explicitly
  creates `auth.users`, `public.users`, `teams`, `users_teams`,
  `access_tokens`, and `team_api_keys`.
- `e2b-dev/infra/packages/db/migrations/20231124185944_create_schemas_and_tables.sql`
  defines the original teams, memberships, access tokens, and team API keys.
- `e2b-dev/infra/packages/db/migrations/20251217000000_create_public_users_table.sql`
  adds `public.users` as the app-facing user table.
- `e2b-dev/infra/packages/db/migrations/20260416120000_remove_user_team_provision_triggers.sql`
  states that the application now owns user projection and default team
  bootstrap. e2b-agents should use explicit application code too, not database
  triggers.

## Main Capabilities

e2b-agents should provide:

- preset-based agent instance creation
- custom sandbox image or template creation where policy allows it
- owner-aware access control, with team-bound access in the MVP
- service-principal create flows for external systems
- external identity resolution for chat or workflow platforms
- idempotent create requests for bots and automation
- canonical instance URLs
- an API for instance and Slack workspace management
- ACP bridging from Slack workspaces to sandbox runtimes
- later terminal access through the control plane
- optional SSH access through short-lived credentials
- optional port forwarding through the control plane
- instance web app proxying
- app-level readiness discovery when a preset needs it
- metadata discovery for agent name, version, and capabilities
- sandbox timeout and idle-timeout lifecycle enforcement
- activity tracking
- optional shared state mounts
- Slack gateway support as the first user surface
- later channel gateway support for Teams-like surfaces
- later CLI support for local and automation workflows
- later web UI for creating, listing, opening, and chatting with agents
- audit-friendly metadata on who requested what and why

## High-Level Architecture

```text
Slack user / Slack event / trusted automation
              |
              v
        Slack gateway
 install, signatures, event normalization
              |
              v
        e2b-agents API
 auth, policy, routing, sandbox lifecycle
              |
              v
      e2b-agents worker
 timeout reconciliation and setup checks
              |
              v
          E2B
 create, connect, commands, files, port hosts, pause, kill
              |
              v
       Agent runtime
 ACP endpoint, optional web UI, optional shell
```

## Components

### API Service

The API service owns the public control surface.

Responsibilities:

- authenticate Slack requests, admins, and service principals
- authorize team, workspace, user, and instance access
- validate create requests
- resolve presets
- resolve Slack workspaces and Slack users
- enforce placement and quota policy
- create durable instance records
- create durable agent records
- expose Slack ingress endpoints and internal control endpoints
- bridge Slack workspace messages to runtime ACP sessions
- deliver ACP responses back to Slack
- expose internal endpoints for trusted gateways and workers

### Worker Service

The worker is the lifecycle reconciler.

Responsibilities:

- find instance records that need work
- create E2B sandboxes from presets
- connect to existing sandboxes
- write runtime context files
- clone or prepare requested repositories
- upload generated workspace files
- start runtime commands when the template does not auto-start them
- probe runtime health
- fetch runtime metadata
- update setup status when e2b-agents performs post-create work
- refresh E2B sandbox timeouts after activity
- find stale instances whose e2b-agents idle policy has elapsed
- kill or pause expired E2B sandboxes
- repair missing metadata where possible

### Web UI

The web UI is not part of the first version. Slack is the first human surface.

When added later, the web UI should support:

- instance list
- create flow
- preset picker
- repository input
- instruction input
- sandbox state and setup status badges
- open action for the runtime web UI
- chat surface for agent sessions
- terminal surface
- settings for channel integrations
- clear error states for provisioning, setup, auth, and expired instances

### CLI

The CLI is not part of the first version.

The CLI is a thin client over the API.

It should support:

- configure API profiles
- list instances
- suggest names
- create instances
- delete instances
- open instance URL
- send a chat prompt
- attach terminal
- open SSH
- open local port forward
- print machine-readable JSON for automation

The CLI should not own policy, URL construction, preset expansion, or team
resolution. Those belong in the API.

### Channel Gateway

The Slack gateway is the first product surface.

Responsibilities:

- handle Slack installation flows
- store Slack bot tokens as secret references
- verify Slack request signatures
- map Slack workspaces to e2b-agents teams
- map Slack users to e2b-agents users
- create team memberships when policy allows it
- resolve the configured agent
- create or reuse the agent's active instance
- bootstrap an ACP agent session
- send Slack messages as ACP prompts
- deliver assistant text back to Slack
- manage workspace-level routing and mention policies

The Slack gateway should not create sandboxes directly. It should call the
e2b-agents API, which applies policy and creates E2B sandboxes.

Later channel gateways should follow the same shape: provider workspace maps to
team, provider user maps to user, provider configuration chooses an agent, and
the instance remains owned by the team.

## Runtime Contract

Every chat-capable runtime should expose ACP inside the sandbox.

Default contract:

```text
ACP port: 2529
ACP transport: WebSocket
ACP path: /
health endpoint: GET /healthz
metadata endpoint: GET /.well-known/e2b-agents-acp
```

The metadata endpoint should return:

```json
{
  "protocolVersion": 1,
  "agentInfo": {
    "name": "runtime-name",
    "title": "Runtime Name",
    "version": "1.0.0"
  },
  "agentCapabilities": {
    "loadSession": true,
    "promptCapabilities": {
      "image": false,
      "audio": false,
      "embeddedContext": true
    },
    "mcp": {
      "http": true,
      "sse": false
    }
  },
  "authMethods": []
}
```

External clients should never connect directly to ACP. The path is always:

```text
Slack gateway or browser -> e2b-agents API -> sandbox ACP endpoint
```

## E2B OpenClaw Gateway Integration

E2B has an existing documented way to launch the OpenClaw gateway inside an E2B
sandbox:

```text
https://e2b.dev/docs/agents/openclaw/openclaw-gateway
```

Treat that page as a reference for the currently documented E2B launch path.
e2b-agents will most likely not use that exact approach as its main product
architecture. The preferred direction is still to make OpenClaw a managed
runtime preset behind e2b-agents-managed instance lifecycle, proxying, auth, and
Slack workspace flows.

## Go E2B Integration Layout

e2b-agents should keep E2B-specific Go integration code under one package
boundary:

```text
internal/e2b/
  api/          generated E2B REST API client
  envd/         generated sandbox daemon clients and types
  sandbox.go    handwritten facade for sandbox lifecycle and operations
  commands.go   handwritten command helpers
  files.go      handwritten file helpers
  errors.go     handwritten error mapping
```

Application code should import `internal/e2b`, not the generated packages
directly. The generated `api` and `envd` packages should be replaceable outputs
from E2B specs.

This mirrors the relevant E2B naming pattern without using a vague top-level
`internal/api` package:

- E2B's Go API service uses `internal/api` for generated API code inside the
  API service module.
- E2B's Python SDK uses `api/client` for generated REST client code.
- E2B's Go daemon clients are generated under service-specific envd packages.

Because e2b-agents will also have its own API service, `internal/api` should be
reserved for e2b-agents itself. The E2B cloud API client should live at
`internal/e2b/api`, and the sandbox daemon client should live at
`internal/e2b/envd`.

## Runtime Context

e2b-agents should write a runtime context document into each sandbox before agent
traffic begins.

Recommended path:

```text
/etc/e2b-agents/runtime-context/context.json
```

Recommended environment variable:

```text
E2B_AGENTS_RUNTIME_CONTEXT_PATH=/etc/e2b-agents/runtime-context/context.json
```

Schema:

```json
{
  "schemaVersion": "e2b-agents.runtimeContext.v1",
  "instanceId": "inst_123",
  "ownerType": "team",
  "ownerId": "team_123",
  "teamId": "team_123",
  "requesterUserId": "user_123",
  "agentRef": {
    "type": "preset",
    "provider": "internal",
    "id": "openclaw"
  },
  "profile": {
    "name": "Research Agent",
    "imageUrl": "https://example.com/avatar.png"
  },
  "repo": {
    "url": "https://github.com/example/project",
    "branch": "main",
    "dir": "/workspace/repo"
  },
  "instructions": "Help with this repository."
}
```

Runtime-specific templates decide how to consume this file.

## Presets

A preset is the stable public abstraction for launching a runtime.

Presets should define:

- ID
- display name
- description
- E2B template ID or alias
- ACP port
- optional web port
- default sandbox timeout
- maximum sandbox timeout
- idle timeout
- required environment variables
- allowed input fields
- default repository behavior
- resource profile where supported
- visibility
- service-principal allowlist

Presets are the source of truth for runtime launch behavior. Product surfaces
should read launchable presets from the API and create instances with
`presetId`, not by copying hidden environment details into Slack messages,
browser clients, or other callers.

## Instance Lifecycle

e2b-agents should treat an instance as an E2B sandbox. E2B owns the runtime
state. e2b-agents should store that state directly and avoid inventing a second
runtime lifecycle unless product setup requires it.

Public E2B sandbox states:

```text
running
paused
```

e2b-agents setup status:

```text
none
pending
in_progress
succeeded
failed
```

Setup status rules:

- `none`: the preset is complete when E2B sandbox creation succeeds
- `pending`: e2b-agents has post-create work queued for the sandbox
- `in_progress`: e2b-agents is applying context, cloning a repo, starting an
  agent server, or waiting for an app-level endpoint such as ACP
- `succeeded`: e2b-agents-specific setup finished
- `failed`: e2b-agents-specific setup failed, even if the sandbox still exists

Most presets should aim for `none`: the E2B template should boot directly into
the usable runtime. Use setup status only for work e2b-agents performs after E2B
returns a sandbox.

E2B timeout terminology:

- `timeoutMs` is the JavaScript/TypeScript SDK timeout value in milliseconds
- REST create uses `timeout` in seconds
- `onTimeout: "kill"` makes timeout terminal
- `onTimeout: "pause"` preserves filesystem and memory state
- `autoResume: true` lets supported activity resume a paused sandbox

If e2b-agents uses E2B auto-pause, the instance should remain logically available
while paused. The next user action can reconnect or auto-resume the sandbox.
If e2b-agents uses E2B kill-on-timeout, the sandbox is no longer connectable
after the timeout, and e2b-agents should mark its instance record accordingly.

e2b-agents should store both:

- product metadata fields such as `setupStatus`, `lastActivityAt`, and
  `expiresAt`
- E2B fields such as `e2bSandboxId`, `e2bSandboxState`, `e2bTemplateId`,
  `e2bEnvdVersion`, and `e2bTrafficAccessToken`

`e2bSandboxState` should use E2B's public values: `running` or `paused`. If E2B
no longer has the sandbox, e2b-agents should represent that as missing runtime
state on its own record, not as another E2B state.

Activity should update `lastActivityAt`.

Idle expiration should be computed from `lastActivityAt`.

Maximum expiration should be computed from `createdAt`.

The effective expiration is the earlier of idle expiration and maximum
expiration.

## Agent Flow

Slack message to agent flow:

1. Authenticate and verify the Slack request.
2. Resolve the Slack workspace to an e2b-agents team.
3. Resolve the Slack user to an e2b-agents user and team membership.
4. Resolve the agent configured for that Slack workspace.
5. Load the agent.
6. If the agent already has an active instance, reuse it.
7. If not, create a new team-owned instance.
8. Create the E2B sandbox using the server-side E2B API key.
9. Store requester metadata for the Slack actor that caused creation.
10. Queue setup work if the preset requires post-create work.
11. Attach the instance to the agent.
12. Return the agent, instance, and ACP connection metadata.

Service principals should not automatically receive post-create access to the
instance. Service principals can request agent operations only when scoped to
the owning team.

## Agents And Agent Sessions

For the first version, the product routing unit is the agent. Slack is only one
way to send messages to an agent. Later channel gateways should also resolve to
agents instead of introducing channel-specific agent concepts.

e2b-agents should store agent metadata:

- agent ID
- owner type
- owner ID
- team ID
- name
- description
- active instance ID
- active agent session ID
- preset ID
- status
- last activity timestamp
- last error

An agent session stores ACP metadata for the runtime session behind an agent:

- agent session ID
- agent ID
- team ID
- instance ID
- current working directory
- ACP session ID
- agent info
- capabilities
- last initialized timestamp
- last error

The durable transcript belongs to Slack and the runtime or ACP backend unless
product requirements explicitly add a separate transcript store. e2b-agents may
store Slack event IDs and delivery markers for idempotency, but it should not
store full chat transcripts in the MVP.

## ACP Bootstrap For Slack

Before sending a Slack message to the runtime, the API should be able to
bootstrap or repair the configured agent's session.

Flow:

1. Authorize the Slack workspace, Slack user, team, and team membership
   mapping.
2. Resolve the agent configured for that Slack workspace.
3. Load or create the agent's active instance.
4. Confirm the instance setup is complete and ACP-capable.
5. Open an ACP connection to the runtime from the server side.
6. Send ACP initialize.
7. If the agent has an existing ACP session ID, try to load it.
8. If the session is missing, create a new session and update the agent session
   record.
9. Persist effective ACP session ID, cwd, agent info, and capabilities.
10. Send the Slack message as the prompt.
11. Deliver ACP responses back to Slack.

## WebSocket ACP Bridge

WebSockets stay in the MVP. The first WebSocket surface is the server-side ACP
bridge used by the Slack gateway and API, not a browser chat UI.

Recommended flow:

1. Slack gateway receives and verifies a Slack event.
2. Slack gateway resolves the configured agent through the API.
3. Slack gateway requests a short-lived ACP connect ticket for that agent.
4. Slack gateway opens a WebSocket to e2b-agents.
5. e2b-agents validates the ticket before WebSocket upgrade.
6. e2b-agents connects to the sandbox runtime's ACP WebSocket.
7. e2b-agents relays messages between Slack handling code and ACP.
8. e2b-agents refreshes instance activity after successful message exchange.

Tickets should include:

- type
- agent ID
- instance ID
- team ID
- requester
- expiry
- nonce
- allowed protocol

MVP ticket type:

- agent ACP session

Later ticket types:

- browser ACP session
- terminal session
- port forward

## Deferred Surfaces

The following surfaces are out of scope for the first version and should not
appear in the MVP API:

- browser chat API
- public generic chat CRUD
- instance web proxy
- web terminal
- SSH access
- local port forwarding
- shared state mounts
- durable bindings

These can be reintroduced later when there is a browser UI, CLI, or multi-channel
product surface that needs them.

## API Surface

Public Slack ingress API:

```text
GET    /healthz
GET    /slack/install
GET    /slack/oauth/callback
POST   /slack/events
POST   /slack/interactions
POST   /slack/commands
```

Internal API used by the Slack gateway, worker, and admin tooling:

```text
GET    /internal/v1/presets
GET    /internal/v1/presets/:presetId
POST   /internal/v1/instances
GET    /internal/v1/instances/:id
DELETE /internal/v1/instances/:id
GET    /internal/v1/agents/:agentId
PUT    /internal/v1/agents/:agentId
POST   /internal/v1/agents/:agentId/messages
POST   /internal/v1/agents/:agentId/acp/connect-ticket
GET    /internal/v1/agents/:agentId/acp/connect
POST   /internal/v1/slack/workspaces/resolve
POST   /internal/v1/slack/users/resolve
```

`GET /internal/v1/agents/:agentId/acp/connect` upgrades to WebSocket after
ticket validation.

No public browser chat, terminal, SSH, port-forward, shared mount, or
binding API should be exposed in the first version.

## Data Model

e2b-agents has two possible identity deployment modes:

- E2B-infra mode: e2b-agents runs next to an existing E2B infra database. In
  this mode, it should reuse E2B identity tables and add only product-specific
  tables that reference them.
- hosted-E2B mode: e2b-agents talks to hosted E2B through an API key and cannot
  access E2B's internal database. In this mode, it should create local
  compatible identity tables for users, teams, memberships, and service
  credentials.

The project should not create duplicate identity tables inside the same
database. It should pick one identity source per deployment.

Identity tables in E2B-infra mode:

```text
users
teams
team_api_keys
access_tokens
users_teams
```

Local identity tables in hosted-E2B mode:

```text
users
teams
users_teams
team_api_keys
access_tokens
```

e2b-agents-owned product tables:

```text
slack_workspaces
slack_users
service_principals
presets
agents
instances
instance_events
agent_sessions
connect_tickets
audit_events
```

Initial Slack-specific tables:

```text
slack_workspaces
id
team_id
slack_team_id
slack_enterprise_id
slack_team_name
bot_token_ref
signing_secret_ref
bot_user_id
default_agent_id
installed_by_user_id
created_at
updated_at

slack_users
id
team_id
user_id
slack_team_id
slack_user_id
slack_user_name
slack_email
created_at
updated_at
```

`agents` should include:

```text
agents
id
owner_type
owner_id
team_id
name
description
preset_id
active_instance_id
active_agent_session_id
status
last_activity_at
last_error
created_at
updated_at
```

Team membership should use `users_teams`. In E2B-infra mode that is E2B's
existing table. In hosted-E2B mode that is the local compatible table.

`agents.status` should be a small explicit state machine:

```text
unconfigured
ready
creating_instance
waiting_ready
failed
disabled
```

Slack delivery state should not define the agent. It can be stored separately
or on `slack_workspaces` while Slack is the only gateway:

```text
slack_workspace_id
agent_id
last_slack_event_id
last_slack_channel_id
last_slack_message_ts
updated_at
```

`agent_sessions` should store ACP metadata only:

```text
id
agent_id
team_id
instance_id
acp_session_id
cwd
agent_info_json
capabilities_json
last_initialized_at
last_error
created_at
updated_at
```

`instances` should include:

```text
id
name
owner_type
owner_id
team_id
agent_id
requester_id
requester_type
preset_id
e2b_sandbox_id
e2b_sandbox_state
e2b_template_id
e2b_template_alias
e2b_envd_version
e2b_traffic_access_token
e2b_envd_access_token
setup_status
setup_status_message
source
request_id
idempotency_key
slack_workspace_id
repo_url
repo_branch
repo_dir
runtime_context_json
agent_profile_json
acp_status_json
created_at
setup_completed_at
last_activity_at
idle_expires_at
max_expires_at
expires_at
killed_at
setup_failed_at
metadata_json
```

`owner_type` should support `team` and `user`. In the MVP, `owner_type` should
always be `team` and `owner_id` should match `team_id`. Keeping both fields now
prevents the later personal-agent path from needing a disruptive instance schema
change.

## E2B Sandbox Adapter Interface

e2b-agents should isolate direct E2B behavior behind an adapter. Product code
should speak in e2b-agents instances. Adapter code should speak in E2B sandbox
IDs, template IDs, metadata, environment variables, commands, filesystem
operations, port hosts, pause, resume, and kill.

```go
type SandboxAdapter interface {
	Create(ctx context.Context, input CreateSandboxInput) (SandboxHandle, error)
	Connect(ctx context.Context, sandboxID string) (SandboxHandle, error)
	Pause(ctx context.Context, sandboxID string) error
	Kill(ctx context.Context, sandboxID string) error
	SetTimeout(ctx context.Context, sandboxID string, timeout time.Duration) error
	RunCommand(ctx context.Context, sandboxID string, command CommandSpec) (CommandResult, error)
	StartCommand(ctx context.Context, sandboxID string, command CommandSpec) (CommandHandle, error)
	WriteFile(ctx context.Context, sandboxID string, path string, data []byte) error
	ReadFile(ctx context.Context, sandboxID string, path string) ([]byte, error)
	PortHost(ctx context.Context, sandboxID string, port int) (string, error)
	CreateSnapshot(ctx context.Context, sandboxID string) (SnapshotHandle, error)
}
```

The adapter should wrap E2B.

For the initial product, E2B is not just an interchangeable backend. e2b-agents
should be designed around hosted E2B sandboxes as the execution substrate:

- instance creation calls E2B sandbox creation
- preset templates map to E2B template IDs
- instance records store the E2B sandbox ID and E2B sandbox state
- commands, file writes, port hosts, pause, resume, and shutdown use E2B APIs
- E2B metadata should include e2b-agents instance ID, owner type, owner ID,
  team ID for team-owned instances, preset ID, source, and request ID for
  recovery
- E2B env vars should be treated as runtime config and secrets, never as UI
  state
- the control plane stays outside E2B as an always-on API, worker, and Slack
  gateway

## Security Requirements

e2b-agents should enforce:

- owner-bound access to agents, instances, and agent sessions
- service-principal scopes
- idempotency for service-principal create flows
- Slack request signature verification
- Slack workspace-to-team authorization
- Slack user-to-team membership resolution
- short-lived connect tickets for WebSockets
- no browser access to internal ACP endpoints in the MVP
- no shared secret leakage in forwarded headers
- no raw provider tokens in logs
- no repo credentials in user-visible errors
- rate limits on Slack events, instance creation, and ACP connect
- audit logs for create, delete, team resolution, and gateway actions

## Observability

e2b-agents should emit:

- structured logs
- request IDs
- create duration metrics
- sandbox creation failure counters
- setup latency metrics
- ACP readiness latency metrics
- active instance gauges
- active agent gauges
- ACP connection counters
- gateway delivery counters
- E2B lifecycle events
- e2b-agents setup transition events

## Failure Behavior

Expected failure cases:

- preset not found
- owner mismatch
- team mismatch
- Slack workspace unresolved
- Slack user unresolved
- Slack user mapping forbidden
- quota exceeded
- E2B sandbox creation failed
- sandbox missing
- sandbox timeout elapsed
- ACP unavailable
- ACP metadata invalid
- connect ticket expired
- repository clone failed
- runtime context failed to apply

Each failure should produce a typed error for API clients and a useful Slack
message when the request came from Slack.

## MVP Scope

The first working version should include:

- API service
- worker service
- Slack gateway
- Postgres schema
- one configured identity mode: E2B-infra reuse or hosted-E2B local compatible
  identity
- e2b-agents-owned Slack workspace links and Slack user links
- E2B sandbox adapter
- preset catalog
- agent to instance routing
- create/get/delete instances
- runtime context write
- ACP readiness check
- agent session metadata
- ACP WebSocket bridge
- Slack message delivery to ACP
- ACP response delivery back to Slack
- sandbox timeout cleanup

Nice-to-have but not required for MVP:

- web UI
- CLI
- SSH
- port forwarding
- terminal WebSocket
- shared mounts
- Teams-like channel gateway
- durable bindings
- admin console
- full audit viewer

## Non-Goals

The control plane should not:

- become the agent runtime
- store channel-provider bot logic in the runtime template
- let a service principal impersonate a human after create
- expose E2B credentials to users
- require users to know E2B sandbox internals
- make the UI the source of truth for preset behavior
- store durable transcripts unless explicitly designed as a product feature
