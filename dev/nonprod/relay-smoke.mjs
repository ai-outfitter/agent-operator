import { randomUUID } from "node:crypto";

const url =
	process.env.AGENT_RELAY_URL?.trim() ||
	"ws://127.0.0.1:8787/v1/connect";
const token = process.env.AGENT_RELAY_TOKEN?.trim();
const endpoint = process.env.AGENT_RELAY_ENDPOINT?.trim() || "operator-local";
const principal =
	process.env.AGENT_RELAY_PRINCIPAL?.trim() || "operator:nicholas";
const recipient = process.env.AGENT_RELAY_RECIPIENT?.trim() || "nonprod-bot";
const timeoutMs = Number(process.env.AGENT_RELAY_TIMEOUT_MS ?? "180000");
const body =
	process.argv.slice(2).join(" ").trim() ||
	"Use a Grafana MCP tool to list the configured datasources, then report the tool name and result.";

if (!token) throw new Error("AGENT_RELAY_TOKEN is required");
if (!Number.isInteger(timeoutMs) || timeoutMs < 1_000) {
	throw new Error("AGENT_RELAY_TIMEOUT_MS must be an integer of at least 1000");
}

const messageId = randomUUID();
const requestId = randomUUID();
const conversationId = `nonprod-smoke-${messageId}`;
const socket = new WebSocket(url);
const timeout = setTimeout(() => {
	socket.close();
	console.error("timed out waiting for nonprod-bot relay response");
	process.exitCode = 1;
}, timeoutMs);

function send(frame) {
	socket.send(JSON.stringify(frame));
}

socket.addEventListener("open", () => {
	send({
		type: "authenticate",
		version: 1,
		token,
		endpoint,
		principal,
		cursor: 0,
	});
});

socket.addEventListener("message", (event) => {
	const frame = JSON.parse(String(event.data));
	switch (frame.type) {
		case "authenticated":
			send({
				type: "send",
				requestId,
				input: {
					id: messageId,
					recipient,
					conversationId,
					body,
				},
			});
			break;
		case "ping":
			send({ type: "pong", nonce: frame.nonce });
			break;
		case "deliver":
			send({ type: "ack", cursor: frame.cursor });
			if (frame.message?.replyTo !== messageId) break;
			clearTimeout(timeout);
			console.log(
				JSON.stringify({
					messageId,
					responseId: frame.message.id,
					response: frame.message.body,
				}),
			);
			socket.close();
			break;
		case "error":
			clearTimeout(timeout);
			console.error(`relay error ${frame.code}: ${frame.message}`);
			socket.close();
			process.exitCode = 1;
			break;
		default:
			break;
	}
});

socket.addEventListener("error", () => {
	clearTimeout(timeout);
	console.error(`failed to connect to ${url}`);
	process.exitCode = 1;
});
