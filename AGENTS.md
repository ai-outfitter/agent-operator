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
