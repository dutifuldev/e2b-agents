export function acpBridgeScript() {
  return String.raw`#!/usr/bin/env node
import http from "node:http";
import { spawn } from "node:child_process";
import readline from "node:readline";
import { readFile, writeFile } from "node:fs/promises";

const port = Number(process.env.E2B_AGENTS_ACP_ADAPTER_PORT || "18790");
const token = String(process.env.E2B_AGENTS_ACP_AUTH_TOKEN || "");
const cwd = String(process.env.E2B_AGENTS_ACP_CWD || process.cwd());
const sessionStorePath = String(process.env.E2B_AGENTS_ACP_SESSION_STORE || "/home/user/.e2b-agents/acp-sessions.json");
const command = JSON.parse(process.env.E2B_AGENTS_ACP_COMMAND_JSON || '["openclaw","acp"]');
const requestTimeoutMs = Number(process.env.E2B_AGENTS_ACP_REQUEST_TIMEOUT_MS || "300000");
const protocolVersion = Number(process.env.E2B_AGENTS_ACP_PROTOCOL_VERSION || "1");
const runtimeSessionKeyPrefix = String(process.env.E2B_AGENTS_ACP_RUNTIME_SESSION_KEY_PREFIX || "");

if (!Array.isArray(command) || command.length === 0 || typeof command[0] !== "string") {
  throw new Error("E2B_AGENTS_ACP_COMMAND_JSON must be a non-empty JSON string array");
}

let child = null;
let rl = null;
let nextID = 1;
let initialized = false;
let initializing = null;
let agentCapabilities = {};
const pending = new Map();
const sessions = new Map();
const sessionStore = new Map();
const promptBuffers = new Map();
const sessionQueues = new Map();
let sessionStoreLoaded = false;
let sessionStoreLoadPromise = null;
let sessionStoreWritePromise = Promise.resolve();

function log(msg, fields = {}) {
  process.stderr.write(JSON.stringify({ msg, ...fields }) + "\n");
}

function normalizeText(text) {
  return String(text || "")
    .replace(/\r\n/g, "\n")
    .split("\n")
    .map((line) => line.replace(/[ \t]+$/g, ""))
    .join("\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

function startAgent() {
  if (child && !child.killed && child.exitCode === null && child.signalCode === null) return;
  const [bin, ...args] = command;
  log("acp bridge starting harness", { command: bin, args: redactCommandArgs(args) });
  child = spawn(bin, args, {
    cwd,
    env: process.env,
    stdio: ["pipe", "pipe", "pipe"],
  });
  child.on("error", (err) => {
    log("acp harness spawn failed", { error: err.message });
    clearAgentState(new Error("ACP harness spawn failed: " + err.message));
  });
  child.stdin.on("error", (err) => {
    log("acp harness stdin failed", { error: err.message });
    clearAgentState(new Error("ACP harness stdin failed: " + err.message));
  });
  child.stderr.on("data", (chunk) => {
    const text = String(chunk).trim();
    if (text) log("acp harness stderr", { text: text.slice(0, 1000) });
  });
  child.on("exit", (code, signal) => {
    log("acp harness exited", { code, signal });
    clearAgentState(new Error("ACP harness exited (" + (code ?? signal ?? "unknown") + ")"));
  });
  rl = readline.createInterface({ input: child.stdout });
  rl.on("line", handleLine);
}

function clearAgentState(err) {
  initialized = false;
  initializing = null;
  child = null;
  rl = null;
  sessions.clear();
  for (const entry of pending.values()) {
    clearTimeout(entry.timeout);
    entry.reject(err);
  }
  pending.clear();
  promptBuffers.clear();
}

function handleLine(line) {
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    log("acp bridge received non-json stdout", { line: line.slice(0, 1000) });
    return;
  }
  if (Object.prototype.hasOwnProperty.call(msg, "id") && (Object.prototype.hasOwnProperty.call(msg, "result") || Object.prototype.hasOwnProperty.call(msg, "error"))) {
    const entry = pending.get(msg.id);
    if (!entry) return;
    pending.delete(msg.id);
    clearTimeout(entry.timeout);
    if (msg.error) {
      entry.reject(new Error(msg.error.message || JSON.stringify(msg.error)));
    } else {
      entry.resolve(msg.result);
    }
    return;
  }
  if (msg.method === "session/update") {
    const params = msg.params || {};
    const update = params.update || {};
    if (update.sessionUpdate === "agent_message_chunk" && update.content?.type === "text") {
      const current = promptBuffers.get(params.sessionId) || "";
      promptBuffers.set(params.sessionId, current + update.content.text);
    }
    return;
  }
  if (msg.method) {
    handleAgentRequest(msg);
  }
}

function handleAgentRequest(msg) {
  if (!Object.prototype.hasOwnProperty.call(msg, "id")) return;
  const response = {
    jsonrpc: "2.0",
    id: msg.id,
    error: {
      code: -32601,
      message: "Unsupported client method: " + msg.method,
    },
  };
  child.stdin.write(JSON.stringify(response) + "\n");
}

function request(method, params) {
  startAgent();
  if (!child?.stdin?.writable) {
    return Promise.reject(new Error("ACP harness stdin is unavailable"));
  }
  const id = nextID++;
  const payload = { jsonrpc: "2.0", id, method, params };
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      if (!pending.has(id)) return;
      const err = new Error("ACP request timed out: " + method);
      const timedOutChild = child;
      clearAgentState(err);
      if (timedOutChild && !timedOutChild.killed) {
        timedOutChild.kill("SIGTERM");
      }
    }, requestTimeoutMs);
    pending.set(id, { resolve, reject, timeout });
    child.stdin.write(JSON.stringify(payload) + "\n");
  });
}

async function initialize() {
  if (initialized) return;
  if (initializing) return initializing;
  initializing = (async () => {
    const start = Date.now();
    const result = await request("initialize", {
      protocolVersion,
      clientCapabilities: {},
      clientInfo: {
        name: "e2b-agents",
        title: "e2b-agents",
        version: "1.0.0",
      },
    });
    initialized = true;
    agentCapabilities = result?.agentCapabilities || {};
    log("acp bridge initialize completed", { durationMs: Date.now() - start });
  })();
  try {
    await initializing;
  } finally {
    initializing = null;
  }
}

async function sessionForKey(sessionKey) {
  await initialize();
  const cached = sessions.get(sessionKey);
  if (cached) return cached;
  await loadSessionStore();
  const stored = sessionStore.get(sessionKey);
  if (stored && await restoreStoredSession(sessionKey, stored)) {
    sessions.set(sessionKey, stored);
    return stored;
  }
  if (stored) {
    sessionStore.delete(sessionKey);
    await persistSessionStore();
  }
  const start = Date.now();
  const agentSessionKey = runtimeSessionKey(sessionKey);
  const result = await request("session/new", {
    cwd,
    mcpServers: [],
    _meta: { sessionKey: agentSessionKey },
  });
  const sessionId = result?.sessionId;
  if (!sessionId) throw new Error("ACP session/new did not return sessionId");
  sessions.set(sessionKey, sessionId);
  sessionStore.set(sessionKey, sessionId);
  await persistSessionStore();
  log("acp bridge session created", { sessionKey, sessionId, durationMs: Date.now() - start });
  return sessionId;
}

async function restoreStoredSession(sessionKey, sessionId) {
  const params = { sessionId, cwd, mcpServers: [], _meta: { sessionKey: runtimeSessionKey(sessionKey) } };
  const start = Date.now();
  try {
    if (agentCapabilities?.loadSession) {
      await request("session/load", params);
      log("acp bridge session loaded", { sessionKey, sessionId, durationMs: Date.now() - start });
      return true;
    }
    if (agentCapabilities?.sessionCapabilities?.resume) {
      await request("session/resume", params);
      log("acp bridge session resumed", { sessionKey, sessionId, durationMs: Date.now() - start });
      return true;
    }
  } catch (error) {
    log("acp bridge stored session restore failed", { sessionKey, sessionId, error: String(error?.message || error).slice(0, 1000) });
  }
  return false;
}

function runtimeSessionKey(sessionKey) {
  const key = String(sessionKey || "").trim();
  if (!runtimeSessionKeyPrefix) return key;
  const normalized = key.toLowerCase();
  return runtimeSessionKeyPrefix + normalized;
}

async function loadSessionStore() {
  if (sessionStoreLoaded) return;
  if (sessionStoreLoadPromise) return sessionStoreLoadPromise;
  sessionStoreLoadPromise = (async () => {
    try {
      const raw = await readFile(sessionStorePath, "utf8");
      const data = JSON.parse(raw);
      for (const [key, value] of Object.entries(data)) {
        if (typeof key === "string" && typeof value === "string" && key && value) {
          sessionStore.set(key, value);
        }
      }
    } catch (error) {
      if (error?.code !== "ENOENT") {
        log("acp bridge session store read failed", { error: String(error?.message || error).slice(0, 1000) });
      }
    } finally {
      sessionStoreLoaded = true;
      sessionStoreLoadPromise = null;
    }
  })();
  return sessionStoreLoadPromise;
}

async function persistSessionStore() {
  sessionStoreWritePromise = sessionStoreWritePromise
    .catch(() => {})
    .then(() => writeFile(sessionStorePath, JSON.stringify(Object.fromEntries(sessionStore), null, 2) + "\n"));
  return sessionStoreWritePromise;
}

function enqueue(sessionKey, task) {
  const previous = sessionQueues.get(sessionKey) || Promise.resolve();
  const next = previous.then(task, task);
  sessionQueues.set(sessionKey, next.catch(() => {}));
  return next;
}

async function prompt(sessionKey, text) {
  return enqueue(sessionKey, async () => {
    const totalStart = Date.now();
    const sessionId = await sessionForKey(sessionKey);
    promptBuffers.set(sessionId, "");
    const promptStart = Date.now();
    const result = await request("session/prompt", {
      sessionId,
      prompt: [{ type: "text", text }],
    });
    const reply = normalizeText(promptBuffers.get(sessionId) || "");
    promptBuffers.delete(sessionId);
    if (!reply) throw new Error("ACP prompt completed without assistant text");
    log("acp bridge prompt completed", {
      sessionKey,
      sessionId,
      stopReason: result?.stopReason,
      promptDurationMs: Date.now() - promptStart,
      totalDurationMs: Date.now() - totalStart,
      replyBytes: Buffer.byteLength(reply),
    });
    return { text: reply, sessionKey, acpSessionId: sessionId };
  });
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on("data", (chunk) => {
      size += chunk.length;
      if (size > 1024 * 1024) {
        reject(new Error("request body too large"));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

function authorized(req) {
  if (!token) return true;
  return req.headers.authorization === "Bearer " + token;
}

const server = http.createServer(async (req, res) => {
  try {
    const url = new URL(req.url || "/", "http://127.0.0.1");
    if (url.pathname === "/healthz") {
      if (url.searchParams.get("ready") === "1") {
        if (!authorized(req)) {
          res.writeHead(401, { "content-type": "application/json" });
          res.end(JSON.stringify({ error: "unauthorized" }));
          return;
        }
        await initialize();
      }
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ ok: true, initialized, sessions: sessions.size }));
      return;
    }
    if (url.pathname !== "/prompt" || req.method !== "POST") {
      res.writeHead(404, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: "not found" }));
      return;
    }
    if (!authorized(req)) {
      res.writeHead(401, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: "unauthorized" }));
      return;
    }
    const body = JSON.parse(await readBody(req));
    const sessionKey = String(body.sessionKey || "").trim();
    const text = String(body.prompt || "").trim();
    if (!sessionKey || !text) {
      res.writeHead(400, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: "sessionKey and prompt are required" }));
      return;
    }
    const output = await prompt(sessionKey, text);
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify(output));
  } catch (error) {
    const message = String(error?.message || error);
    const unavailable = message.includes("ACP harness exited") ||
      message.includes("ACP harness spawn failed") ||
      message.includes("ACP harness stdin failed") ||
      message.includes("ACP harness stdin is unavailable") ||
      message.includes("ACP request timed out");
    log("acp bridge request failed", { error: message.slice(0, 1000), unavailable });
    res.writeHead(unavailable ? 503 : 500, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: message }));
  }
});

server.listen(port, "0.0.0.0", () => {
  log("acp bridge listening", { port, cwd, command: redactCommandArgs(command) });
});

process.on("SIGTERM", () => {
  server.close();
  child?.kill();
  process.exit(0);
});

function redactCommandArgs(args) {
  return args.map((arg, index) => {
    if (index > 0 && args[index - 1] === "--token") return "[redacted]";
    return arg;
  });
}
`;
}
