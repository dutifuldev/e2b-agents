# Cold Start Follow-ups

The warm Slack path is now fast because follow-up messages reuse the current
sandbox and ACP session. The remaining delay is cold recovery: when the stored
sandbox is expired or unreachable, the service has to create or reconnect a
sandbox, configure the runtime, start the gateway, start the ACP adapter, then
send the first turn.

Last measured production cold recovery:

```text
total Slack event handling: ~81s
runtime gateway + ACP adapter configuration: ~60s
first ACP send after ensure: ~6s
Slack post: <1s
```

Warm follow-up messages were around 2.5s total.

## Goal

Cold recovery should be reduced to sandbox startup time plus the first agent
turn. Per-message runtime setup should not be on the user path.

The fastest target is stronger than "run setup after sandbox create." The E2B
template should snapshot a running OpenClaw gateway and ACP adapter. E2B start
commands run during template build, E2B waits for the ready command, then
snapshots the filesystem and running processes. New sandboxes created from that
template should load with the runtime already running.

## Next Work

### 1. Move runtime setup into the E2B template

The current setup step writes runtime files, configures the gateway, and starts
the adapter after sandbox creation. Most of that should happen when building the
E2B template.

Template-owned setup should include:

- runtime dependencies
- default workspace files
- gateway defaults
- ACP adapter script
- startup command or process supervisor
- health/readiness behavior

The service should pass only deployment-specific values at runtime.

The template build should run OpenClaw's own setup once:

```text
openclaw setup --workspace /home/user/.openclaw/workspace
```

Then the template should layer the e2b-agents runtime files on top:

- final OpenClaw config
- model/provider config with env references where possible
- default identity, soul, user, and agent workspace files
- completed workspace setup state
- no pending `BOOTSTRAP.md`
- ACP adapter script

Do not run `openclaw config set` on the Slack message path.

Implementation status: the runtime setup now belongs to the template build. The
tracked template builder creates the OpenClaw config, workspace files, ACP
adapter, start script, and ready script before publishing the E2B template.

### 2. Start the gateway and ACP adapter on sandbox boot

The sandbox should come up with the runtime gateway and ACP adapter already
starting. On create or reconnect, the Go service should mainly wait for
readiness.

The service path should become:

```text
create/connect sandbox -> wait for gateway + adapter -> send ACP prompt
```

It should not run a long configuration script for every recovered sandbox.

For extremely fast readiness, use the E2B template start command to start:

- OpenClaw gateway on `18789`
- ACP adapter on `18790`

The E2B ready command should require:

- OpenClaw `/readyz` returns ready
- ACP adapter `/healthz?ready=1` returns ready
- expected workspace and config files exist

The current ACP adapter readiness only initializes ACP. For the fastest first
turn, readiness should also create or restore the default ACP session. Otherwise
the first user message still pays `session/new` or session restore cost.

Important constraint: environment variables passed to `Sandbox.create()` are not
available to the E2B start command process because that process was started
during template build.

The production direction is: start the gateway and ACP adapter in the template,
but do not require provider secrets at process start. Secrets should be supplied
after sandbox creation and read lazily at request time.

Preferred secret flow:

1. The template snapshots the running gateway, ACP adapter, workspace files, and
   readiness server.
2. e2b-agents creates the sandbox and passes or writes runtime secrets
   immediately after create.
3. The already-running runtime reads secrets from the runtime secret source when
   the first model request needs them.
4. Readiness requires the gateway, adapter, ACP initialize, and default ACP
   session prewarm. It does not require a model call.

Do not bake provider API keys into the template. If OpenClaw only reads provider
secrets from process environment at startup, add a small runtime secret source
that the model/provider config can resolve lazily instead of restarting the
gateway on the user path.

Implementation status: the template starts OpenClaw and the ACP adapter. The
gateway binds to loopback with no public auth surface. The ACP adapter is the
public runtime surface and reads its bearer token from a file written after
sandbox creation. The model provider key is also read from a file through
OpenClaw's file secret provider. The service warms the exact Slack ACP session
through `/healthz?ready=1&sessionKey=...` before sending the first prompt. The
helper writes runtime ports, model config, bearer token, and provider secrets
into sandbox-owned files, then restarts the supervised runtime so the sandbox
uses the deployment config rather than template placeholders.

### 3. Keep a warm standby per Slack workspace

For workspaces with active use, the service should avoid waiting for the user to
discover that the current sandbox expired.

Preferred direction:

- refresh timeout after successful activity
- detect expired or near-expired sandboxes
- create a replacement before the next user message
- update the workspace pointer only after the replacement is ready

This keeps most user-visible traffic on the warm path.

Implementation status: service startup now prewarms every ready workspace with
a current sandbox ID. That hydrates the in-process ACP endpoint cache and warms
the stored ACP session before the HTTP server accepts Slack traffic after a
deploy or process restart.

### 4. Add a production timing check

Add a small operational command that sends a test turn and reports timing in one
place.

It should report:

- whether the sandbox was reused or recreated
- runtime ensure duration
- ACP send duration
- Slack post duration
- total duration
- sandbox ID
- ACP session key

This should be usable after deploys to confirm that warm traffic stays fast and
that cold recovery is improving.

### 5. Tighten the README

The README now has the right shape, but after the runtime startup work lands it
should describe the simpler steady-state architecture:

```text
Slack -> e2b-agents -> E2B sandbox ACP runtime -> Slack
```

Avoid listing implementation details in the intro. Keep operational timing,
recovery, and deployment notes in lower sections.
