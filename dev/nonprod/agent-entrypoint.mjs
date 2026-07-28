import { spawn } from "node:child_process";
import {
	chmodSync,
	copyFileSync,
	mkdirSync,
	renameSync,
	writeFileSync,
} from "node:fs";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";

const DEFAULT_MODEL = "openai-codex/gpt-5.4-mini";
const DEFAULT_MCP_CONFIG_SOURCE = "/opt/link/mcp.json";
const MCP_EXTENSION = "/usr/local/lib/node_modules/pi-mcp-adapter/index.ts";
const DEFAULT_RELAY_CREDENTIALS_PATH =
	"/workspace/.channels/relay/credentials.json";

export function buildPiArguments(model = DEFAULT_MODEL) {
	return [
		"--mode",
		"rpc",
		"--session-id",
		"nonprod-bot",
		"--offline",
		"--approve",
		"--no-context-files",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-builtin-tools",
		"--tools",
		"channel_read,channel_respond,mcp,grafana_list_datasources,grafana_get_datasource,grafana_query_prometheus,grafana_list_prometheus_metric_names,grafana_list_prometheus_label_names,grafana_list_prometheus_label_values,grafana_query_loki_logs,grafana_list_loki_label_names,grafana_list_loki_label_values,grafana_query_loki_stats",
		"--model",
		model,
		"--extension",
		"/opt/channels/extensions/relay-extension.ts",
		"--extension",
		"/opt/channels/extensions/index.ts",
		"--extension",
		MCP_EXTENSION,
		"--skill",
		"/opt/link/slack-grafana-responder/SKILL.md",
		"--append-system-prompt",
		"/opt/link/slack-grafana-system-prompt.md",
	];
}

export function installMcpConfig(env = process.env) {
	const home = env.HOME?.trim() || "/workspace";
	const source =
		env.LINK_MCP_CONFIG_SOURCE?.trim() || DEFAULT_MCP_CONFIG_SOURCE;
	const target = join(home, ".pi", "agent", "mcp.json");
	mkdirSync(dirname(target), { recursive: true });
	copyFileSync(source, target);
	return target;
}

function enabled(value) {
	const normalized = value?.trim().toLowerCase();
	return normalized === "1" || normalized === "true";
}

export function installRelayCredentials(env = process.env) {
	if (!enabled(env.AGENT_RELAY_SERVER)) return undefined;

	const agentToken = env.AGENT_RELAY_TOKEN?.trim();
	const operatorToken = env.LINK_AGENT_RELAY_OPERATOR_TOKEN?.trim();
	if (!agentToken || !operatorToken) {
		throw new Error(
			"AGENT_RELAY_TOKEN and LINK_AGENT_RELAY_OPERATOR_TOKEN are required when the relay server is enabled",
		);
	}

	const agentEndpoint = env.AGENT_ENDPOINT_ID?.trim() || "nonprod-bot";
	const agentPrincipal =
		env.AGENT_PRINCIPAL_ID?.trim() || `agent:${agentEndpoint}`;
	const operatorEndpoint =
		env.LINK_AGENT_RELAY_OPERATOR_ENDPOINT?.trim() || "operator-local";
	const operatorPrincipal =
		env.LINK_AGENT_RELAY_OPERATOR_PRINCIPAL?.trim() || "operator:local";
	const target =
		env.AGENT_RELAY_CREDENTIALS_PATH?.trim() ||
		DEFAULT_RELAY_CREDENTIALS_PATH;
	const directory = dirname(target);
	const temporary = join(directory, `.credentials-${process.pid}.json`);

	mkdirSync(directory, { recursive: true, mode: 0o700 });
	chmodSync(directory, 0o700);
	writeFileSync(
		temporary,
		`${JSON.stringify(
			{
				credentials: [
					{
						token: agentToken,
						principal: agentPrincipal,
						register: [agentEndpoint],
						send: [operatorEndpoint],
						list: [operatorEndpoint],
					},
					{
						token: operatorToken,
						principal: operatorPrincipal,
						register: [operatorEndpoint],
						send: [agentEndpoint],
						list: [agentEndpoint],
					},
				],
			},
			null,
			2,
		)}\n`,
		{ mode: 0o600 },
	);
	renameSync(temporary, target);
	chmodSync(target, 0o600);
	return target;
}

export function main(env = process.env) {
	installMcpConfig(env);
	const relayCredentialsPath = installRelayCredentials(env);
	const model = env.LINK_SLACK_MODEL?.trim() || DEFAULT_MODEL;
	const child = spawn("pi", buildPiArguments(model), {
		env: {
			...env,
			HOME: "/workspace",
			OUTFITTER_CHANNELS:
				env.OUTFITTER_CHANNELS?.trim() || "slack,agent",
			...(relayCredentialsPath
				? { AGENT_RELAY_CREDENTIALS_PATH: relayCredentialsPath }
				: {}),
		},
		stdio: ["pipe", "inherit", "inherit"],
	});

	for (const signal of ["SIGINT", "SIGTERM"]) {
		process.on(signal, () => child.kill(signal));
	}

	child.on("error", (error) => {
		console.error(`link-agent: failed to start Pi: ${error.message}`);
		process.exit(1);
	});
	child.on("exit", (code, signal) => {
		if (signal) process.kill(process.pid, signal);
		else process.exit(code ?? 1);
	});
}

if (
	process.argv[1] &&
	import.meta.url === pathToFileURL(process.argv[1]).href
) {
	main();
}
