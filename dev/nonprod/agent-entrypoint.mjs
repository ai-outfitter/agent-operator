import { spawn } from "node:child_process";
import { copyFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";

const DEFAULT_MODEL = "openai-codex/gpt-5.4-mini";
const DEFAULT_MCP_CONFIG_SOURCE = "/opt/link/mcp.json";
const MCP_EXTENSION = "/usr/local/lib/node_modules/pi-mcp-adapter/index.ts";

export function buildPiArguments(model = DEFAULT_MODEL) {
	return [
		"--mode",
		"rpc",
		"--no-session",
		"--offline",
		"--approve",
		"--no-context-files",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-builtin-tools",
		"--tools",
		"channel_read,channel_respond,mcp",
		"--model",
		model,
		"--extension",
		"/opt/channels/extensions/index.ts",
		"--extension",
		MCP_EXTENSION,
		"--skill",
		"/opt/channels/slack-responder/SKILL.md",
		"--append-system-prompt",
		"/opt/channels/slack-system-prompt.md",
	];
}

export function installMcpConfig(env = process.env) {
	const authorization = env.MCP_GRAFANA_BASIC_AUTH_HEADER?.trim();
	if (!authorization) {
		throw new Error("MCP_GRAFANA_BASIC_AUTH_HEADER must be configured");
	}

	const home = env.HOME?.trim() || "/workspace";
	const source =
		env.LINK_MCP_CONFIG_SOURCE?.trim() || DEFAULT_MCP_CONFIG_SOURCE;
	const target = join(home, ".pi", "agent", "mcp.json");
	mkdirSync(dirname(target), { recursive: true });
	copyFileSync(source, target);
	return target;
}

export function main(env = process.env) {
	installMcpConfig(env);
	const model = env.LINK_SLACK_MODEL?.trim() || DEFAULT_MODEL;
	const child = spawn("pi", buildPiArguments(model), {
		env: {
			...env,
			HOME: "/workspace",
			OUTFITTER_CHANNELS: "slack",
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
