import { Sandbox } from "e2b";

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

  const sandbox = input.sandboxId
    ? await Sandbox.connect(input.sandboxId, {
        apiKey: requiredEnv("E2B_API_KEY"),
        requestTimeoutMs: 60_000,
      })
    : await Sandbox.create(input.templateId, {
        apiKey: requiredEnv("E2B_API_KEY"),
        timeoutMs: envelope.sandboxTimeoutMs || 3_600_000,
        requestTimeoutMs: 120_000,
        envs,
        metadata: input.metadata ?? {},
      });

  await configureGateway(sandbox, envelope);
  const host = sandbox.getHost(envelope.gatewayPort);
  return {
    sandboxId: sandbox.sandboxId,
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

async function configureGateway(sandbox: Sandbox, envelope: Envelope) {
  const commandEnvs = {
    ANTHROPIC_API_KEY: requiredEnv("ANTHROPIC_API_KEY"),
    OPENCLAW_GATEWAY_TOKEN: envelope.gatewayToken,
  };
  await sandbox.commands.run(`mkdir -p /home/user/.openclaw/agents/main/agent`, {
    requestTimeoutMs: 60_000,
    envs: commandEnvs,
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
  const commands = [
    `openclaw config set agents.defaults.model.primary ${shellQuote(envelope.model)}`,
    `openclaw config set agents.defaults.models.${shellQuote(envelope.model)}.alias ${shellQuote("default")}`,
    `openclaw config set gateway.http.endpoints.chatCompletions.enabled true`,
    `openclaw config set gateway.http.endpoints.responses.enabled true`,
    `openclaw config set gateway.controlUi.allowInsecureAuth true`,
    `openclaw config set gateway.controlUi.dangerouslyDisableDeviceAuth true`,
  ];
  for (const command of commands) {
    await sandbox.commands.run(command, { requestTimeoutMs: 60_000, envs: commandEnvs });
  }
  await sandbox.commands.run(
    `bash -lc ${shellQuote(
      `for p in "[o]penclaw gateway" "[o]penclaw-gateway"; do for pid in $(pgrep -f "$p" || true); do kill "$pid" >/dev/null 2>&1 || true; done; done`,
    )}`,
    { requestTimeoutMs: 60_000, envs: commandEnvs },
  );
  await sleep(1000);
  await sandbox.commands.run(
    `openclaw gateway --allow-unconfigured --bind lan --auth token --token ${shellQuote(
      envelope.gatewayToken,
    )} --port ${envelope.gatewayPort}`,
    { background: true, requestTimeoutMs: 60_000, envs: commandEnvs },
  );
  for (let i = 0; i < 60; i++) {
    const probe = await sandbox.commands.run(
      `bash -lc ${shellQuote(`ss -ltn | grep -q ":${envelope.gatewayPort} " && echo ready || echo waiting`)}`,
      { requestTimeoutMs: 20_000, envs: commandEnvs },
    );
    if (probe.stdout.trim() === "ready") return;
    await sleep(1000);
  }
  throw new Error("runtime gateway did not become ready");
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
