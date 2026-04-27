# Warm Runtime Latency

This document describes the production target for Slack reply latency.

## Current Problem

Slack replies are still slower than they should be even when the sandbox and ACP
session are reused.

The reason is that every message still performs runtime setup work before it
sends the prompt:

1. connect to the current E2B sandbox
2. extend the sandbox timeout
3. write runtime files
4. run runtime configuration commands
5. check gateway readiness
6. send the prompt
7. post the Slack reply

For warm messages, most of that work is unnecessary. The sandbox is already the
agent instance, and the ACP session key is already known.

## Near-Term Fix

Use a direct-send fast path:

1. Load the Slack workspace row.
2. If the workspace is `ready` and has `current_sandbox_id`, send directly to
   the current sandbox using the channel-scoped ACP session key.
3. If direct send succeeds, post the Slack reply.
4. If direct send fails because the runtime is unavailable, run `Ensure`.
5. After `Ensure`, retry send once.

The normal warm path should become:

```text
Slack message -> DB lookup -> direct ACP send -> Slack reply
```

The recovery path should be:

```text
Slack message -> direct ACP send fails -> Ensure runtime -> retry send -> Slack reply
```

Only runtime availability failures should trigger recovery:

- sandbox not found or expired
- connection refused
- gateway not reachable
- gateway readiness timeout
- transient network failure connecting to the sandbox gateway

Do not recover/retry for real request failures:

- invalid prompt
- model/provider error
- auth/config error
- malformed runtime response
- Slack post failure

## Production Target

The long-term target is that Slack request handling never waits on sandbox setup.

Production shape:

```text
Slack event -> acknowledge Slack -> enqueue message job -> worker sends to warm runtime -> Slack reply
```

Behind that, a supervisor owns runtime health:

- keep one warm sandbox per active workspace/channel surface
- start or reconnect sandboxes before user traffic needs them
- configure the gateway once per sandbox lifecycle
- refresh sandbox timeout in the background
- mark the workspace degraded if the runtime cannot be kept warm
- recover dead sandboxes outside the Slack event acknowledgement path

In this model, the message worker normally only sends over ACP. Runtime setup is
not part of the user-facing reply path.

## Observability

Add timing logs and metrics for each stage:

- Slack event acknowledgement
- queue wait
- direct ACP send
- ensure/recovery
- retry send
- model response time
- Slack post time

The goal is to make each slow reply explainable from logs without guessing
whether the time was spent in Slack, the database, E2B, the runtime gateway, or
the model provider.

## Design Rules

- Keep the agent interface pure ACP.
- Do not add Slack-specific tools to the underlying agent.
- Do not require new product tables for the near-term fast path.
- Keep the workspace lock around recovery so concurrent messages do not race to
  recreate the same runtime.
- Treat E2B sandbox IDs as running agent instance IDs.
- Treat E2B templates as the agent image.
