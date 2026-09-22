const fs = require('node:fs');
const path = require('node:path');
(async () => {
  fs.mkdirSync('/workspace/.hosted/bin', { recursive: true, mode: 0o700 });
  // Search the original image PATH, excluding our wrapper from prior starts.
  const realGh = process.env.OUTFITTER_RUNTIME_PATH.split(':').map(dir => path.join(dir, 'gh')).find(file => {
    try { fs.accessSync(file, fs.constants.X_OK); return fs.realpathSync(file) !== '/workspace/.hosted/bin/gh'; } catch { return false; }
  });
  if (!realGh) throw Error('The pinned resident image must include gh.');
  fs.writeFileSync('/workspace/.hosted/gh-path', realGh, { mode: 0o600 });
  fs.writeFileSync('/workspace/.hosted/bin/gh', ghWrapper, { mode: 0o700 });
  const response = await fetch(`${process.env.OUTFITTER_SERVICE_BASE_URL}/v1/models`, {
    headers: { Authorization: `Bearer ${process.env.OUTFITTER_RESIDENT_TOKEN}` }, redirect: 'error', signal: AbortSignal.timeout(15000),
  });
  if (!response.ok) throw Error('Resident model discovery failed.');
  const payload = await response.json();
  if (!payload.data.some(model => model.id === process.env.OUTFITTER_SELECTED_MODEL)) throw Error('Configured resident model is unavailable.');
  const models = payload.data.map(model => ({
    id: model.id, name: model.name, reasoning: false, input: ['text'], contextWindow: model.context_length, maxTokens: model.max_output_tokens,
    cost: { input: Number(model.pricing.prompt) * 1e6, output: Number(model.pricing.completion) * 1e6, cacheRead: 0, cacheWrite: 0 },
    compat: { supportsStore: false, supportsDeveloperRole: false, supportsReasoningEffort: false, supportsUsageInStreaming: true, maxTokensField: 'max_tokens' },
  }));
  fs.mkdirSync('/workspace/.pi/agent', { recursive: true, mode: 0o700 });
  fs.writeFileSync('/workspace/.pi/agent/models.json', JSON.stringify({ providers: { outfitter: { baseUrl: `${process.env.OUTFITTER_SERVICE_BASE_URL}/v1`, api: 'openai-completions', apiKey: '$OUTFITTER_RESIDENT_TOKEN', models } } }), { mode: 0o600 });
})().catch(() => { console.error('Outfitter resident initialization failed. Check image tools, model access, and service origin.'); process.exitCode = 1; });
