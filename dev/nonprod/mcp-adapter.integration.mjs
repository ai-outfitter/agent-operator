import assert from "node:assert/strict";
import { createServer } from "node:http";

const adapterRoot =
	process.env.PI_MCP_ADAPTER_TEST_ROOT ??
	"/usr/local/lib/node_modules/pi-mcp-adapter";
const jitiRoot =
	"/usr/local/lib/node_modules/@ai-outfitter/outfitter/node_modules/@earendil-works/pi-coding-agent/node_modules/jiti";
const { createJiti } = await import(`${jitiRoot}/lib/jiti.mjs`);
const jiti = createJiti(import.meta.url, { interopDefault: true });
const { McpServerManager } = await jiti.import(
	`${adapterRoot}/server-manager.ts`,
);
const { McpServer } = await import(
	`${adapterRoot}/node_modules/@modelcontextprotocol/sdk/dist/esm/server/mcp.js`
);
const { StreamableHTTPServerTransport } = await import(
	`${adapterRoot}/node_modules/@modelcontextprotocol/sdk/dist/esm/server/streamableHttp.js`
);

const authorization = "Basic local-integration-only";
const requests = [];
const transports = new Set();

const httpServer = createServer(async (request, response) => {
	requests.push({
		authorization: request.headers.authorization,
		method: request.method,
	});

	if (request.headers.authorization !== authorization) {
		response.writeHead(401).end("unauthorized");
		return;
	}

	const mcpServer = new McpServer({
		name: "authenticated-grafana-fixture",
		version: "1.0.0",
	});
	mcpServer.registerTool(
		"list_datasources",
		{
			description: "List test Grafana data sources",
			inputSchema: {},
		},
		async () => ({
			content: [
				{
					type: "text",
					text: JSON.stringify([{ name: "nonprod-prometheus", type: "prometheus" }]),
				},
			],
		}),
	);

	const transport = new StreamableHTTPServerTransport({
		sessionIdGenerator: undefined,
	});
	transports.add(transport);
	response.on("close", () => transports.delete(transport));
	await mcpServer.connect(transport);
	await transport.handleRequest(request, response);
});

await new Promise((resolve) => httpServer.listen(0, "127.0.0.1", resolve));
const address = httpServer.address();
assert(address && typeof address === "object");
const url = `http://127.0.0.1:${address.port}/mcp`;

const manager = new McpServerManager();

try {
	process.env.TEST_MCP_AUTHORIZATION = authorization;
	const connection = await manager.connect("grafana", {
		url,
		headers: {
			Authorization: "${TEST_MCP_AUTHORIZATION}",
		},
		includeTools: ["list_datasources"],
		requestTimeoutMs: 5_000,
	});

	assert.deepEqual(
		connection.tools.map(({ name }) => name),
		["list_datasources"],
	);
	const result = await connection.client.callTool({
		name: "list_datasources",
		arguments: {},
	});
	assert.equal(result.content[0].type, "text");
	assert.match(result.content[0].text, /nonprod-prometheus/);
	assert(requests.length >= 2);
	assert(requests.every(({ authorization: value }) => value === authorization));

	const rejectedManager = new McpServerManager();
	process.env.TEST_MCP_AUTHORIZATION = "Basic wrong";
	await assert.rejects(
		rejectedManager.connect("grafana", {
			url,
			headers: {
				Authorization: "${TEST_MCP_AUTHORIZATION}",
			},
			requestTimeoutMs: 5_000,
		}),
		/401|unauthorized/i,
	);
	await rejectedManager.closeAll();

	console.log(
		JSON.stringify({
			adapter: "pi-mcp-adapter",
			authenticatedRequests: requests.filter(
				({ authorization: value }) => value === authorization,
			).length,
			tool: connection.tools[0].name,
			result: "passed",
		}),
	);
} finally {
	await manager.closeAll();
	for (const transport of transports) await transport.close();
	await new Promise((resolve, reject) =>
		httpServer.close((error) => (error ? reject(error) : resolve())),
	);
}
