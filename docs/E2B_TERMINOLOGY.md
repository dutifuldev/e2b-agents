# E2B Terminology

This is a working glossary of E2B terms that matter for e2b-agents. It separates
E2B provider concepts from e2b-agents control-plane concepts so the product can
store E2B state without copying E2B terminology into every user-facing label.

## Core Concepts

| Term | Meaning for e2b-agents |
| --- | --- |
| E2B | Hosted sandbox infrastructure used as the execution substrate. |
| Sandbox | A fast, isolated Linux VM created on demand. This is the runtime unit e2b-agents creates and manages through E2B. |
| Template | A reusable starting environment for sandboxes. e2b-agents presets should map to E2B template IDs or aliases. |
| Snapshot | A point-in-time capture of a running sandbox, including filesystem and memory state. Can be used to create new sandboxes from captured runtime state. |
| Volume | Persistent storage independent of a sandbox lifecycle. Volumes are currently described by E2B as private beta. |
| Dashboard | E2B web surface for keys, templates, teams, and sandbox visibility. |
| SDK | E2B client libraries. The main SDKs are JavaScript/TypeScript and Python. |
| CLI | E2B command-line tool for template and sandbox workflows. |
| REST API | HTTP API for sandbox management. Requests use the team API key. |
| BYOC | Bring Your Own Cloud deployment option for running E2B infrastructure in a customer cloud. Not needed for the first e2b-agents MVP. |

## Identity And Authentication

| Term | Meaning for e2b-agents |
| --- | --- |
| API key | Team-scoped key used by SDK/API calls. e2b-agents stores this server-side and never exposes it to users. |
| Access token | Token used for some CLI/user auth flows. Distinct from the API key. |
| Team | E2B account/group scope for API keys, templates, sandboxes, and billing. |
| Team ID | Identifier for an E2B team. Needed by some CLI and API workflows. |
| X-API-Key | REST API header used to authenticate sandbox API requests. |
| Secure sandbox | Sandbox mode where system communication with the sandbox is secured. |
| envd access token | Token returned for secure sandboxes to authenticate calls to the sandbox daemon. |
| traffic access token | Token returned for accessing sandbox traffic through E2B proxying. |

## Sandbox Identifiers

| Term | Meaning for e2b-agents |
| --- | --- |
| sandbox ID | Primary E2B identifier for a sandbox. Store this on e2b-agents instance records. |
| template ID | Identifier of the E2B template used to create the sandbox. |
| alias | Template alias returned by E2B for a created sandbox. |
| client ID | Deprecated identifier returned by the sandbox create API. Do not use as a primary key. |
| domain | Deprecated response field. E2B recommends constructing sandbox URLs from the port and sandbox ID. |
| envd version | Version of the sandbox daemon available inside the sandbox. Some features depend on it. |

## Sandbox Lifecycle

| Term | Meaning for e2b-agents |
| --- | --- |
| Running | Sandbox is active and can execute commands or serve traffic. |
| Paused | Sandbox execution is suspended, but filesystem and memory state are preserved. |
| Snapshotting | Sandbox is briefly paused while a persistent snapshot is being created, then returns to running. |
| Killed | Sandbox is terminated and resources are released. This is terminal. |
| Create | Start a new sandbox from a template or snapshot. |
| Connect | Attach to an existing sandbox. If paused, this can resume it. |
| Pause | Preserve sandbox state while stopping active execution. |
| Resume | Return a paused sandbox to running state, usually via connect or auto-resume. |
| Kill | Permanently terminate a sandbox. It cannot be resumed after kill. |
| Timeout | Sandbox lifetime countdown. E2B APIs expose seconds in REST and milliseconds in SDK options. |
| `timeoutMs` | JavaScript/TypeScript SDK timeout value in milliseconds. |
| `setTimeout` | SDK method to update a running sandbox timeout. |
| `onTimeout` | Lifecycle setting that controls whether timeout kills or pauses a sandbox. |
| `autoPause` | REST/API terminology for pausing a sandbox after timeout. |
| `autoResume` | Setting that allows paused sandboxes to resume automatically when activity arrives. |
| Lifecycle event | E2B event describing sandbox creation, pause, resume, update, snapshot, or kill. |
| Lifecycle webhook | Webhook form of lifecycle event delivery. Useful for keeping e2b-agents state in sync. |

## Lifecycle Event Types

| Term | Meaning for e2b-agents |
| --- | --- |
| created | Sandbox was created. |
| paused | Sandbox was paused. |
| resumed | Sandbox was resumed. |
| updated | Sandbox metadata or lifecycle-related data changed. |
| snapshotted | Snapshot was created from a sandbox. |
| killed | Sandbox was terminated. |

## Templates

| Term | Meaning for e2b-agents |
| --- | --- |
| Template builder | SDK API for defining templates in code. |
| Base image | Container image used as the starting point for a template. |
| Base template | Existing E2B template used as the starting point for another template. |
| `e2b.Dockerfile` | E2B template definition file used by CLI workflows. |
| Start command | Command run during template build and captured in the template snapshot. Useful for prestarted services. |
| Ready command | Command or readiness check used to decide when a template build is ready to snapshot. |
| Build | Process of creating a template from a template definition. |
| Build logs | Logs emitted during template build. |
| Template tag | Human-friendly tag used to create sandboxes from a specific template version. |
| Template version | Versioned form of a template. Useful for stable production presets. |
| Caching | E2B template-build caching for faster rebuilds. |
| Layer command | Command executed as part of template provisioning. |
| Default user | User context inside the sandbox. |
| Workdir | Default working directory inside the sandbox. |
| Kernel | E2B sandboxes run on an LTS Linux kernel fixed at template build time. Rebuild templates to move to newer kernel versions. |

## Commands

| Term | Meaning for e2b-agents |
| --- | --- |
| `sandbox.commands.run` | Run a command inside the sandbox and return output. |
| Background command | Long-running command started without waiting for completion. |
| Command handle | Handle for a running command or process. |
| Command result | Completed command output, including stdout, stderr, and exit information. |
| stdout | Standard output stream. |
| stderr | Standard error stream. |
| PID | Process identifier for commands that can be listed, connected to, or killed. |
| PTY | Pseudo-terminal. Needed for interactive terminal sessions. |
| stdin | Standard input stream for interactive commands. |
| Command timeout | Timeout for command execution or stream connection. Separate from sandbox timeout. |
| Connect to command | Attach to an existing running command or PTY session. |
| Kill command | Terminate a running command by PID. |

## Filesystem

| Term | Meaning for e2b-agents |
| --- | --- |
| Filesystem | Sandbox-local file tree. Ephemeral unless saved in a snapshot or volume. |
| Read file | SDK/API operation to read sandbox file contents. |
| Write file | SDK/API operation to write sandbox file contents. |
| List directory | SDK/API operation to inspect directory entries. |
| Upload | Transfer local or remote data into sandbox or volume storage. |
| Download | Transfer sandbox or volume data out. |
| Watch directory | Subscribe to filesystem events for a directory. |
| File metadata | File attributes such as UID, GID, mode, size, owner, group, modified time, and symlink target. |
| File type | Classification such as file, directory, or symlink. |
| UID | Numeric user owner of a file. |
| GID | Numeric group owner of a file. |
| Mode | Unix file permissions. |

## Volumes

| Term | Meaning for e2b-agents |
| --- | --- |
| Volume | Persistent storage object that can outlive sandboxes. |
| Volume name | Human-readable E2B volume identifier. |
| Volume mount | Mapping from a volume to a path inside a sandbox. |
| Mount path | Filesystem path where a volume appears inside a sandbox. |
| Standalone volume access | SDK access to volume contents when not mounted into a sandbox. |
| Volume metadata | File or directory metadata inside a volume. |

## Networking

| Term | Meaning for e2b-agents |
| --- | --- |
| Port host | E2B host for traffic to a sandbox port. In SDKs, this is exposed through helpers like `getHost(port)`. |
| Sandbox URL | Public URL routed to a sandbox port. REST docs describe the pattern `https://{port}-{sandboxID}.e2b.app`. |
| Proxy tunneling | E2B feature for routing traffic to services running inside sandboxes. |
| Custom domain | E2B feature for mapping a custom domain to sandbox traffic. |
| Internet access | Sandbox ability to reach the public internet. |
| `allow_internet_access` | REST create option controlling internet access. |
| Network config | Create-time configuration for public traffic and outbound allow/deny rules. |
| `allowPublicTraffic` | Network setting that permits public traffic to the sandbox. |
| `allowOut` | Outbound allowlist. |
| `denyOut` | Outbound denylist. |
| `maskRequestHost` | Network setting for request host masking. |
| SSH access | E2B feature for SSH access to a sandbox. |
| Secured access | E2B security mode for sandbox communication and traffic access. |

## Metadata And Environment

| Term | Meaning for e2b-agents |
| --- | --- |
| Metadata | Arbitrary key-value data attached to a sandbox. Useful for owner IDs, instance IDs, source, and trace IDs. |
| Metadata filtering | Listing or finding sandboxes by metadata. Useful for recovery. |
| Environment variables | Key-value values injected into a sandbox at creation time. |
| `envVars` | REST create field for environment variables. |
| `envs` | SDK create option for environment variables. |
| Secrets | Sensitive values passed as environment variables or provider config. e2b-agents should keep them server-side. |

## MCP Gateway

| Term | Meaning for e2b-agents |
| --- | --- |
| MCP | Model Context Protocol, an open protocol for connecting models to tools and data sources. |
| MCP gateway | E2B component that runs inside sandboxes and exposes MCP tools through a unified interface. |
| MCP server | Tool server available through the MCP gateway. |
| Docker MCP Catalog | Catalog of prebuilt MCP servers that can be used with E2B's MCP gateway. |
| `mcp` config | Sandbox create configuration for MCP servers. |
| MCP URL | URL clients use to connect to the sandbox MCP gateway. |
| MCP token | Authorization token used to access the sandbox MCP gateway. |
| `mcp-gateway` template | E2B base template with MCP gateway support. |
| Pre-pulled MCP server | MCP server image downloaded during template build to speed up sandbox startup. |

## Agents And Example Runtimes

| Term | Meaning for e2b-agents |
| --- | --- |
| Coding agent | Agent that uses a sandbox to inspect, edit, run, and test code. |
| Computer use | Agent workflow where the sandbox includes a browser or desktop-like interface. |
| Desktop sandbox | E2B desktop-oriented sandbox for computer-use agents. |
| OpenClaw | Agent runtime with an E2B-documented gateway launch path. e2b-agents may support it as a managed preset. |
| OpenClaw gateway | OpenClaw web UI and WebSocket gateway that can run inside an E2B sandbox. |
| Claude Code | Example agent runtime supported by E2B docs. |
| Codex | Example coding-agent runtime supported by E2B docs. |
| OpenCode | Example coding-agent runtime supported by E2B docs. |
| OpenAI Agents SDK | Agent framework shown in E2B docs as a sandbox use case. |
| Amp | Agent runtime shown in E2B docs as a sandbox use case. |

## e2b-agents Mapping

| E2B term | e2b-agents term |
| --- | --- |
| Sandbox | Instance runtime substrate |
| Sandbox ID | Provider sandbox ID on an instance record |
| Template | Preset runtime image/template |
| Template ID or tag | Preset provider reference |
| Running | E2B public state for an active sandbox |
| Paused | E2B public state for a resumable sandbox |
| Timeout with kill | Missing or no-longer-connectable sandbox runtime |
| Snapshot | Runtime checkpoint or fork source |
| Volume | Persistent owner/team storage option |
| Metadata | Provider-level recovery and correlation data |
| Env vars | Runtime secret/config injection |
| Port host | Upstream target for authenticated e2b-agents proxying |
| MCP gateway | Optional tool gateway inside a runtime |

## Sources

- E2B docs home: https://e2b.dev/docs
- Sandbox create API: https://e2b.dev/docs/api-reference/sandboxes/create-sandbox
- Sandbox persistence and states: https://e2b.dev/docs/sandbox/persistence
- Auto-resume: https://e2b.dev/docs/sandbox/auto-resume
- Lifecycle events API: https://e2b.dev/docs/sandbox/lifecycle-events-api
- Sandbox metadata: https://e2b.dev/docs/sandbox/metadata
- Sandbox snapshots: https://e2b.dev/docs/sandbox/snapshots
- Template quickstart: https://e2b.dev/docs/template/quickstart
- Template internals: https://e2b.dev/docs/template/how-it-works
- Volumes: https://e2b.dev/docs/volumes
- MCP gateway overview: https://e2b.dev/docs/mcp
- MCP custom templates: https://e2b.dev/docs/mcp/custom-templates
- OpenClaw gateway reference: https://e2b.dev/docs/agents/openclaw/openclaw-gateway
