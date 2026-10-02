#!/usr/bin/env node
// Operator-managed broker wrapper. Role tokens never grant contents or pull-request writes.
const fs = require('node:fs');
const { spawnSync } = require('node:child_process');
(async () => {
  const args = process.argv.slice(2);
  const allowed = (args[0] === 'issue' && ['view', 'list', 'comment', 'edit'].includes(args[1])) || (args[0] === 'label' && args[1] === 'list');
  const needsIssue = args[0] === 'issue' && ['view', 'comment', 'edit'].includes(args[1]);
  if (needsIssue && !/^[1-9][0-9]*$/.test(args[2] || '')) throw Error('Use a numeric issue number in the selected repository.');
  if (!allowed || args.some(arg => arg === '--hostname' || arg.startsWith('--hostname='))) throw Error('Only issue triage commands are enabled.');
  let repo = process.env.GH_REPO;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--repo' || args[i] === '-R') repo = args[++i];
    else if (args[i].startsWith('--repo=')) repo = args[i].slice(7);
    else if (args[i].startsWith('-R') && args[i].length > 2) repo = args[i].slice(2);
  }
  const repositories = JSON.parse(process.env.OUTFITTER_REPOSITORIES_JSON);
  const match = typeof repo === 'string' && repositories.find(item => item.fullName.toLowerCase() === repo.toLowerCase());
  if (!match) throw Error('Select a provisioned repository with --repo owner/name.');
  const response = await fetch(`${process.env.OUTFITTER_SERVICE_BASE_URL}/api/residents/github-token`, {
    method: 'POST', redirect: 'error', signal: AbortSignal.timeout(15000),
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${process.env.OUTFITTER_RESIDENT_TOKEN}` },
    body: JSON.stringify({ repository_id: match.id }),
  });
  if (!response.ok) throw Error('GitHub token broker denied the request.');
  const token = await response.json();
  if (typeof token.token !== 'string' || !token.token) throw Error('Invalid GitHub token response.');
  const realGh = fs.readFileSync('/workspace/.hosted/gh-path', 'utf8');
  const result = spawnSync(realGh, args, { stdio: 'inherit', env: { ...process.env, GH_TOKEN: token.token, GITHUB_TOKEN: token.token, GH_HOST: 'github.com', GH_REPO: match.fullName, GH_PROMPT_DISABLED: '1' } });
  process.exitCode = result.status ?? 1;
})().catch(() => { console.error('Outfitter GitHub triage command failed. Check repository access and resident status.'); process.exitCode = 1; });
