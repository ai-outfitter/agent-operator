import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

if (!process.env.SLACK_VERIFY_CHANNEL_IDS?.trim()) {
	throw new Error("SLACK_VERIFY_CHANNEL_IDS must name the smoke-test channel");
}

const channelsRoot = fileURLToPath(new URL("../../../channels", import.meta.url));
const verifier = spawn("npm", ["run", "verify:slack"], {
	cwd: channelsRoot,
	env: {
		...process.env,
		SLACK_VERIFY_MARKER:
			process.env.SLACK_VERIFY_MARKER?.trim() || "[channels-nonprod-smoke]",
	},
	stdio: "inherit",
});
verifier.on("exit", (code, signal) => {
	if (signal) process.kill(process.pid, signal);
	else process.exit(code ?? 1);
});
