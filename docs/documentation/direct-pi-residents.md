# Direct Pi residents

Set `spec.pi` to run Pi directly. The operator renders native `settings.json`,
`SYSTEM.md`, and optional `models.json` from the Agent resource. The selected
image must contain the `pi` executable and its declared packages. The provided
`code/pi` image pins Pi and Channels and includes GitHub CLI; it contains no
Outfitter installation or catalog. GitHub operations use `gh` through Pi's bash
tool. The gateway credential remains separate from the GitHub work token.

```yaml
spec:
  pi:
    model: openai/gpt-4o-mini
    thinking: medium
    packages:
      - /opt/pi/node_modules/@ai-outfitter/channels
    systemPrompt: |
      You are a resident engineer. Process only the delivered task's subject.
      Implement new assignments; answer addressed questions; review requested
      pull requests; revise existing pull requests for requested changes.
      Never merge or close issues. Read back external writes before completion.
  forge:
    username: agent-login
  github:
    mode: webhook
  channels: [slack]
```

Use the standard credential Secret for provider credentials. `spec.pi.models`
is a native Pi models.json object; refer to credentials by environment variable,
never put credential values in the CRD. For forge residents the operator maps
`GITHUB_PERSONAL_ACCESS_TOKEN` from that Secret into `GH_TOKEN` for GitHub CLI.

Direct Pi and a catalog profile, catalog sync, or catalog workflow are mutually
exclusive. Remove catalog bootstrap setup steps during migration. Organizations
may omit catalogs when all their residents use direct Pi. Other residents can
retain their existing catalog profile during migration.

The generated Pi configuration lives in a fresh per-pod volume under
`$HOME/.config/pi-runtime`. It is writable because Pi locks its settings files;
restarts reconstruct it from the CRD. Settings changes roll the pod. Session
files retain the existing `$HOME/.pi/agent/sessions` path on the home PVC, and Channels retains its existing task
store path and acceptance history. Channels task sessions load the same Pi
configuration directory, including its system prompt and packages.

Pi ignores untrusted project configuration at startup. A separate empty,
read-only volume masks `$HOME/.agents`, preventing leftover catalog skills
from being discovered by Channels task sessions. No catalog skills or
profile inheritance are needed. Configuration validation and an authenticated
A2A read are useful checks, but verify a real provider event and the resident's
response before claiming migration is complete.

Older installations with only an `agent-workspace` PVC are blocked from
creating an empty replacement. Preserve their state and provision `agent-home`
before upgrading; this operator does not migrate or delete legacy volumes.
