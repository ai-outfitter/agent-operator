# Agent guidance

## Local development

Local development instructions (Nix, devenv, local cluster, demo tasks) live in
@CONTRIBUTING.md. Put any new local-dev setup or workflow there, not in the
user-facing docs under `docs/documentation/`.

## Git

- **Do not create branches.** Work directly on the current branch (`main`) unless
  Nicholas explicitly asks for a branch. Do not commit or push unless asked.

## Resident filesystem

Resident homes are `/home/<Agent metadata.name>` on `agent-home`, with matching
`HOME` and passwd identity. Use application-native/XDG paths under that home;
Nix's store stays at `/nix`. Chromium uses the home's `.config/chromium` directory.
Do not add old-path aliases, data migrations, or a separate browser-data volume.

## GitHub identity

Follow [GitHub agent identity and authorization](docs/github-agent-identity.md).
Residents use Chrome MCP and their password manager for their own-account sign-in.
Preserve the intended agent author; never substitute a human account or App bot.
Resident `GH_TOKEN` credentials are temporary; gateway-managed App user grants
are planned. Declarative configuration references bindings, not raw credentials.
