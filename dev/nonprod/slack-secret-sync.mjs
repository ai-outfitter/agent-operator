import { spawn } from "node:child_process";

const context = process.env.LINK_KUBE_CONTEXT;
const namespace = process.env.LINK_AGENT_NAMESPACE;
const appToken = process.env.SLACK_APP_TOKEN?.trim();
const botToken = process.env.SLACK_BOT_TOKEN?.trim();

if (context !== "unsup-nonprod-engineer") {
	throw new Error("LINK_KUBE_CONTEXT must be unsup-nonprod-engineer");
}
if (namespace !== "agent-nonprod-bot") {
	throw new Error("LINK_AGENT_NAMESPACE must be agent-nonprod-bot");
}
if (!appToken?.startsWith("xapp-") || !botToken?.startsWith("xoxb-")) {
	throw new Error("Slack CLI did not inject the expected app and bot tokens");
}

const manifest = JSON.stringify({
	apiVersion: "v1",
	kind: "Secret",
	metadata: { name: "nonprod-bot-slack", namespace },
	type: "Opaque",
	stringData: {
		SLACK_APP_TOKEN: appToken,
		SLACK_BOT_TOKEN: botToken,
	},
});

const kubectl = spawn("kubectl", ["--context", context, "apply", "-f", "-"], {
	stdio: ["pipe", "inherit", "inherit"],
});
kubectl.stdin.end(manifest);
kubectl.on("exit", (code, signal) => {
	if (signal) process.kill(process.pid, signal);
	else process.exit(code ?? 1);
});
