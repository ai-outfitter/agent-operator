import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  createMcpExtension,
  DefaultResourceLoader,
  SettingsManager,
} from "@earendil-works/pi-coding-agent";

// Exercise the installed image's SDK and extension loader without credentials,
// providers, or a resident's home. A package version alone is insufficient:
// older Channels artifacts share a version with the native-MCP source commit.
const root = mkdtempSync(join(tmpdir(), "resident-native-mcp-"));
try {
  assert.equal(typeof createMcpExtension, "function");
  const channels = fileURLToPath(import.meta.resolve("@ai-outfitter/channels"));
  // Use Pi's TypeScript extension loader, as task modules live in node_modules.
  const taskMcp = import.meta.resolve("@ai-outfitter/channels/extensions/task-plane/task-mcp.ts");
  const probe = join(root, "task-mcp-probe.ts");
  writeFileSync(probe, `
    import assert from "node:assert/strict";
    import { taskSessionMcpExtensions } from ${JSON.stringify(taskMcp)};
    export default function () {
      const factories = taskSessionMcpExtensions("image-smoke", undefined, ${JSON.stringify(root)});
      assert.deepEqual(factories.map(({ name }) => name), ["codemode", "tool-search", "mcp"]);
      assert.equal(factories.find(({ name }) => name === "mcp").replaceable, false);
    }
  `);

  // Check Pi 1.4.0's trust rule independently of the Channels session adapter.
  mkdirSync(join(root, ".pi"));
  writeFileSync(join(root, ".pi", "mcp.json"), JSON.stringify({
    mcpServers: { repositoryCommand: { command: "must-not-execute" } },
  }));
  const entry = import.meta.resolve("@earendil-works/pi-coding-agent");
  // Internal Pi 1.4.0 helper layout, also used by the pinned Channels adapter.
  const { loadMcpConfig } = await import(new URL("./extensions/mcp/config.js", entry));
  assert.deepEqual(loadMcpConfig({ agentDir: root, cwd: root, projectTrusted: false }).servers, []);
  assert.equal(loadMcpConfig({ agentDir: root, cwd: root, projectTrusted: true }).servers.length, 1);

  const loader = new DefaultResourceLoader({
    cwd: root,
    agentDir: root,
    settingsManager: SettingsManager.inMemory({ packages: [] }),
    additionalExtensionPaths: [channels, probe],
    noSkills: true,
    noPromptTemplates: true,
    noThemes: true,
  });
  await loader.reload();
  const loaded = loader.getExtensions();
  assert.deepEqual(loaded.errors, []);
  assert.ok(loaded.extensions.some((extension) => extension.resolvedPath === channels));
  assert.ok(loaded.extensions.some((extension) => extension.resolvedPath === probe));
  console.log("Native Pi MCP task factories, Pi trust rule, and Channels extension load successfully");
} finally {
  rmSync(root, { recursive: true, force: true });
}
