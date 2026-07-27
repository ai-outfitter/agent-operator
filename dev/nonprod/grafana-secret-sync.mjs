import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export function buildGrafanaSecretManifest({
	context,
	namespace,
	authorization,
}) {
	if (context !== "unsup-nonprod-engineer") {
		throw new Error("LINK_KUBE_CONTEXT must be unsup-nonprod-engineer");
	}
	if (namespace !== "agent-nonprod-bot") {
		throw new Error("LINK_AGENT_NAMESPACE must be agent-nonprod-bot");
	}
	if (!authorization?.startsWith("Basic ")) {
		throw new Error(
			"MCP_GRAFANA_BASIC_AUTH_HEADER must contain a Basic authorization header",
		);
	}

	return JSON.stringify({
		apiVersion: "v1",
		kind: "Secret",
		metadata: { name: "nonprod-bot-grafana", namespace },
		type: "Opaque",
		stringData: {
			MCP_GRAFANA_BASIC_AUTH_HEADER: authorization,
		},
	});
}

export function main(env = process.env, input = readFileSync(0, "utf8")) {
	const context = env.LINK_KUBE_CONTEXT;
	const namespace = env.LINK_AGENT_NAMESPACE;
	const authorization =
		env.MCP_GRAFANA_BASIC_AUTH_HEADER?.trim() || input.trim();
	const manifest = buildGrafanaSecretManifest({
		context,
		namespace,
		authorization,
	});

	const kubectl = spawn("kubectl", ["--context", context, "apply", "-f", "-"], {
		stdio: ["pipe", "inherit", "inherit"],
	});
	kubectl.stdin.end(manifest);
	kubectl.on("exit", (code, signal) => {
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
