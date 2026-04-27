import { Sandbox } from "e2b";
import { defaultRuntimeModel, openClawConfig, runtimePaths } from "./templateFiles.js";

type Envelope = {
  command: "ensure";
  input: unknown;
  model: string;
  gatewayPort: number;
  adapterPort: number;
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

async function main() {
  const envelope = JSON.parse(await readStdin()) as Envelope;
  if (envelope.command === "ensure") {
    const output = await ensureRuntime(envelope.input as EnsureInput, envelope);
    await writeJSON(output);
    return;
  }
  throw new Error(`unsupported command: ${String(envelope.command)}`);
}

async function ensureRuntime(input: EnsureInput, envelope: Envelope) {
  const totalStart = Date.now();
  if (!input.templateId) throw new Error("templateId is required");
  if (!input.sessionKey) throw new Error("sessionKey is required");

  const runtime = await connectOrCreateSandbox(input, envelope);
  const acpHost = runtime.sandbox.getHost(adapterPort(envelope));
  const acpBaseUrl = acpHost.startsWith("http") ? acpHost : `https://${acpHost}`;

  const secretStart = Date.now();
  await writeRuntimeFiles(runtime.sandbox, envelope);
  logTiming("runtime helper secrets injected", {
    durationMs: durationMs(secretStart),
    sandboxId: runtime.sandbox.sandboxId,
    created: runtime.created,
  });

  await activateRuntime(runtime.sandbox, acpBaseUrl, input, envelope, runtime.created);

  const host = runtime.sandbox.getHost(envelope.gatewayPort);
  logTiming("runtime helper ensure completed", {
    durationMs: durationMs(totalStart),
    sandboxId: runtime.sandbox.sandboxId,
    sessionKey: input.sessionKey,
    created: runtime.created,
  });
  return {
    sandboxId: runtime.sandbox.sandboxId,
    templateId: input.templateId,
    host,
    baseUrl: host.startsWith("http") ? host : `https://${host}`,
    acpHost,
    acpBaseUrl,
    sessionKey: input.sessionKey,
  };
}

async function connectOrCreateSandbox(input: EnsureInput, envelope: Envelope) {
  if (input.sandboxId) {
    try {
      const connectStart = Date.now();
      const sandbox = await Sandbox.connect(input.sandboxId, {
        apiKey: requiredEnv("E2B_API_KEY"),
        requestTimeoutMs: 60_000,
      });
      logTiming("runtime helper existing sandbox connect completed", {
        durationMs: durationMs(connectStart),
        sandboxId: input.sandboxId,
      });
      if (!(await sandboxHasTemplateSupervisor(sandbox))) {
        logTiming("runtime helper existing sandbox incompatible", {
          sandboxId: input.sandboxId,
          reason: "missing_template_supervisor",
        });
        return createSandbox(input, envelope);
      }
      const timeoutStart = Date.now();
      await sandbox.setTimeout(envelope.sandboxTimeoutMs || 3_600_000, { requestTimeoutMs: 60_000 });
      logTiming("runtime helper sandbox timeout update completed", {
        durationMs: durationMs(timeoutStart),
        sandboxId: input.sandboxId,
      });
      return { sandbox, created: false };
    } catch (error) {
      if (!isMissingSandboxError(error)) throw error;
      logTiming("runtime helper existing sandbox missing", {
        sandboxId: input.sandboxId,
        error: errorMessage(error),
      });
      return createSandbox(input, envelope);
    }
  }
  return createSandbox(input, envelope);
}

async function sandboxHasTemplateSupervisor(sandbox: Sandbox) {
  try {
    await sandbox.commands.run(
      [
        "bash -lc",
        shellSingleQuote([
          `test -x ${runtimePaths.startScript}`,
          `test -x ${runtimePaths.readyScript}`,
          `test -x ${runtimePaths.acpAdapter}`,
          `test -s ${runtimePaths.config}`,
        ].join(" && ")),
      ].join(" "),
      { requestTimeoutMs: 10_000 },
    );
    return true;
  } catch {
    return false;
  }
}

function isMissingSandboxError(error: unknown) {
  const message = error instanceof Error ? error.message : String(error);
  return /\b(404|410)\b/.test(message) || /not found|expired|does not exist/i.test(message);
}

async function createSandbox(input: EnsureInput, envelope: Envelope) {
  const start = Date.now();
  const sandbox = await Sandbox.create(input.templateId, {
    apiKey: requiredEnv("E2B_API_KEY"),
    timeoutMs: envelope.sandboxTimeoutMs || 3_600_000,
    requestTimeoutMs: 120_000,
    envs: {
      ANTHROPIC_API_KEY: requiredEnv("ANTHROPIC_API_KEY"),
      OPENCLAW_GATEWAY_TOKEN: envelope.gatewayToken,
      E2B_AGENTS_RUNTIME_MODEL: envelope.model,
    },
    metadata: input.metadata ?? {},
  });
  logTiming("runtime helper sandbox create completed", {
    durationMs: durationMs(start),
    sandboxId: sandbox.sandboxId,
    templateId: input.templateId,
  });
  return { sandbox, created: true };
}

async function writeRuntimeFiles(sandbox: Sandbox, envelope: Envelope) {
  await sandbox.files.write(runtimePaths.authToken, `${envelope.gatewayToken}\n`);
  await sandbox.files.write(runtimePaths.runtimeEnv, runtimeEnv(envelope));
  await sandbox.files.write(runtimePaths.config, `${JSON.stringify(openClawConfig(envelope.model), null, 2)}\n`);
  await sandbox.files.write(
    runtimePaths.secrets,
    `${JSON.stringify(
      {
        providers: {
          anthropic: {
            apiKey: requiredEnv("ANTHROPIC_API_KEY"),
          },
        },
      },
      null,
      2,
    )}\n`,
  );
}

function runtimeEnv(envelope: Envelope) {
  return [
    `OPENCLAW_GATEWAY_PORT=${positivePort(envelope.gatewayPort, 18789)}`,
    `E2B_AGENTS_ACP_ADAPTER_PORT=${positivePort(envelope.adapterPort, positivePort(envelope.gatewayPort, 18789) + 1)}`,
    `E2B_AGENTS_RUNTIME_MODEL=${shellSingleQuote(envelope.model || "anthropic/claude-sonnet-4-6")}`,
    "",
  ].join("\n");
}

function positivePort(value: number, fallback: number) {
  return Number.isInteger(value) && value > 0 ? value : fallback;
}

function shellSingleQuote(value: string) {
  return `'${String(value).replace(/'/g, `'\\''`)}'`;
}

async function requestRuntimeRestart(sandbox: Sandbox) {
  await sandbox.commands.run(
    "bash -lc 'for p in \"[o]penclaw gateway\" \"[o]penclaw acp\" \"[a]cp-adapter.mjs\"; do pkill -f \"$p\" >/dev/null 2>&1 || true; done'",
    { requestTimeoutMs: 20_000 },
  );
}

async function activateRuntime(
  sandbox: Sandbox,
  acpBaseUrl: string,
  input: EnsureInput,
  envelope: Envelope,
  created: boolean,
) {
  if (templateModelDiffers(envelope)) {
    logTiming("runtime helper runtime restart required", {
      sandboxId: sandbox.sandboxId,
      created,
      reason: "model_differs_from_template",
      model: envelope.model,
    });
    await restartRuntimeAndWait(sandbox, acpBaseUrl, input, envelope, created);
    return;
  }

  try {
    if (!created) {
      const activeModelStart = Date.now();
      const activeModel = await activeRuntimeModel(sandbox, envelope);
      logTiming("runtime helper active model checked", {
        durationMs: durationMs(activeModelStart),
        sandboxId: sandbox.sandboxId,
        activeModel: activeModel || "",
        requestedModel: requestedModelID(envelope.model),
      });
      if (!activeModel || activeModel !== requestedModelID(envelope.model)) {
        logTiming("runtime helper runtime restart required", {
          sandboxId: sandbox.sandboxId,
          created,
          reason: activeModel ? "active_model_differs_from_requested_model" : "active_model_unknown",
          activeModel: activeModel || "",
          requestedModel: requestedModelID(envelope.model),
        });
        await restartRuntimeAndWait(sandbox, acpBaseUrl, input, envelope, created);
        return;
      }
    }

    const reloadStart = Date.now();
    await reloadRuntimeSecrets(sandbox, envelope);
    logTiming("runtime helper secrets reloaded", {
      durationMs: durationMs(reloadStart),
      sandboxId: sandbox.sandboxId,
      created,
    });

    const liveStart = Date.now();
    await waitForACPAdapterLive(acpBaseUrl);
    logTiming("runtime helper acp live", {
      durationMs: durationMs(liveStart),
      sandboxId: sandbox.sandboxId,
      created,
    });
    return;
  } catch (error) {
    if (!isRuntimeProcessUnavailableError(error)) {
      throw error;
    }
    logTiming("runtime helper snapshotted runtime unavailable", {
      sandboxId: sandbox.sandboxId,
      created,
      error: errorMessage(error),
    });
  }

  await restartRuntimeAndWait(sandbox, acpBaseUrl, input, envelope, created);
}

async function restartRuntimeAndWait(
  sandbox: Sandbox,
  acpBaseUrl: string,
  input: EnsureInput,
  envelope: Envelope,
  created: boolean,
) {
  const restartStart = Date.now();
  await requestRuntimeRestart(sandbox);
  logTiming("runtime helper runtime restart requested", {
    durationMs: durationMs(restartStart),
    sandboxId: sandbox.sandboxId,
    created,
    recovery: true,
  });

  const readyStart = Date.now();
  await waitForACPAdapterReady(acpBaseUrl, input.sessionKey, envelope);
  logTiming("runtime helper acp ready after restart", {
    durationMs: durationMs(readyStart),
    sandboxId: sandbox.sandboxId,
    sessionKey: input.sessionKey,
    created,
    recovery: true,
  });
}

function templateModelDiffers(envelope: Envelope) {
  return requestedModelID(envelope.model) !== requestedModelID(defaultRuntimeModel);
}

function requestedModelID(model: string) {
  const normalized = (model || defaultRuntimeModel).trim() || defaultRuntimeModel;
  return normalized.startsWith("anthropic/") ? normalized.slice("anthropic/".length) : normalized;
}

async function activeRuntimeModel(sandbox: Sandbox, envelope: Envelope) {
  const result = await runGatewayCommand(sandbox, envelope, "status");
  const parsed = JSON.parse(result.stdout) as {
    sessions?: {
      defaults?: {
        model?: unknown;
      };
    };
  };
  const model = parsed.sessions?.defaults?.model;
  return typeof model === "string" && model.trim() ? requestedModelID(model) : "";
}

async function reloadRuntimeSecrets(sandbox: Sandbox, envelope: Envelope) {
  await runGatewayCommand(sandbox, envelope, "secrets.reload");
}

async function runGatewayCommand(sandbox: Sandbox, envelope: Envelope, method: string) {
  const gatewayPort = positivePort(envelope.gatewayPort, 18789);
  const script = [
    "export HOME=/home/user",
    `export OPENCLAW_STATE_DIR=${runtimePaths.stateDir}`,
    `export OPENCLAW_CONFIG_PATH=${runtimePaths.config}`,
    `export OPENCLAW_GATEWAY_TOKEN=${shellSingleQuote(envelope.gatewayToken)}`,
    `openclaw gateway call ${method} --url ws://127.0.0.1:${gatewayPort} --token "$OPENCLAW_GATEWAY_TOKEN" --timeout 10000 --json`,
  ].join(" && ");
  return await sandbox.commands.run(["bash -lc", shellSingleQuote(script)].join(" "), {
    requestTimeoutMs: 20_000,
  });
}

async function waitForACPAdapterLive(baseURL: string) {
  const deadline = Date.now() + 10_000;
  let lastError = "";
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseURL.replace(/\/+$/, "")}/healthz`);
      if (response.ok) {
        return;
      }
      lastError = `HTTP ${response.status}: ${await response.text()}`;
    } catch (error) {
      lastError = errorMessage(error);
    }
    await sleep(250);
  }
  throw new Error(`runtime ACP adapter is not live: ${redact(lastError)}`);
}

async function waitForACPAdapterReady(baseURL: string, sessionKey: string, envelope: Envelope) {
  const deadline = Date.now() + 90_000;
  let lastError = "";
  while (Date.now() < deadline) {
    try {
      const url = new URL(`${baseURL.replace(/\/+$/, "")}/healthz`);
      url.searchParams.set("ready", "1");
      url.searchParams.set("sessionKey", sessionKey);
      const response = await fetch(url, {
        headers: {
          authorization: `Bearer ${envelope.gatewayToken}`,
        },
      });
      if (response.ok) {
        return;
      }
      lastError = `HTTP ${response.status}: ${await response.text()}`;
    } catch (error) {
      lastError = errorMessage(error);
    }
    await sleep(1000);
  }
  throw new Error(`runtime ACP adapter did not become ready: ${redact(lastError)}`);
}

function adapterPort(envelope: Envelope) {
  return envelope.adapterPort > 0 ? envelope.adapterPort : envelope.gatewayPort + 1;
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

function requiredEnv(name: string) {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function durationMs(startMs: number) {
  return Date.now() - startMs;
}

function errorMessage(error: unknown) {
  return redact(error instanceof Error ? error.message : String(error));
}

function isRuntimeProcessUnavailableError(error: unknown) {
  const message = errorMessage(error).toLowerCase();
  return [
    "connection refused",
    "connection reset",
    "connection aborted",
    "econnrefused",
    "econnreset",
    "etimedout",
    "timed out",
    "timeout",
    "gateway is closed",
    "websocket",
    "fetch failed",
    "failed to fetch",
    "runtime acp adapter is not live",
    "http 502",
    "http 503",
    "404",
    "410",
  ].some((pattern) => message.includes(pattern));
}

function logTiming(msg: string, fields: Record<string, unknown>) {
  process.stderr.write(`${JSON.stringify({ msg, ...fields })}\n`);
}

async function readStdin() {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString("utf8");
}

function writeJSON(value: unknown) {
  return new Promise<void>((resolve, reject) => {
    process.stdout.write(`${JSON.stringify(value)}\n`, (error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });
}

main().then(() => {
  process.exit(0);
}).catch((error) => {
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
