import { Sandbox } from "e2b";
import { createHash } from "node:crypto";

type Envelope = {
  command: "ensure" | "send";
  input: unknown;
  model: string;
  gatewayPort: number;
  gatewayToken: string;
  sandboxTimeoutMs: number;
};

type EnsureInput = {
  sandboxId?: string;
  templateId: string;
  teamId: string;
  requesterUserId: string;
  sessionKey: string;
  metadata?: Record<string, string>;
};

type SendInput = {
  sandboxId: string;
  prompt: string;
  sessionKey: string;
};

const gatewayFingerprintPath = "/home/user/.openclaw/e2b-agents-gateway.sha256";

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

async function main() {
  const envelope = JSON.parse(await readStdin()) as Envelope;
  if (envelope.command === "ensure") {
    const output = await ensureRuntime(envelope.input as EnsureInput, envelope);
    writeJSON(output);
    return;
  }
  if (envelope.command === "send") {
    const output = await sendPrompt(envelope.input as SendInput, envelope);
    writeJSON(output);
    return;
  }
  throw new Error(`unsupported command: ${String(envelope.command)}`);
}

async function ensureRuntime(input: EnsureInput, envelope: Envelope) {
  if (!input.templateId) throw new Error("templateId is required");
  if (!input.sessionKey) throw new Error("sessionKey is required");

  const envs = {
    ANTHROPIC_API_KEY: requiredEnv("ANTHROPIC_API_KEY"),
    OPENCLAW_GATEWAY_TOKEN: envelope.gatewayToken,
    E2B_AGENTS_RUNTIME_MODEL: envelope.model,
  };

  const runtime = await connectOrCreateSandbox(input, envelope, envs);

  await configureGateway(runtime.sandbox, envelope);
  const host = runtime.sandbox.getHost(envelope.gatewayPort);
  return {
    sandboxId: runtime.sandbox.sandboxId,
    templateId: input.templateId,
    host,
    baseUrl: host.startsWith("http") ? host : `https://${host}`,
    sessionKey: input.sessionKey,
  };
}

async function sendPrompt(input: SendInput, envelope: Envelope) {
  if (!input.sandboxId) throw new Error("sandboxId is required");
  if (!input.sessionKey) throw new Error("sessionKey is required");
  if (!input.prompt.trim()) throw new Error("prompt is required");

  const sandbox = await Sandbox.connect(input.sandboxId, {
    apiKey: requiredEnv("E2B_API_KEY"),
    requestTimeoutMs: 60_000,
  });
  const host = sandbox.getHost(envelope.gatewayPort);
  const baseUrl = host.startsWith("http") ? host : `https://${host}`;
  const response = await fetch(`${baseUrl}/v1/chat/completions`, {
    method: "POST",
    headers: {
      authorization: `Bearer ${envelope.gatewayToken}`,
      "content-type": "application/json",
      "x-openclaw-agent-id": "main",
      "x-openclaw-session-key": input.sessionKey,
    },
    body: JSON.stringify({
      model: "openclaw:main",
      user: input.sessionKey,
      messages: [
        {
          role: "system",
          content: runtimeSystemPrompt(),
        },
        {
          role: "user",
          content: input.prompt,
        },
      ],
    }),
  });
  const bodyText = await response.text();
  if (!response.ok) {
    throw new Error(`runtime HTTP ${response.status}: ${bodyText.slice(0, 500)}`);
  }
  const body = JSON.parse(bodyText) as {
    choices?: Array<{ message?: { content?: string } }>;
    output_text?: string;
  };
  const text = body.choices?.[0]?.message?.content ?? body.output_text ?? "";
  if (!text.trim()) {
    throw new Error("runtime returned an empty reply");
  }
  return {
    text: normalizeText(text),
    sessionKey: input.sessionKey,
  };
}

async function connectOrCreateSandbox(input: EnsureInput, envelope: Envelope, envs: Record<string, string>) {
  if (input.sandboxId) {
    try {
      const sandbox = await Sandbox.connect(input.sandboxId, {
        apiKey: requiredEnv("E2B_API_KEY"),
        requestTimeoutMs: 60_000,
      });
      await sandbox.setTimeout(envelope.sandboxTimeoutMs || 3_600_000, { requestTimeoutMs: 60_000 });
      return { sandbox, created: false };
    } catch (error) {
      if (!isMissingSandboxError(error)) throw error;
      return createSandbox(input, envelope, envs);
    }
  }
  return createSandbox(input, envelope, envs);
}

function isMissingSandboxError(error: unknown) {
  const message = error instanceof Error ? error.message : String(error);
  return /\b(404|410)\b/.test(message) || /not found|expired|does not exist/i.test(message);
}

async function createSandbox(input: EnsureInput, envelope: Envelope, envs: Record<string, string>) {
  const sandbox = await Sandbox.create(input.templateId, {
    apiKey: requiredEnv("E2B_API_KEY"),
    timeoutMs: envelope.sandboxTimeoutMs || 3_600_000,
    requestTimeoutMs: 120_000,
    envs,
    metadata: input.metadata ?? {},
  });
  return { sandbox, created: true };
}

async function configureGateway(sandbox: Sandbox, envelope: Envelope) {
  const baseEnvs = {
    ANTHROPIC_API_KEY: requiredEnv("ANTHROPIC_API_KEY"),
  };
  const gatewayEnvs = {
    ...baseEnvs,
    OPENCLAW_GATEWAY_TOKEN: envelope.gatewayToken,
  };
  await sandbox.commands.run(`mkdir -p /home/user/.openclaw/agents/main/agent /home/user/.openclaw/workspace`, {
    requestTimeoutMs: 60_000,
    envs: baseEnvs,
  });
  await sandbox.files.write(
    "/home/user/.openclaw/agents/main/agent/models.json",
    JSON.stringify(
      {
        mode: "merge",
        providers: {
          anthropic: {
            baseUrl: "https://api.anthropic.com",
            apiKey: "${ANTHROPIC_API_KEY}",
            api: "anthropic-messages",
            models: [
              {
                id: "claude-sonnet-4-6",
                name: "Claude Sonnet 4.6",
                reasoning: true,
                input: ["text"],
                contextWindow: 200000,
                maxTokens: 64000,
              },
            ],
          },
        },
      },
      null,
      2,
    ),
  );
  await sandbox.files.write("/home/user/.openclaw/workspace/IDENTITY.md", identityMarkdown());
  await sandbox.files.write("/home/user/.openclaw/workspace/SOUL.md", soulMarkdown());
  await sandbox.files.write("/home/user/.openclaw/workspace/AGENTS.md", agentsMarkdown());
  await sandbox.files.write("/home/user/.openclaw/workspace/USER.md", userMarkdown());
  await sandbox.commands.run(`rm -f /home/user/.openclaw/workspace/BOOTSTRAP.md`, {
    requestTimeoutMs: 60_000,
    envs: baseEnvs,
  });
  const commands = [
    `openclaw config set agents.defaults.model.primary ${shellQuote(envelope.model)}`,
    `openclaw config set agents.defaults.models.${shellQuote(envelope.model)}.alias ${shellQuote("default")}`,
    `openclaw config set gateway.http.endpoints.chatCompletions.enabled true`,
    `openclaw config set gateway.http.endpoints.responses.enabled true`,
    `openclaw config set gateway.controlUi.allowInsecureAuth true`,
    `openclaw config set gateway.controlUi.dangerouslyDisableDeviceAuth true`,
  ];
  for (const command of commands) {
    await sandbox.commands.run(command, { requestTimeoutMs: 60_000, envs: baseEnvs });
  }

  const fingerprint = gatewayFingerprint(envelope, baseEnvs);
  const readyBeforeStart = await isGatewayReady(sandbox, envelope, baseEnvs);
  const currentFingerprint = await readGatewayFingerprint(sandbox, baseEnvs);
  if (readyBeforeStart && currentFingerprint === fingerprint) return;

  await sandbox.commands.run(
    `bash -lc ${shellQuote(
      `for p in "[o]penclaw gateway" "[o]penclaw-gateway"; do for pid in $(pgrep -f "$p" || true); do kill "$pid" >/dev/null 2>&1 || true; done; done`,
    )}`,
    { requestTimeoutMs: 60_000, envs: baseEnvs },
  );
  await sleep(1000);
  await sandbox.commands.run(
    `openclaw gateway --allow-unconfigured --bind lan --auth token --port ${envelope.gatewayPort}`,
    { background: true, requestTimeoutMs: 60_000, envs: gatewayEnvs },
  );
  for (let i = 0; i < 60; i++) {
    if (await isGatewayReady(sandbox, envelope, baseEnvs)) {
      await sandbox.files.write(gatewayFingerprintPath, `${fingerprint}\n`);
      return;
    }
    await sleep(1000);
  }
  throw new Error("runtime gateway did not become ready");
}

async function readGatewayFingerprint(sandbox: Sandbox, envs: Record<string, string>) {
  const output = await sandbox.commands.run(
    `bash -lc ${shellQuote(`test -f ${shellQuote(gatewayFingerprintPath)} && cat ${shellQuote(gatewayFingerprintPath)} || true`)}`,
    { requestTimeoutMs: 20_000, envs },
  );
  return output.stdout.trim();
}

function gatewayFingerprint(envelope: Envelope, envs: Record<string, string>) {
  return createHash("sha256")
    .update(JSON.stringify({
      model: envelope.model,
      gatewayPort: envelope.gatewayPort,
      gatewayToken: envelope.gatewayToken,
      anthropicKeyHash: createHash("sha256").update(envs.ANTHROPIC_API_KEY ?? "").digest("hex"),
      version: 2,
    }))
    .digest("hex");
}

async function isGatewayReady(sandbox: Sandbox, envelope: Envelope, envs: Record<string, string>) {
  const probe = await sandbox.commands.run(
    `bash -lc ${shellQuote(`ss -ltn | grep -q ":${envelope.gatewayPort} " && echo ready || echo waiting`)}`,
    { requestTimeoutMs: 20_000, envs },
  );
  return probe.stdout.trim() === "ready";
}

function normalizeText(text: string) {
  return text
    .replace(/\r\n/g, "\n")
    .split("\n")
    .map((line) => line.replace(/[ \t]+$/g, ""))
    .join("\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

function runtimeSystemPrompt() {
  return [
    "You are E2B OpenClaw, an agent runtime inside an E2B sandbox managed by e2b-agents.",
    "The current conversation is arriving through the e2b-agents Slack gateway.",
    "When asked what channel or gateway is being tested, answer Slack or Slack gateway, never webchat.",
    "Keep replies concise and accurate. Use Slack-safe formatting with no decorative emoji, no malformed Markdown, and no repeated blank lines.",
    "Use bullets only when the user asks for a list or checklist.",
  ].join("\n");
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

function shellQuote(value: string | number | boolean) {
  const text = String(value);
  return `'${text.replace(/'/g, `'\\''`)}'`;
}

function requiredEnv(name: string) {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}

async function readStdin() {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString("utf8");
}

function writeJSON(value: unknown) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

main().catch((error) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`${redact(message)}\n`);
  process.exit(1);
});

function redact(message: string) {
  const secrets = [process.env.E2B_API_KEY, process.env.ANTHROPIC_API_KEY, process.env.OPENCLAW_GATEWAY_TOKEN]
    .filter((value): value is string => Boolean(value && value.length > 8))
    .map((value) => value.trim());
  let out = message;
  for (const secret of secrets) {
    out = out.split(secret).join("[redacted]");
  }
  return out;
}
