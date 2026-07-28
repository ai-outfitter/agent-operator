import assert from "node:assert/strict";
import {
	mkdtempSync,
	readFileSync,
	rmSync,
	statSync,
	writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
	buildPiArguments,
	installMcpConfig,
	installRelayCredentials,
} from "./agent-entrypoint.mjs";

const nonprodDirectory = fileURLToPath(new URL(".", import.meta.url));

test("loads the channel and MCP extensions with only bounded tools active", () => {
	const args = buildPiArguments("provider/model");

	assert.deepEqual(
		args.filter((_value, index) => args[index - 1] === "--extension"),
		[
			"/opt/channels/extensions/relay-extension.ts",
			"/opt/channels/extensions/index.ts",
			"/usr/local/lib/node_modules/pi-mcp-adapter/index.ts",
		],
	);
	assert.equal(args[args.indexOf("--session-id") + 1], "nonprod-bot");
	assert.equal(args.includes("--no-session"), false);
	assert.equal(
		args[args.indexOf("--tools") + 1],
		"channel_read,channel_respond,mcp,grafana_list_datasources,grafana_get_datasource,grafana_query_prometheus,grafana_list_prometheus_metric_names,grafana_list_prometheus_label_names,grafana_list_prometheus_label_values,grafana_query_loki_logs,grafana_list_loki_label_names,grafana_list_loki_label_values,grafana_query_loki_stats",
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
	rmSync(home, { recursive: true, force: true });
});

test("writes a permission-restricted relay credential document", () => {
	const home = mkdtempSync(join(tmpdir(), "nonprod-bot-relay-"));
	const target = join(home, ".channels", "relay", "credentials.json");
	try {
		assert.equal(
			installRelayCredentials({
				AGENT_RELAY_SERVER: "1",
				AGENT_RELAY_TOKEN: "agent-secret",
				LINK_AGENT_RELAY_OPERATOR_TOKEN: "operator-secret",
				AGENT_ENDPOINT_ID: "nonprod-bot",
				AGENT_PRINCIPAL_ID: "agent:nonprod-bot",
				LINK_AGENT_RELAY_OPERATOR_ENDPOINT: "operator-local",
				LINK_AGENT_RELAY_OPERATOR_PRINCIPAL: "operator:nicholas",
				AGENT_RELAY_CREDENTIALS_PATH: target,
			}),
			target,
		);
		assert.deepEqual(JSON.parse(readFileSync(target, "utf8")), {
			credentials: [
				{
					token: "agent-secret",
					principal: "agent:nonprod-bot",
					register: ["nonprod-bot"],
					send: ["operator-local"],
					list: ["operator-local"],
				},
				{
					token: "operator-secret",
					principal: "operator:nicholas",
					register: ["operator-local"],
					send: ["nonprod-bot"],
					list: ["nonprod-bot"],
				},
			],
		});
		assert.equal(statSync(dirname(target)).mode & 0o077, 0);
		assert.equal(statSync(target).mode & 0o077, 0);
	} finally {
		rmSync(home, { recursive: true, force: true });
	}
});

test("does not require relay secrets when the relay server is disabled", () => {
	assert.equal(installRelayCredentials({ AGENT_RELAY_SERVER: "0" }), undefined);
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
	assert.equal(config.settings.directTools, true);
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
		/grafana_list_datasources\(\{\}\)/,
	);
	assert.match(skill, /call a direct `grafana_\*` MCP tool before drafting/);
	assert.match(skill, /never reuse an error stated in an earlier Slack reply/);
});
