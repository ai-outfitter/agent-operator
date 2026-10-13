const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(`${__dirname}/gh-wrapper.cjs`, 'utf8');
const setup = fs.readFileSync(`${__dirname}/runtime-setup.cjs`, 'utf8');
const env = { OUTFITTER_RESIDENT_TOKEN: 'resident-token', OUTFITTER_SERVICE_BASE_URL: 'https://ai-outfitter.com', OUTFITTER_REPOSITORIES_JSON: '[{"id":45,"fullName":"owner/repo"}]', OUTFITTER_RUNTIME_PATH: '/usr/bin:/bin', OUTFITTER_SELECTED_MODEL: 'test' };
async function invoke(args, response = { ok: true, json: async () => ({ token: 'repo-token' }) }) {
  const requests = []; const spawned = []; const errors = [];
  const process = { argv: ['node', 'gh', ...args], env: { ...env } };
  await vm.runInNewContext(source, { require: name => name === 'node:fs' ? { readFileSync: () => '/usr/bin/gh' } : { spawnSync: (...params) => { spawned.push(params); return { status: 0 }; } }, process, AbortSignal, console: { error: text => errors.push(text) }, fetch: async (...params) => { requests.push(params); return response; } });
  return { requests, spawned, errors, process };
}
test('brokers fresh repository token per command and invokes preserved gh without a shell', async () => {
  for (let attempt = 0; attempt < 2; attempt++) {
    const result = await invoke(['issue', 'comment', '9', '--repo', 'owner/repo', '--body', 'Triage plan']);
    assert.equal(result.requests.length, 1); assert.equal(result.requests[0][0], 'https://ai-outfitter.com/api/residents/github-token');
    assert.equal(result.requests[0][1].body, '{"repository_id":45}'); assert.equal(result.requests[0][1].redirect, 'error');
    assert.equal(result.spawned[0][0], '/usr/bin/gh'); assert.equal(result.spawned[0][2].env.GH_TOKEN, 'repo-token'); assert.equal(result.spawned[0][2].env.GH_HOST, 'github.com');
    assert.equal(result.process.exitCode, 0);
  }
});
test('rejects non-triage commands, foreign repositories, URL issue selectors and host overrides before brokering', async () => {
  for (const args of [['pr', 'create', '-R', 'owner/repo'], ['api', 'repos/owner/repo'], ['issue', 'view', '9', '-R', 'other/repo'], ['issue', 'view', 'https://evil.test/issue/9', '-R', 'owner/repo'], ['issue', 'view', '9', '--repo=owner/repo', '--hostname=evil.test']]) {
    const result = await invoke(args); assert.equal(result.requests.length, 0); assert.equal(result.spawned.length, 0); assert.equal(result.process.exitCode, 1);
  }
});
test('never spawns gh or prints credentials after broker rejection', async () => {
  const result = await invoke(['issue', 'view', '9', '-Rowner/repo'], { ok: false });
  assert.equal(result.spawned.length, 0); assert.equal(result.process.exitCode, 1); assert.ok(!result.errors.join('').includes('resident-token'));
});
test('initializes a native Outfitter model registry without storing any credential value', async () => {
  const writes = new Map(); const process = { env: { ...env } }; const requests = [];
  const fakeFS = { constants: { X_OK: 1 }, mkdirSync: () => {}, accessSync: () => {}, realpathSync: file => file, writeFileSync: (file, value) => writes.set(file, value) };
  await vm.runInNewContext(`const ghWrapper = ${JSON.stringify(source)};\n${setup}`, { require: name => name === 'node:fs' ? fakeFS : require(name), process, AbortSignal, console: { error: () => assert.fail('setup failed') }, fetch: async (...params) => { requests.push(params); return { ok: true, json: async () => ({ data: [{ id: 'test', name: 'Test', context_length: 8000, max_output_tokens: 1000, pricing: { prompt: '0.000001', completion: '0.000002' } }] }) }; } });
  assert.equal(writes.get('/workspace/.hosted/gh-path'), '/usr/bin/gh');
  const models = JSON.parse(writes.get('/workspace/.pi/agent/models.json'));
  assert.equal(models.providers.outfitter.apiKey, '$OUTFITTER_RESIDENT_TOKEN'); assert.equal(models.providers.outfitter.models[0].maxTokens, 1000);
  assert.equal(requests[0][0], 'https://ai-outfitter.com/v1/models');
  assert.ok(![...writes.values()].join('').includes('resident-token'));
});
test('native Pi resolves the generated environment-reference credential', { skip: !process.env.NATIVE_PI_MODULE }, async () => {
  const { AuthStorage, ModelRegistry } = await import(process.env.NATIVE_PI_MODULE);
  const os = require('node:os'); const path = require('node:path');
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'resident-pi-models-'));
  process.env.OUTFITTER_RESIDENT_SMOKE_TOKEN = 'synthetic-native-proof';
  try {
    const file = path.join(directory, 'models.json');
    fs.writeFileSync(file, JSON.stringify({ providers: { outfitter: { baseUrl: 'https://ai-outfitter.com/v1', api: 'openai-completions', apiKey: '$OUTFITTER_RESIDENT_SMOKE_TOKEN', models: [{ id: 'test', name: 'Test', reasoning: false, input: ['text'], contextWindow: 8000, maxTokens: 1000, cost: { input: 1, output: 2, cacheRead: 0, cacheWrite: 0 } }] } } }));
    const registry = ModelRegistry.create(AuthStorage.inMemory(), file);
    assert.equal(registry.getError(), undefined);
    const auth = await registry.getApiKeyAndHeaders(registry.find('outfitter', 'test'));
    assert.equal(auth.ok, true); assert.equal(auth.apiKey, 'synthetic-native-proof');
  } finally { delete process.env.OUTFITTER_RESIDENT_SMOKE_TOKEN; fs.rmSync(directory, { recursive: true, force: true }); }
});
