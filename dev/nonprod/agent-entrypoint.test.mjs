import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { buildPiArguments, installMcpConfig } from "./agent-entrypoint.mjs";

const nonprodDirectory = fileURLToPath(new URL(".", import.meta.url));

test("loads the channel and MCP extensions with only bounded tools active", () => {
	const args = buildPiArguments("provider/model");

	assert.deepEqual(
		args.filter((_value, index) => args[index - 1] === "--extension"),
		[
			"/opt/channels/extensions/index.ts",
			"/usr/local/lib/node_modules/pi-mcp-adapter/index.ts",
		],
	);
	assert.equal(
		args[args.indexOf("--tools") + 1],
		"channel_read,channel_respond,mcp",
	);
	assert.equal(args[args.indexOf("--model") + 1], "provider/model");
	assert.equal(
		args[args.indexOf("--skill") + 1],
		"/opt/link/slack-grafana-responder/SKILL.md",
	);
	assert.equal(
		args[args.indexOf("--append-system-prompt") + 1],
		"/opt/link/slack-grafana-system-prompt.md",
	);
});

test("copies managed MCP configuration into the persistent Pi agent directory", () => {
	const home = mkdtempSync(join(tmpdir(), "nonprod-bot-home-"));
	const source = join(home, "source-mcp.json");
	writeFileSync(source, '{"mcpServers":{"grafana":{}}}\n');

	const target = installMcpConfig({
		HOME: home,
		LINK_MCP_CONFIG_SOURCE: source,
	});

	assert.equal(target, join(home, ".pi", "agent", "mcp.json"));
	assert.equal(readFileSync(target, "utf8"), '{"mcpServers":{"grafana":{}}}\n');
});

test("Grafana MCP is internal, read-only, and bounded to investigation tools", () => {
	const config = JSON.parse(
		readFileSync(join(nonprodDirectory, "mcp.json"), "utf8"),
	);
	const grafana = config.mcpServers.grafana;

	assert.deepEqual(Object.keys(config.mcpServers), ["grafana"]);
	assert.equal(
		grafana.url,
		"http://mcp-grafana.unsupervised-singleton.svc.cluster.local:8000/mcp",
	);
	assert.equal(grafana.headers, undefined);
	assert.equal(grafana.lifecycle, "keep-alive");
	assert.deepEqual(grafana.includeTools, [
		"list_datasources",
		"get_datasource",
		"query_prometheus",
		"list_prometheus_metric_names",
		"list_prometheus_label_names",
		"list_prometheus_label_values",
		"query_loki_logs",
		"list_loki_label_names",
		"list_loki_label_values",
		"query_loki_stats",
	]);
	assert.equal(config.settings.directTools, false);
	assert.equal(config.settings.elicitation, false);
	assert.equal(config.settings.outputGuard, true);
	assert.equal(config.settings.sampling, false);
});

test("nonprod prompt requires fresh MCP evidence before reporting authorization errors", () => {
	const prompt = readFileSync(
		join(nonprodDirectory, "slack-grafana-system-prompt.md"),
		"utf8",
	);

	assert.match(prompt, /Make a fresh MCP call for every request/);
	assert.match(
		prompt,
		/never claim that Grafana is\s+unauthorized unless the current MCP tool result/i,
	);
});

test("nonprod responder requires the exact Grafana MCP probe before replying", () => {
	const skill = readFileSync(
		join(nonprodDirectory, "slack-grafana-responder", "SKILL.md"),
		"utf8",
	);

	assert.match(
		skill,
		/mcp\(\{ server: "grafana", tool: "list_datasources", args: \{\} \}\)/,
	);
	assert.match(skill, /call `mcp` before drafting the response/);
	assert.match(skill, /never reuse an error stated in an earlier Slack reply/);
});
