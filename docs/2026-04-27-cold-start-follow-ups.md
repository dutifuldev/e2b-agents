# Cold Start Follow-ups

The warm Slack path is now fast because follow-up messages reuse the current
sandbox and ACP session. The remaining delay is cold recovery: when the stored
sandbox is expired or unreachable, the service has to create or reconnect a
sandbox, configure the runtime, and send the first turn.

Previously measured production cold recovery:

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

The helper now keeps that benefit in the normal cold path. After
`Sandbox.create()`, it writes runtime files, asks OpenClaw to reload secrets,
checks that the ACP adapter HTTP process is live, and then sends the prompt
without restarting OpenClaw.

If the first prompt after that fast ensure returns an availability-style error,
the gateway forces one runtime recovery and resends the same prompt. This keeps
the normal path short while still handling a snapshotted adapter that is
listening but cannot yet complete the ACP prompt path.

This assumes the runtime model matches the model baked into the template. For a
new sandbox, if the deployment requests a different model, the helper restarts
the runtime so OpenClaw reads the new config instead of silently serving the
template config. The production direction is to publish a matching template when
config-affecting runtime settings change.

For reconnected sandboxes, including custom-model deployments, the helper checks
the gateway's active default model before it rewrites runtime config and before
taking the fast path. If the running model does not match the requested model, it
restarts instead of serving stale runtime config.

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

### 2. Use the snapshotted gateway and ACP adapter

The sandbox should come up with the runtime gateway and ACP adapter already
running from the E2B template snapshot. On create or reconnect, the Go service
should not restart those processes unless recovery proves they are broken.

The service path should become:

```text
create/connect sandbox -> write runtime files -> reload runtime secrets -> send ACP prompt
```

It should not run a long configuration script for every recovered sandbox.

For extremely fast readiness, use the E2B template start command to start:

- OpenClaw gateway on `18789`
- ACP adapter on `18790`

The E2B ready command should require:

- OpenClaw `/readyz` returns ready
- ACP adapter `/healthz?ready=1` returns ready
- expected workspace and config files exist

OpenClaw's own readiness model matters here:

- `/healthz` means the gateway HTTP process is live.
- `/readyz` is stricter and waits for startup sidecars.
- `chat.send` is not blocked by the startup sidecars that gate `/readyz`.
- `secrets.reload` can refresh runtime secrets without restarting the gateway.

For e2b-agents, the first Slack prompt does not need every OpenClaw sidecar to
be ready. It needs the gateway process, ACP adapter, runtime secret files, and
the `chat.send` path.

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
3. e2b-agents calls OpenClaw `secrets.reload` through the gateway/ACP sidecar.
4. The already-running runtime reads secrets from the refreshed runtime secret
   source when the first model request needs them.
5. The first prompt goes directly to `/prompt`; the adapter may create or load
   the ACP session as part of that request.

Do not bake provider API keys into the template. Do not restart OpenClaw just to
pick up provider secrets. OpenClaw already has file-backed secret providers and a
`secrets.reload` gateway method, so runtime secret updates should use that.

Implementation status: the template starts OpenClaw and the ACP adapter. The
gateway binds to loopback with no public auth surface. The ACP adapter is the
public runtime surface and reads its bearer token from a file written after
sandbox creation. The model provider key is also read from a file through
OpenClaw's file secret provider. The helper writes runtime ports, model config,
bearer token, and provider secrets into sandbox-owned files, calls OpenClaw
`secrets.reload`, and verifies that the ACP adapter HTTP process is live. It
does not restart the supervised runtime in the normal cold path when the
requested model matches the template model. If the first prompt after ensure
gets an availability-style failure, the gateway retries through a forced runtime
recovery. If the already-running gateway or adapter is unavailable, or if a
config-affecting model override differs from the template, restart or recreate
the sandbox.

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
