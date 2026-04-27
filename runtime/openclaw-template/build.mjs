#!/usr/bin/env node
import { mkdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { Template, defaultBuildLogger } from "e2b";
import { acpBridgeScript } from "../e2b-helper/dist/acpBridgeScript.js";
import { runtimePaths, templateAssetFiles } from "../e2b-helper/dist/templateFiles.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const contextDir = path.join(root, "tmp_e2b_openclaw_template_context");
const templateName = process.env.E2B_AGENTS_TEMPLATE_NAME || "openclaw";
const tag = process.env.E2B_AGENTS_TEMPLATE_TAG || "";
const buildName = tag ? `${templateName}:${tag}` : templateName;

await rm(contextDir, { recursive: true, force: true });
await mkdir(contextDir, { recursive: true });

for (const [relativePath, content] of Object.entries(templateAssetFiles())) {
  const target = path.join(contextDir, relativePath);
  await mkdir(path.dirname(target), { recursive: true });
  await writeFile(target, content, "utf8");
}
await writeFile(path.join(contextDir, "acp-adapter.mjs"), acpBridgeScript(), "utf8");

const template = Template({ fileContextPath: contextDir })
  .fromNodeImage("22")
  .aptInstall(["curl", "git"])
  .npmInstall("openclaw", { g: true })
  .makeDir([
    "/home/user/.e2b-agents/auth",
    "/home/user/.e2b-agents/secrets",
    "/home/user/.e2b-agents/logs",
    runtimePaths.workspace,
  ])
  .runCmd(`openclaw setup --workspace ${runtimePaths.workspace}`)
  .remove(`${runtimePaths.workspace}/BOOTSTRAP.md`, { force: true })
  .copy("openclaw.json", runtimePaths.config, { mode: 0o644 })
  .copy("workspace", runtimePaths.workspace, { mode: 0o644 })
  .copy("acp-adapter.mjs", runtimePaths.acpAdapter, { mode: 0o755 })
  .copy("start-runtime.sh", "/home/user/.e2b-agents/start-runtime.sh", { mode: 0o755 })
  .copy("ready-runtime.sh", "/home/user/.e2b-agents/ready-runtime.sh", { mode: 0o755 })
  .runCmd([
    "chmod 700 /home/user/.e2b-agents/auth /home/user/.e2b-agents/secrets",
    "chmod 755 /home/user/.e2b-agents/start-runtime.sh /home/user/.e2b-agents/ready-runtime.sh /home/user/.e2b-agents/acp-adapter.mjs",
  ])
  .setStartCmd(
    "/home/user/.e2b-agents/start-runtime.sh",
    "/home/user/.e2b-agents/ready-runtime.sh",
  );

const info = await Template.build(template, buildName, {
  apiKey: process.env.E2B_API_KEY,
  cpuCount: Number(process.env.E2B_AGENTS_TEMPLATE_CPU || "2"),
  memoryMB: Number(process.env.E2B_AGENTS_TEMPLATE_MEMORY_MB || "4096"),
  skipCache: process.env.E2B_AGENTS_TEMPLATE_SKIP_CACHE === "1",
  onBuildLogs: defaultBuildLogger({ minLevel: "info" }),
});

console.log(JSON.stringify(info, null, 2));
