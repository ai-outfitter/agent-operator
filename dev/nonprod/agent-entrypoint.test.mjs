import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { buildPiArguments, installMcpConfig } from "./agent-entrypoint.mjs";
import { buildGrafanaSecretManifest } from "./grafana-secret-sync.mjs";

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
});

test("fails closed when Grafana authorization is absent", () => {
	assert.throws(
		() => installMcpConfig({ HOME: "/workspace" }),
		/MCP_GRAFANA_BASIC_AUTH_HEADER must be configured/,
	);
});

test("copies managed MCP configuration into the persistent Pi agent directory", () => {
	const home = mkdtempSync(join(tmpdir(), "nonprod-bot-home-"));
	const source = join(home, "source-mcp.json");
	writeFileSync(source, '{"mcpServers":{"grafana":{}}}\n');

	const target = installMcpConfig({
		HOME: home,
		LINK_MCP_CONFIG_SOURCE: source,
		MCP_GRAFANA_BASIC_AUTH_HEADER: "Basic redacted",
	});

	assert.equal(target, join(home, ".pi", "agent", "mcp.json"));
	assert.equal(readFileSync(target, "utf8"), '{"mcpServers":{"grafana":{}}}\n');
});

test("Grafana MCP is authenticated, read-only, and bounded to investigation tools", () => {
	const config = JSON.parse(
		readFileSync(join(nonprodDirectory, "mcp.json"), "utf8"),
	);
	const grafana = config.mcpServers.grafana;

	assert.deepEqual(Object.keys(config.mcpServers), ["grafana"]);
	assert.equal(
		grafana.headers.Authorization,
		"$" + "{MCP_GRAFANA_BASIC_AUTH_HEADER}",
	);
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

test("Grafana credential sync is scoped to the bot namespace and expected key", () => {
	const manifest = JSON.parse(
		buildGrafanaSecretManifest({
			context: "unsup-nonprod-engineer",
			namespace: "agent-nonprod-bot",
			authorization: "Basic test-only",
		}),
	);

	assert.equal(manifest.metadata.name, "nonprod-bot-grafana");
	assert.equal(manifest.metadata.namespace, "agent-nonprod-bot");
	assert.deepEqual(Object.keys(manifest.stringData), [
		"MCP_GRAFANA_BASIC_AUTH_HEADER",
	]);
	assert.throws(
		() =>
			buildGrafanaSecretManifest({
				context: "another-context",
				namespace: "agent-nonprod-bot",
				authorization: "Basic test-only",
			}),
		/LINK_KUBE_CONTEXT must be unsup-nonprod-engineer/,
	);
});
