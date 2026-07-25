import { spawn } from "node:child_process";

const model = process.env.LINK_SLACK_MODEL?.trim() || "openai-codex/gpt-5.4-mini";
const child = spawn(
	"pi",
	[
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
		"channel_read,channel_respond",
		"--model",
		model,
		"--extension",
		"/opt/channels/extensions/index.ts",
		"--skill",
		"/opt/channels/slack-responder/SKILL.md",
		"--append-system-prompt",
		"/opt/channels/slack-system-prompt.md",
	],
	{
		env: {
			...process.env,
			HOME: "/workspace",
			OUTFITTER_CHANNELS: "slack",
		},
		stdio: ["pipe", "inherit", "inherit"],
	},
);

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
