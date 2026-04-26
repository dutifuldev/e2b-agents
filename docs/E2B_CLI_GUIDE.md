# E2B CLI Guide

This guide shows how to use the E2B CLI for the workflows e2b-agents needs:
creating sandbox templates, starting sandboxes, running commands in them, and
cleaning them up.

## Mental Model

E2B does not expose a separate "container" object for day-to-day use.

The practical flow is:

1. Write a `Dockerfile` or `e2b.Dockerfile`.
2. Build it into an E2B template.
3. Create sandboxes from that template.
4. Use the sandbox ID as the runtime identifier.

In E2B terms:

| Term | Meaning |
| --- | --- |
| Dockerfile | Input used to define the filesystem and startup process. |
| Template | Snapshot produced from the Dockerfile build. This is the reusable image. |
| Sandbox | Running VM created from a template. This is the live runtime. |
| Sandbox ID | Identifier for a running sandbox. In e2b-agents this is also the agent runtime ID. |

During template creation, E2B builds a container from the Dockerfile, extracts and
configures its filesystem, starts a sandbox, optionally runs a start command,
waits for readiness, then snapshots the result as a template.

## Install

On macOS:

```sh
brew install e2b
```

Alternative:

```sh
npm i -g @e2b/cli
```

Check the installed version:

```sh
e2b --version
```

## Authenticate

For SDK/API-style sandbox operations, set the team API key:

```sh
export E2B_API_KEY="..."
```

For local development in this repo:

```sh
set -a
source .env
set +a
```

Some template/account commands use E2B user authentication instead of only the
team API key. If a template command asks for an access token, run:

```sh
e2b auth login
e2b auth info
```

Some template commands also accept an explicit team:

```sh
e2b template list --team <team-id>
```

The team ID is available in the E2B dashboard team settings.

## List Sandboxes

List running sandboxes:

```sh
e2b sandbox list
```

JSON output:

```sh
e2b sandbox list --format json
```

List paused sandboxes:

```sh
e2b sandbox list --state paused
```

Filter by metadata:

```sh
e2b sandbox list --metadata owner=dev --format json
```

## Create A Sandbox

Create a sandbox from a template and attach an interactive terminal:

```sh
e2b sandbox create <template>
```

Example:

```sh
e2b sandbox create base
```

This starts the sandbox, connects your terminal, keeps it alive while connected,
and kills it when you exit the terminal.

For e2b-agents development, detached mode is usually more useful:

```sh
e2b sandbox create <template> --detach
```

The CLI prints the sandbox ID. Save that ID for later commands:

```sh
SANDBOX_ID="<sandbox-id>"
```

## Inspect A Sandbox

Show sandbox details:

```sh
e2b sandbox info "$SANDBOX_ID"
```

JSON output:

```sh
e2b sandbox info "$SANDBOX_ID" --format json
```

Connect an interactive terminal to a running sandbox:

```sh
e2b sandbox connect "$SANDBOX_ID"
```

## Run Commands In A Sandbox

Run a command and wait for output:

```sh
e2b sandbox exec "$SANDBOX_ID" -- pwd
e2b sandbox exec "$SANDBOX_ID" -- uname -a
e2b sandbox exec "$SANDBOX_ID" -- ls -la /home/user
```

Run from a specific working directory:

```sh
e2b sandbox exec "$SANDBOX_ID" --cwd /home/user -- npm test
```

Run as a specific user:

```sh
e2b sandbox exec "$SANDBOX_ID" --user root -- apt-get update
```

Pass environment variables:

```sh
e2b sandbox exec "$SANDBOX_ID" \
  --env FOO=bar \
  --env MODE=dev \
  -- printenv FOO
```

Start a background command:

```sh
e2b sandbox exec "$SANDBOX_ID" --background -- sleep 300
```

## Logs And Metrics

Show logs:

```sh
e2b sandbox logs "$SANDBOX_ID"
```

Follow logs:

```sh
e2b sandbox logs "$SANDBOX_ID" --follow
```

Show only higher-severity logs:

```sh
e2b sandbox logs "$SANDBOX_ID" --level WARN
```

Show metrics:

```sh
e2b sandbox metrics "$SANDBOX_ID"
```

Follow metrics:

```sh
e2b sandbox metrics "$SANDBOX_ID" --follow
```

## Pause, Resume, And Kill

Pause a sandbox:

```sh
e2b sandbox pause "$SANDBOX_ID"
```

Resume it:

```sh
e2b sandbox resume "$SANDBOX_ID"
```

Kill one sandbox:

```sh
e2b sandbox kill "$SANDBOX_ID"
```

Kill all running sandboxes:

```sh
e2b sandbox kill --all
```

Kill all paused sandboxes:

```sh
e2b sandbox kill --all --state paused
```

## Create A Template From A Dockerfile

Create a minimal template directory:

```sh
mkdir -p tmp_e2b_template
cd tmp_e2b_template
```

Example `Dockerfile`:

```Dockerfile
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /home/user
```

Build it as an E2B template:

```sh
e2b template create e2b-agents-dev --dockerfile Dockerfile
```

With a start command:

```sh
e2b template create e2b-agents-dev \
  --dockerfile Dockerfile \
  --cmd "/usr/local/bin/e2b-agents-runtime"
```

With a readiness command:

```sh
e2b template create e2b-agents-dev \
  --dockerfile Dockerfile \
  --cmd "/usr/local/bin/e2b-agents-runtime" \
  --ready-cmd "curl -fsS http://127.0.0.1:8080/healthz"
```

With resource sizing:

```sh
e2b template create e2b-agents-dev \
  --dockerfile Dockerfile \
  --cpu-count 2 \
  --memory-mb 1024
```

Skip build cache:

```sh
e2b template create e2b-agents-dev --dockerfile Dockerfile --no-cache
```

## Manage Templates

List templates:

```sh
e2b template list
e2b template list --format json
```

List templates for a specific team:

```sh
e2b template list --team <team-id> --format json
```

Create a sandbox from the new template:

```sh
e2b sandbox create e2b-agents-dev --detach
```

Publish a template:

```sh
e2b template publish e2b-agents-dev
```

Unpublish a template:

```sh
e2b template unpublish e2b-agents-dev
```

Delete a template:

```sh
e2b template delete e2b-agents-dev --yes
```

## Template SDK Initialization

The CLI can scaffold a template SDK project:

```sh
e2b template init --name e2b-agents-dev --language typescript
```

Supported scaffold languages in the installed CLI are:

```text
typescript
python-sync
python-async
```

For this repo, use Go for e2b-agents control-plane code. If JavaScript-family
source is needed for a template SDK scaffold, use TypeScript.

## e2b-agents Development Loop

The expected local loop is:

1. Build or rebuild the runtime template:

   ```sh
   e2b template create e2b-agents-dev --dockerfile Dockerfile
   ```

2. Start a detached sandbox:

   ```sh
   e2b sandbox create e2b-agents-dev --detach
   ```

3. Export the sandbox ID:

   ```sh
   export SANDBOX_ID="<sandbox-id>"
   ```

4. Check the runtime health endpoint from inside the sandbox:

   ```sh
   e2b sandbox exec "$SANDBOX_ID" -- curl -fsS http://127.0.0.1:8080/healthz
   ```

5. Inspect logs:

   ```sh
   e2b sandbox logs "$SANDBOX_ID" --follow
   ```

6. Kill the sandbox when done:

   ```sh
   e2b sandbox kill "$SANDBOX_ID"
   ```

## Common Problems

### `E2B_API_KEY` Works For Sandboxes But Template Commands Fail

Sandbox API operations can work with `E2B_API_KEY`, while some template commands
may require CLI user auth:

```sh
e2b auth login
```

After login, retry:

```sh
e2b template list --format json
```

If the account has multiple teams, pass the team ID:

```sh
e2b template list --team <team-id> --format json
```

### Sandbox Is Not Listed

By default, `e2b sandbox list` shows running sandboxes. Check paused sandboxes:

```sh
e2b sandbox list --state paused
```

### Need Machine-Readable Output

Use JSON where available:

```sh
e2b sandbox list --format json
e2b sandbox info "$SANDBOX_ID" --format json
e2b template list --format json
```

## References

- E2B CLI docs: https://e2b.dev/docs/cli
- Create sandbox: https://e2b.dev/docs/cli/create-sandbox
- CLI template reference: https://e2b.dev/docs/sdk-reference/cli/v2.7.2/template
- Template build process: https://e2b.dev/docs/template/how-it-works
