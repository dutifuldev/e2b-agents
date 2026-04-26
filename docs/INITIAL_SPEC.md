# e2b-agents Product And System Spec

Status: initial planning spec

## Purpose

e2b-agents is a control plane for disposable, user-owned agent runtimes.

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
request source, URLs, auth policy, setup progress, and audit history. The record
does not represent a second runtime. It is metadata that e2b-agents stores for
the E2B sandbox.

Each instance belongs to one human owner. It may have been requested by that
owner directly, by an administrator, or by a narrow service principal such as a
chat gateway.

The requester and the owner are separate concepts:

- the requester is the actor that asked e2b-agents to create the instance
- the owner is the human who can later access and use the instance

This distinction matters because bots and automations can create instances for
humans without becoming those humans.

## Main Capabilities

e2b-agents should provide:

- preset-based agent instance creation
- custom sandbox image or template creation where policy allows it
- owner-bound access control
- service-principal create flows for external systems
- external identity resolution for chat or workflow platforms
- idempotent create requests for bots and automation
- canonical instance URLs
- a web UI for creating, listing, opening, and chatting with agents
- an API for instance and conversation management
- an ACP WebSocket gateway
- terminal access through the control plane
- optional SSH access through short-lived credentials
- optional port forwarding through the control plane
- instance web app proxying
- app-level readiness discovery when a preset needs it
- metadata discovery for agent name, version, and capabilities
- sandbox timeout and idle-timeout lifecycle enforcement
- activity tracking
- optional shared state mounts
- channel gateway support for Slack-like or Teams-like surfaces
- CLI support for local and automation workflows
- audit-friendly metadata on who requested what and why

## High-Level Architecture

```text
Human user / external bot / automation
              |
              v
        e2b-agents API
 auth, policy, create, URLs, gatewaying
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

- authenticate humans, admins, and service principals
- authorize instance and conversation access
- validate create requests
- resolve presets
- resolve external owners
- enforce placement and quota policy
- create durable instance records
- expose canonical routes for open, chat, terminal, and API use
- mint short-lived WebSocket connect tickets
- proxy ACP traffic to runtime instances
- proxy terminal and port-forward traffic where supported
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

The web UI is the first-party human surface.

It should support:

- instance list
- create flow
- preset picker
- repository input
- instruction input
- sandbox state and setup status badges
- open action for the runtime web UI
- chat surface for ACP conversations
- terminal surface
- settings for channel integrations
- clear error states for provisioning, setup, auth, and expired instances

### CLI

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

The CLI should not own policy, URL construction, preset expansion, or owner
resolution. Those belong in the API.

### Channel Gateway

A channel gateway connects external messaging systems to e2b-agents.

Responsibilities:

- handle provider OAuth or installation flows
- verify provider webhooks
- map external workspace/channel/user identities to e2b-agents records
- resolve or create a channel conversation
- request an instance for a human owner when needed
- bootstrap an ACP conversation
- send provider messages as ACP prompts
- deliver assistant text back to the external channel
- manage channel-level routing and mention policies

The channel gateway should not create sandboxes directly. It should call the
e2b-agents API.

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

The browser should never connect directly to ACP. The path is always:

```text
browser -> e2b-agents API -> sandbox ACP endpoint
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
conversation flows.

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
  "ownerId": "user_123",
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

Presets are the source of truth for runtime launch behavior. The UI should read
launchable presets from the API and create instances with `presetId`, not by
copying hidden environment details into the browser.

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

## Create Flow

Human create flow:

1. Authenticate the caller.
2. Parse the create request.
3. Resolve the namespace or workspace scope.
4. Resolve the preset.
5. Apply create admission rules.
6. Require the owner to match the caller unless the caller is an admin.
7. Generate a human-readable name if none was provided.
8. Resolve lifetimes.
9. Store a durable instance record.
10. Queue setup work if the preset requires post-create work.
11. Return the instance record and canonical URLs.

Service-principal create flow:

1. Authenticate the machine caller.
2. Validate service-principal scopes.
3. Resolve direct or external owner.
4. Validate preset and placement policy.
5. Enforce idempotency key semantics.
6. Enforce per-actor and per-owner limits.
7. Store requester metadata.
8. Store resolved owner metadata.
9. Store or replay the durable instance record.
10. Queue setup work if the preset requires post-create work.
11. Return canonical URLs and sandbox metadata.

Service principals should not automatically receive post-create access to the
instance.

## External Owner Resolution

External systems often know a provider-native user ID, not an internal owner ID.

The create API should accept either:

```json
{
  "ownerId": "user_123"
}
```

or:

```json
{
  "ownerRef": {
    "type": "external",
    "provider": "slack",
    "tenant": "T123",
    "subject": "U456"
  }
}
```

Rules:

- `ownerId` and `ownerRef` are mutually exclusive
- external references are allowed for service principals by default
- provider, tenant, and subject are opaque values
- tenant is required for providers where user IDs are not global
- the resolver namespace comes from the authenticated service principal
- the caller must not choose an arbitrary resolver namespace
- unresolved, forbidden, and ambiguous mappings should return typed errors

## Conversations

An ACP conversation stores metadata only.

Recommended fields:

- conversation ID
- instance ID
- owner ID
- title
- current working directory
- ACP session ID
- binding state
- previous session ID
- agent info
- capabilities
- last bound timestamp
- last replay timestamp
- last error

The durable transcript belongs to the runtime or ACP backend unless product
requirements explicitly add a separate transcript store.

One instance can have many conversations.

## ACP Conversation Bootstrap

Before opening a chat WebSocket, the API should be able to bootstrap or repair
the conversation binding.

Flow:

1. Authorize access to the conversation.
2. Load the owning instance.
3. Confirm the instance setup is complete and ACP-capable.
4. Open a short-lived ACP socket to the runtime.
5. Send ACP initialize.
6. If the conversation has an existing session ID, try to load it.
7. If the session is missing, create a new session and mark the binding as
   replaced.
8. If there is no session ID, create a new session.
9. Persist effective session ID, cwd, agent info, and capabilities.
10. Return the updated conversation metadata.

## WebSocket Connect Tickets

WebSocket URLs should be protected by short-lived connect tickets.

Tickets should include:

- type
- subject resource
- owner
- requester
- expiry
- nonce
- allowed protocol

Ticket types:

- ACP conversation
- terminal session
- port forward

Ticket validation should happen before WebSocket upgrade.

## Instance Web Proxy

If a runtime exposes a web UI, e2b-agents should proxy it behind an authenticated
route.

Recommended public path:

```text
/i/:instanceId/*
```

Proxy behavior:

- authorize user access
- resolve sandbox web target
- strip the external prefix if configured
- set forwarded headers
- remove browser-auth headers before forwarding
- return clear errors for missing, forbidden, setup incomplete, and upstream
  failure

## Terminal Access

The web terminal should go through e2b-agents.

Flow:

1. User requests terminal connect ticket.
2. Browser opens WebSocket to e2b-agents.
3. API authorizes owner access.
4. API connects to the sandbox shell mechanism.
5. API streams terminal input/output.
6. API reports activity after input.

The preferred implementation can be either:

- E2B command or PTY streaming
- a runtime-local terminal daemon
- SSH through short-lived credentials

The user-facing interface should stay stable regardless of implementation.

## SSH Access

Optional SSH access should use short-lived credentials.

Flow:

1. Client generates an ephemeral key pair.
2. Client sends public key to API.
3. API verifies owner access.
4. API mints a short-lived cert or access credential.
5. Client opens SSH using the returned host, port, user, key, and cert.

Long-lived shared SSH keys should not be the default.

## Port Forwarding

Port forwarding should support local developer workflows.

Flow:

```text
local TCP port -> CLI -> WebSocket to API -> sandbox target port
```

The API should enforce:

- owner access
- allowed target ports
- running sandbox state or completed setup status
- ticket expiry
- activity refresh

## Shared State

e2b-agents may support shared owner-scoped or team-scoped storage.

Use cases:

- carry memory across disposable instances
- share config between related runtime sessions
- preserve workspace fragments
- avoid recloning or rebuilding expensive context

Rules:

- shared state should be opt-in per preset or create request
- mounts should be scoped to owner, team, or explicit policy
- revisions should be addressable
- latest revision should be easy to fetch
- runtime access should not expose another owner state

## Durable Bindings

Some integrations need a stable logical binding whose active runtime can change.

Example:

```text
external channel -> logical binding -> active instance
```

A binding should support:

- stable external key
- desired revision
- active instance reference
- candidate instance reference
- cleanup instance reference
- disconnected state
- attributes
- adoption of an existing active instance

Binding states:

```text
pending
creating
waiting_ready
cutting_over
cleaning_up
ready
failed
```

This enables safe replacement:

1. Keep current active instance.
2. Create candidate instance for new revision.
3. Wait until candidate is ready.
4. Switch active reference.
5. Clean up old active instance.

## API Surface

Public API:

```text
GET    /healthz
GET    /presets
GET    /instances
POST   /instances/suggest-name
POST   /instances
GET    /instances/:id
DELETE /instances/:id
PATCH  /instances/:id/user-config
GET    /agents
GET    /conversations
POST   /conversations
GET    /conversations/:id
PATCH  /conversations/:id
POST   /conversations/:id/bootstrap
POST   /conversations/:id/connect-ticket
GET    /conversations/:id/connect
POST   /instances/:id/terminal/connect-ticket
GET    /instances/:id/terminal
POST   /instances/:id/ssh
GET    /instances/:id/port-forward
```

Internal API:

```text
GET    /internal/v1/presets/:presetId
PUT    /internal/v1/bindings/:bindingKey
GET    /internal/v1/bindings/:bindingKey
DELETE /internal/v1/bindings/:bindingKey
POST   /internal/v1/bindings/:bindingKey/reconcile
POST   /internal/v1/instances
GET    /internal/v1/instances/:scope/:id
DELETE /internal/v1/instances/:scope/:id
PUT    /internal/v1/shared-mounts/owner/:owner/:mount/latest
GET    /internal/v1/shared-mounts/owner/:owner/:mount/latest
```

Channel integration API:

```text
POST /channel-routes/resolve
POST /channel-conversations/upsert
```

## Data Model

Recommended tables:

```text
users
teams
service_principals
presets
instances
instance_events
conversations
connect_tickets
external_owner_links
provisioning_requests
channel_installations
channel_routes
channel_conversations
runtime_bindings
shared_mounts
shared_mount_revisions
audit_events
```

`instances` should include:

```text
id
name
owner_id
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
repo_url
repo_branch
repo_dir
runtime_context_json
agent_profile_json
acp_status_json
ssh_status_json
web_url
port_hosts_json
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

## E2B Sandbox Adapter Interface

e2b-agents should isolate direct E2B behavior behind an adapter. Product code
should speak in e2b-agents instances. Adapter code should speak in E2B sandbox
IDs, template IDs, metadata, environment variables, commands, filesystem
operations, port hosts, pause, resume, and kill.

```ts
interface E2BSandboxAdapter {
  create(input: CreateSandboxInput): Promise<SandboxHandle>
  connect(sandboxId: string): Promise<SandboxHandle>
  pause(sandboxId: string): Promise<void>
  kill(sandboxId: string): Promise<void>
  setTimeout(sandboxId: string, timeoutMs: number): Promise<void>
  runCommand(sandboxId: string, command: string, options?: RunOptions): Promise<CommandResult>
  startCommand(sandboxId: string, command: string, options?: StartOptions): Promise<CommandHandle>
  writeFile(sandboxId: string, path: string, data: Uint8Array | string): Promise<void>
  readFile(sandboxId: string, path: string): Promise<Uint8Array>
  getPortHost(sandboxId: string, port: number): string
  createSnapshot(sandboxId: string): Promise<SnapshotHandle>
}
```

The adapter should wrap E2B.

For the initial product, E2B is not just an interchangeable backend. e2b-agents
should be designed around hosted E2B sandboxes as the execution substrate:

- instance creation calls E2B sandbox creation
- preset templates map to E2B template IDs
- instance records store the E2B sandbox ID and E2B sandbox state
- commands, file writes, port hosts, pause, resume, and shutdown use E2B APIs
- E2B metadata should include e2b-agents instance ID, owner ID, preset ID,
  source, and request ID for recovery
- E2B env vars should be treated as runtime config and secrets, never as UI
  state
- the control plane stays outside E2B as an always-on API, UI, and worker

## Security Requirements

e2b-agents should enforce:

- owner-bound access to instances and conversations
- service-principal scopes
- idempotency for service-principal create flows
- short-lived connect tickets for WebSockets
- origin checks for browser WebSockets
- no direct browser access to internal ACP endpoints
- no shared secret leakage in forwarded headers
- no raw provider tokens in logs
- no repo credentials in user-visible errors
- rate limits on create, terminal, SSH credential minting, and ACP connect
- audit logs for create, delete, owner resolution, and gateway actions

## Observability

e2b-agents should emit:

- structured logs
- request IDs
- create duration metrics
- sandbox creation failure counters
- setup latency metrics
- ACP readiness latency metrics
- active instance gauges
- active conversation gauges
- terminal connection counters
- ACP connection counters
- gateway delivery counters
- E2B lifecycle events
- e2b-agents setup transition events

## Failure Behavior

Expected failure cases:

- preset not found
- owner mismatch
- external owner unresolved
- external owner forbidden
- quota exceeded
- E2B sandbox creation failed
- sandbox missing
- sandbox timeout elapsed
- ACP unavailable
- ACP metadata invalid
- terminal unavailable
- web proxy target unavailable
- connect ticket expired
- repository clone failed
- runtime context failed to apply

Each failure should produce a typed error for API clients and a useful message
for the UI.

## MVP Scope

The first working version should include:

- API service
- worker service
- Postgres schema
- E2B sandbox adapter
- preset catalog
- create/list/get/delete instances
- runtime context write
- ACP readiness check
- ACP conversation metadata
- ACP WebSocket proxy
- basic React UI
- terminal WebSocket
- sandbox timeout cleanup

Nice-to-have but not required for MVP:

- SSH
- port forwarding
- shared mounts
- channel gateway
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
