# Follow-Up Work

This document lists the next cleanup and production-readiness tasks after the
Slack reply placement fix.

## 1. Make Slack Session Identity Match The Conversation Surface

Status: implemented.

Reply placement is now correct:

- channel-root Slack message -> reply in the channel root
- threaded Slack message -> reply in that thread
- DM -> reply in the DM

The remaining decision is memory/session identity.

The production-ready direction is to remember by durable Slack place, not by
individual message timestamp:

- DM -> one ACP session for that DM channel
- channel -> one ACP session for that Slack channel
- thread -> use the channel ACP session for now

This keeps the gateway simple and keeps the runtime interface purely ACP. The
agent harness should not need Slack-specific tools or metadata to decide where
memory lives.

Implementation summary:

- Keep `replyThreadTS(event)` as the only helper used for Slack reply placement.
- Replace message-timestamp based channel session keys with channel-scoped
  session keys.
- Keep threaded Slack replies visually threaded, but route them through the
  channel-scoped ACP session.
- Add tests proving repeated channel messages use the same channel session key.
- Add tests proving threaded messages still reply in-thread while using the
  channel session key.

## 2. Make Slack Reply Verification Reusable

The signed Slack event verification is useful and should become a repeatable dev
command or script.

It should verify:

- top-level app mention replies in channel root with no `thread_ts`
- threaded app mention replies with the thread root `thread_ts`
- production `healthz` and `readyz` are OK before and after the test
- visible Slack output has no obvious formatting errors

## 3. Add GitHub CI

GitHub currently reports no required checks for this repository.

Add basic CI for:

- `go test ./...`
- `go vet ./...`
- `git diff --check`

Make these checks required before merge.

## 4. Speed Up Production Deploys

The VM rebuild currently compiles the Go binary during deploy. That made the last
deploy slow.

Preferred direction:

- build the Docker image in CI
- push the image to a registry
- make the VM deploy pull and restart that image

This keeps production deploys faster and more predictable.

## 5. Clean Up Database Logging

Application errors now use structured `log/slog`, but GORM still prints noisy
colored slow-query logs.

Configure GORM logging for production so database logs are easier to search and
read with container logs.
