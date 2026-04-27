# ACP Runtime Architecture

## Goal

The Slack gateway should talk to an ACP runtime interface without caring which harness is running inside the sandbox.

Harness-specific behavior belongs behind adapters. OpenClaw exposes ACP for local use, but e2b-agents should connect through a Go adapter that presents the same runtime interface as other harness adapters. Claude and Codex can be reached through their ACP adapters as well.

## ACP References

Use the upstream ACP spec as the source of truth while implementing the runtime layer:

- [ACP repository](https://github.com/zed-industries/agent-client-protocol)
- [Protocol overview](https://github.com/zed-industries/agent-client-protocol/blob/main/docs/protocol/overview.mdx)
- [Transports](https://github.com/zed-industries/agent-client-protocol/blob/main/docs/protocol/transports.mdx)
- [Initialization](https://github.com/zed-industries/agent-client-protocol/blob/main/docs/protocol/initialization.mdx)
- [Session setup](https://github.com/zed-industries/agent-client-protocol/blob/main/docs/protocol/session-setup.mdx)
- [Prompt turn](https://github.com/zed-industries/agent-client-protocol/blob/main/docs/protocol/prompt-turn.mdx)
- [Content model](https://github.com/zed-industries/agent-client-protocol/blob/main/docs/protocol/content.mdx)
- [Tool calls](https://github.com/zed-industries/agent-client-protocol/blob/main/docs/protocol/tool-calls.mdx)
- [File system callbacks](https://github.com/zed-industries/agent-client-protocol/blob/main/docs/protocol/file-system.mdx)
- [Terminal callbacks](https://github.com/zed-industries/agent-client-protocol/blob/main/docs/protocol/terminals.mdx)
- [Protocol schema](https://github.com/zed-industries/agent-client-protocol/blob/main/schema/schema.json)
- [Protocol metadata](https://github.com/zed-industries/agent-client-protocol/blob/main/schema/meta.json)

## Target Flow

1. Slack sends an event to the Go service.
2. The Go service resolves the Slack workspace and conversation.
3. The Go service derives the ACP session key from the Slack team, channel, and thread/channel surface.
4. The runtime manager finds the active sandbox for that workspace.
5. The ACP client opens or reuses a live ACP connection to the sandbox runtime.
6. The ACP client initializes the connection if needed.
7. The ACP client creates or loads the ACP session for the Slack surface.
8. The ACP client sends the user message with `session/prompt`.
9. The ACP runtime streams progress through `session/update` notifications.
10. The ACP runtime completes the prompt turn with a stop reason.
11. The Go service posts the final assistant response back to Slack in the same surface:
   - channel message in, channel message out
   - thread message in, thread reply out

## ACP Protocol Model

ACP is a bidirectional JSON-RPC session protocol.

It should not be treated as a simple `POST message -> text response` API.

The standard lifecycle is:

1. Client connects to the agent runtime over a transport.
2. Client sends `initialize`.
3. Agent returns protocol version, capabilities, auth methods, and agent info.
4. Client creates a session with `session/new`, or resumes one with `session/load` if supported.
5. Client sends user turns with `session/prompt`.
6. Agent sends `session/update` notifications while it works.
7. Agent may call client methods such as permission, file, or terminal requests.
8. Agent responds to `session/prompt` with a stop reason.

The gateway should act as an ACP client. The sandbox runtime should act as the ACP agent through a harness adapter.

The core service should not import, call, or special-case a harness directly. It should depend on ACP lifecycle operations and runtime template metadata only.

### Transport

The standard ACP transport is newline-delimited JSON-RPC over stdio:

- client writes JSON-RPC messages to agent stdin
- agent writes JSON-RPC messages to stdout
- agent may write logs to stderr
- stdout must contain only valid ACP messages

ACP is transport-agnostic, so a sandbox runtime may expose ACP over another bidirectional transport if the template declares it. The gateway should preserve the ACP lifecycle either way.

For remote sandboxes, the practical shape is a generic in-sandbox adapter process:

- it starts the configured harness ACP command
- it owns the local stdio ACP connection when the harness only exposes stdio locally
- it exposes a bidirectional remote transport to the Go service
- it forwards ACP JSON-RPC messages without adding harness-specific semantics

This adapter is a transport and lifecycle adapter, not an OpenClaw-specific integration. The same adapter shape should work for OpenClaw, Claude, Codex, or any other harness that can speak ACP.

### Prompt Turns

A Slack message maps to one ACP prompt turn.

The gateway sends:

- `sessionId`
- prompt content blocks, usually one text block for Slack text

The agent can stream:

- assistant text chunks
- thought chunks
- plans
- tool calls
- tool call updates
- mode/config updates
- available command updates

For Slack, the first implementation can collect assistant text chunks and post one final response. Later, it can stream or update Slack messages if that UX is needed.

### Client Callbacks

ACP is bidirectional. During a prompt turn, the agent may call methods on the client.

Common client-side methods include:

- `session/request_permission`
- `fs/read_text_file`
- `fs/write_text_file`
- `terminal/create`
- `terminal/output`
- `terminal/wait_for_exit`
- `terminal/kill`
- `terminal/release`

For e2b-agents, these callbacks need an explicit policy. Slack-only chat sessions may expose fewer client capabilities than a code editor. The gateway should advertise only the capabilities it actually implements.

## Responsibilities

### Database

The database is the source of truth for durable state:

- Slack workspace ID
- Slack team ID
- default team/template
- current sandbox ID
- current ACP session ID
- setup status
- last activity
- last error

It should not store every Slack event delivery.

### Runtime Manager

The runtime manager owns sandbox lifecycle:

- ensure a sandbox exists
- create a sandbox from a template when needed
- recover from expired or missing sandboxes
- cache warm runtime connection and session data in memory
- evict stale cache entries on failure

The cache is only a speed layer. If the process restarts, the database still has enough state to recover.

### ACP Client

The ACP client owns runtime communication:

- initialize ACP connections
- create or load ACP sessions
- send user messages with `session/prompt`
- collect `session/update` notifications
- respond to supported agent-to-client requests
- return a final assistant response for Slack
- report structured errors
- support future streaming without changing Slack routing code

The Slack gateway should depend on this interface, not on harness details.

### Harness Adapters

Harness adapters translate template configuration into a running ACP-compatible process.

They own:

- the harness startup command
- required environment variables
- working directory
- template identity
- health checks
- local transport details, such as stdio
- any one-time bootstrap needed before ACP `initialize`

They do not own:

- Slack routing
- Slack channel or thread behavior
- ACP session keys
- conversation persistence policy
- response formatting for Slack
- recovery policy beyond reporting health and startup failures

The first adapters should be:

- OpenClaw through its local ACP entrypoint, exposed remotely through the generic sandbox adapter
- Claude through the ACP adapter maintained for that harness
- Codex through the ACP adapter maintained for that harness

The gateway should see all three as the same thing: an ACP runtime.

### Session Router

The session router maps Slack surfaces to ACP sessions.

Expected session keys:

- channel conversation: `slack-v1-<team_id>-<channel_id>-channel`
- thread conversation: `slack-v1-<team_id>-<channel_id>-<thread_ts>`
- direct conversation: `slack-v1-<team_id>-<channel_id>-direct`

This keeps channel and thread context separate while preserving multi-turn memory within each surface.

## Warm Path

For a ready workspace with a live sandbox:

1. Read workspace state from the database.
2. Look up the sandbox runtime connection in the in-memory cache.
3. If cached, reuse the live ACP connection and session.
4. If not cached, reconnect to the sandbox runtime and run ACP `initialize`.
5. Create or load the ACP session for the Slack surface.
6. Send the Slack text as a `session/prompt` content block.
7. Collect assistant text from `session/update` notifications until the prompt response completes.
8. Post the reply to Slack.

The warm path should not:

- create a sandbox
- reconfigure the runtime
- start a short-lived helper process
- reconnect to E2B for every message
- create a new ACP session for every message in the same Slack surface

## Recovery Path

If the warm path fails because the sandbox is gone or unhealthy:

1. Evict the in-memory cache entry.
2. Mark setup as creating or recovering.
3. Ensure the sandbox through the lifecycle path.
4. Update the database with the new sandbox ID.
5. Initialize ACP against the recovered runtime.
6. Create or load the ACP session.
7. Send the message to the recovered runtime.
8. Mark the workspace ready again.

Only availability-style failures should trigger recovery. Authentication, malformed request, model, or protocol errors should fail clearly and keep the original error visible.

## Template Contract

To support multiple ACP harnesses, templates need a clear contract.

Each template should define:

- harness name
- startup behavior
- ACP protocol transport
- exposed port
- health check endpoint or command
- session semantics
- whether `session/load` is supported
- supported client capabilities expected by the harness
- supported prompt content types
- supported streaming behavior
- required environment variables
- adapter command and arguments
- adapter version marker

The Go gateway should read this as configuration instead of hardcoding harness-specific behavior.

## Production Requirements

Before treating this as a generic ACP runtime layer, verify at least two different harness adapters through the same interface.

The implementation should provide:

- one shared transport/client layer with connection reuse
- per-sandbox in-memory ACP connection cache with expiry
- per-Slack-surface ACP session cache
- `singleflight` protection for duplicate ensure/connect work
- per-conversation serialization where needed
- structured timing logs for connect, initialize, session create/load, prompt, update collection, Slack post, and database update
- clear recovery classification for missing, expired, or unreachable sandboxes
- no Slack-specific logic inside ACP client implementations
- no harness-specific logic inside Slack routing or the generic ACP client
- explicit capability advertisement for client callbacks
- graceful handling for unsupported agent-to-client methods
- adapter conformance tests against a fake ACP harness
- at least one real OpenClaw adapter test and one second harness adapter test

## Cutover Plan

The implementation should cut over directly to the generic ACP runtime path.

### Step 1: Define the Runtime Interfaces

- Add explicit `ACPClient`, `RuntimeManager`, and `HarnessAdapter` interfaces.
- Keep Slack routing dependent only on ACP concepts.
- Represent a Slack message as an ACP `session/prompt`.
- Represent Slack channel/thread/direct surfaces as ACP sessions.

### Step 2: Add the Generic Sandbox Adapter

- Run a long-lived adapter process inside each sandbox.
- Let the adapter start the configured ACP harness command.
- Use stdio locally when that is what the harness exposes.
- Expose a bidirectional remote transport to the Go gateway.
- Forward ACP JSON-RPC messages without harness-specific behavior.

### Step 3: Add Harness Adapters

- Add an OpenClaw adapter that starts the OpenClaw ACP entrypoint.
- Add configuration slots for Claude and Codex ACP adapters.
- Keep all adapter-specific command, environment, and health details outside Slack routing.

### Step 4: Keep Runtime Sessions Warm

- Keep live per-sandbox runtime clients in memory.
- Reuse active connections where the transport supports it.
- Keep ACP sessions alive per Slack surface.
- Serialize prompt turns per ACP session.
- Add background health checks for active sandboxes.
- Recreate unhealthy sandboxes before user traffic hits them when possible.

### Step 5: Verify the Cutover

- Test the ACP client against a fake adapter.
- Test the OpenClaw adapter in an E2B sandbox.
- Test a second harness adapter through the same gateway path.
- Run a Slack conversation with at least 10 meaningful turns.
- Verify channel messages receive channel replies.
- Verify thread messages receive thread replies.
- Verify responses have no spacing or formatting errors.

## Current Status

The current production path still uses an OpenAI-style HTTP endpoint exposed by the runtime rather than a direct ACP JSON-RPC connection.

The next implementation should replace that path with the generic ACP runtime path. OpenClaw should be one adapter behind the interface, not a dependency of the gateway.

Generic support should be claimed only after:

- the gateway speaks the ACP lifecycle directly
- the gateway handles `session/update` notifications
- the gateway has a clear policy for agent-to-client callbacks
- OpenClaw uses the adapter interface successfully
- another harness uses the same interface successfully
