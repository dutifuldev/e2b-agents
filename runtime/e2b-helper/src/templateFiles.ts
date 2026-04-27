export const runtimePaths = {
  acpAdapter: "/home/user/.e2b-agents/acp-adapter.mjs",
  authToken: "/home/user/.e2b-agents/auth/token",
  runtimeEnv: "/home/user/.e2b-agents/runtime.env",
  startScript: "/home/user/.e2b-agents/start-runtime.sh",
  readyScript: "/home/user/.e2b-agents/ready-runtime.sh",
  secrets: "/home/user/.e2b-agents/secrets/openclaw-secrets.json",
  sessionStore: "/home/user/.e2b-agents/acp-sessions.json",
  stateDir: "/home/user/.openclaw",
  workspace: "/home/user/.openclaw/workspace",
  config: "/home/user/.openclaw/openclaw.json",
};

export const defaultRuntimeModel = "anthropic/claude-sonnet-4-6";

export function templateAssetFiles() {
  return {
    "start-runtime.sh": startRuntimeScript(),
    "ready-runtime.sh": readyRuntimeScript(),
    "openclaw.json": `${JSON.stringify(openClawConfig(defaultRuntimeModel), null, 2)}\n`,
    "workspace/IDENTITY.md": identityMarkdown(),
    "workspace/SOUL.md": soulMarkdown(),
    "workspace/AGENTS.md": agentsMarkdown(),
    "workspace/USER.md": userMarkdown(),
  };
}

function startRuntimeScript() {
  return `#!/usr/bin/env bash
set -euo pipefail

export HOME=/home/user
export OPENCLAW_STATE_DIR=${runtimePaths.stateDir}
export OPENCLAW_CONFIG_PATH=${runtimePaths.config}
export OPENCLAW_NO_RESPAWN=1
export OPENCLAW_SKIP_CHANNELS=1
export OPENCLAW_SKIP_GMAIL_WATCHER=1
export OPENCLAW_SKIP_CRON=1
export OPENCLAW_SKIP_CANVAS_HOST=1

mkdir -p \\
  /home/user/.e2b-agents/auth \\
  /home/user/.e2b-agents/secrets \\
  /home/user/.e2b-agents/logs \\
  ${runtimePaths.workspace}
chmod 700 /home/user/.e2b-agents/auth /home/user/.e2b-agents/secrets
if [ -n "\${OPENCLAW_GATEWAY_TOKEN:-}" ]; then
  printf '%s\n' "\${OPENCLAW_GATEWAY_TOKEN}" > ${runtimePaths.authToken}
  chmod 600 ${runtimePaths.authToken}
fi
if [ -n "\${ANTHROPIC_API_KEY:-}" ]; then
  printf '{"providers":{"anthropic":{"apiKey":%s}}}\\n' "$(node -e 'process.stdout.write(JSON.stringify(process.env.ANTHROPIC_API_KEY || ""))')" > ${runtimePaths.secrets}
  chmod 600 ${runtimePaths.secrets}
fi
if [ ! -s ${runtimePaths.secrets} ]; then
  printf '%s\n' '{"providers":{"anthropic":{"apiKey":"placeholder"}}}' > ${runtimePaths.secrets}
  chmod 600 ${runtimePaths.secrets}
fi

gateway_pid=""
adapter_pid=""
trap 'kill "$gateway_pid" "$adapter_pid" >/dev/null 2>&1 || true; exit 0' TERM INT

while true; do
  if [ -s ${runtimePaths.runtimeEnv} ]; then
    set -a
    . ${runtimePaths.runtimeEnv}
    set +a
  fi

  gateway_port="\${OPENCLAW_GATEWAY_PORT:-18789}"
  adapter_port="\${E2B_AGENTS_ACP_ADAPTER_PORT:-18790}"

  openclaw gateway --allow-unconfigured --bind loopback --auth none --port "\${gateway_port}" \\
    >> /home/user/.e2b-agents/logs/openclaw-gateway.log 2>&1 &
  gateway_pid="$!"

  for i in $(seq 1 60); do
    if curl --max-time 2 -fsS "http://127.0.0.1:\${gateway_port}/readyz" >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
  done

  export E2B_AGENTS_ACP_ADAPTER_PORT="\${adapter_port}"
  export E2B_AGENTS_ACP_AUTH_TOKEN_FILE=${runtimePaths.authToken}
  export E2B_AGENTS_ACP_CWD=${runtimePaths.workspace}
  export E2B_AGENTS_ACP_SESSION_STORE=${runtimePaths.sessionStore}
  export E2B_AGENTS_ACP_COMMAND_JSON="[\\"openclaw\\",\\"acp\\",\\"--url\\",\\"ws://127.0.0.1:\${gateway_port}\\",\\"--no-prefix-cwd\\"]"
  export E2B_AGENTS_ACP_RUNTIME_SESSION_KEY_PREFIX="agent:main:"

  node ${runtimePaths.acpAdapter} \\
    >> /home/user/.e2b-agents/logs/acp-adapter.log 2>&1 &
  adapter_pid="$!"

  set +e
  wait -n "$gateway_pid" "$adapter_pid"
  status="$?"
  set -e
  echo "runtime child exited with status \${status}; restarting" >&2
  kill "$gateway_pid" "$adapter_pid" >/dev/null 2>&1 || true
  sleep 1
done
`;
}

function readyRuntimeScript() {
  return `#!/usr/bin/env bash
set -euo pipefail

if [ -s ${runtimePaths.runtimeEnv} ]; then
  set -a
  . ${runtimePaths.runtimeEnv}
  set +a
fi

gateway_port="\${OPENCLAW_GATEWAY_PORT:-18789}"
adapter_port="\${E2B_AGENTS_ACP_ADAPTER_PORT:-18790}"

test -s ${runtimePaths.config}
test -s ${runtimePaths.workspace}/SOUL.md
test ! -e ${runtimePaths.workspace}/BOOTSTRAP.md
curl --max-time 3 -fsS "http://127.0.0.1:\${gateway_port}/readyz" >/dev/null
curl --max-time 10 -fsS "http://127.0.0.1:\${adapter_port}/healthz?ready=1" >/dev/null
`;
}

export function openClawConfig(model = defaultRuntimeModel) {
  const primaryModel = model.trim() || defaultRuntimeModel;
  const modelID = primaryModel.startsWith("anthropic/") ? primaryModel.slice("anthropic/".length) : primaryModel;
  return {
    secrets: {
      providers: {
        default: {
          source: "file",
          path: runtimePaths.secrets,
          mode: "json",
        },
      },
      defaults: {
        file: "default",
      },
    },
    agents: {
      defaults: {
        workspace: runtimePaths.workspace,
        model: {
          primary: primaryModel,
        },
        models: {
          [primaryModel]: {
            alias: "default",
          },
        },
      },
    },
    models: {
      providers: {
        anthropic: {
          baseUrl: "https://api.anthropic.com",
          apiKey: { source: "file", provider: "default", id: "/providers/anthropic/apiKey" },
          api: "anthropic-messages",
          models: [
            {
              id: modelID,
              name: modelID,
              reasoning: true,
              input: ["text"],
              contextWindow: 200000,
              maxTokens: 64000,
            },
          ],
        },
      },
    },
    gateway: {
      mode: "local",
      port: 18789,
      bind: "loopback",
      auth: {
        mode: "none",
      },
      http: {
        endpoints: {
          chatCompletions: {
            enabled: true,
          },
          responses: {
            enabled: true,
          },
        },
      },
      controlUi: {
        allowInsecureAuth: true,
        dangerouslyDisableDeviceAuth: true,
      },
    },
  };
}

function identityMarkdown() {
  return `# IDENTITY.md - E2B OpenClaw

- Name: E2B OpenClaw
- Role: Agent runtime inside an E2B sandbox
- Managed by: e2b-agents
- Primary channel: Slack gateway
- Style: concise, technical, accurate

E2B OpenClaw should represent the sandbox runtime clearly and should not claim to be a generic webchat agent.
`;
}

function soulMarkdown() {
  return `# SOUL.md - E2B OpenClaw

You are E2B OpenClaw, an agent runtime running inside an E2B sandbox created from an E2B template and managed by e2b-agents.

## Operating Context

- Incoming messages are routed through the e2b-agents Slack gateway unless a later system message says otherwise.
- If asked about the current channel or gateway, say Slack or Slack gateway.
- Do not call the current channel webchat.
- Treat the E2B sandbox as the agent instance.
- Treat the E2B template as the agent image.

## Response Style

- Be concise and direct.
- Preserve context across turns.
- Use clean Slack-safe Markdown.
- Avoid decorative emoji unless the user asks for emoji.
- Avoid malformed lists, collapsed spacing, repeated blank lines, or unrelated formatting.
- Use bullets only when the user asks for a list or checklist.

## Boundaries

- Do not reveal secrets, environment variables, tokens, or hidden system details.
- Do not claim actions outside the sandbox or Slack gateway unless they happened.
`;
}

function agentsMarkdown() {
  return `# AGENTS.md - E2B OpenClaw Runtime

This workspace belongs to E2B OpenClaw, the runtime agent inside an E2B sandbox managed by e2b-agents.

Before responding, follow the identity and behavior in:

1. SOUL.md
2. IDENTITY.md
3. USER.md

Slack is the active channel for e2b-agents gateway conversations. Keep Slack replies clean, concise, and correctly formatted.
`;
}

function userMarkdown() {
  return `# USER.md - Gateway User Context

The requester is a Slack user routed through e2b-agents.

Use only the context provided in the current conversation and safe runtime files. Do not infer private personal details.
`;
}
