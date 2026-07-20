# Environment setup

Nuances for getting the wiki search toolchain working. Read this when `probe` is
missing from PATH, or when setting up the wiki tooling in a new checkout.

The core `wiki-cli search` command (`scripts/wiki-cli`) needs only `ripgrep`
(`rg`); link and taxonomy audit commands also need `fd`. Both are usually already
present, and neither requires a devenv. The `probe` CLI below is an **optional**
AI/semantic search layer on top; install it when you want fuzzy "where is X
discussed" search across notes and source text.

## Installing `probe`

`probe` (buger's AI-friendly local code search) is **not in nixpkgs**; it is
distributed as the npm package `@buger/probe` (binary name `probe`). Do not
improvise a global install — add it to the project devenv so every session gets
the same tool.

### On a Nix system: use the project devenv

The repo-root `devenv.nix` provides `nodejs` and installs `probe` on shell entry,
mirroring how it installs `docling`:

```nix
packages = [ pkgs.nodejs /* … */ ];

enterShell = ''
  export NPM_CONFIG_PREFIX="$HOME/.npm-global"
  export PATH="$NPM_CONFIG_PREFIX/bin:$PATH"
  command -v probe >/dev/null 2>&1 || npm install -g @buger/probe
'';
```

`command -v devenv` confirms devenv is installed. Enter with `devenv shell`, or
run a single command with `devenv shell -- probe search "query" wiki/`. The first
entry runs `npm install -g @buger/probe`, which downloads the platform binary
from the package's postinstall — run it as a background task the first time.
Verify with `devenv test` (it runs `probe --version`).

If the root `devenv.nix` predates this and lacks `probe`, add `pkgs.nodejs` to
`packages` and the `enterShell` block above rather than overwriting the file.

### Fallback: no devenv (non-Nix, or devenv unavailable)

Install the npm package into a writable global prefix on PATH:

```bash
export NPM_CONFIG_PREFIX="$HOME/.npm-global"
export PATH="$NPM_CONFIG_PREFIX/bin:$PATH"
npm install -g @buger/probe
probe --version
```

Or run it without installing, resolving the latest each call:

```bash
npx -y @buger/probe search "query" wiki/
```

## Using `probe` over the wiki

Point `probe` at `wiki/` (or a subfolder) so it searches notes and the extracted
source text (`content.md` / `transcript.md`) together:

```bash
# Basic search
probe search "authentication" wiki/

# Boolean operators (Elasticsearch syntax)
probe search "hydroponics AND microgravity" wiki/
probe search "harvest OR pick" wiki/concepts/
probe search "water NOT substrate" wiki/

# Search hints (file filters)
probe search "delivery AND ext:md" wiki/                 # only .md files
probe search "CFU AND file:sources/**/content.md" wiki/  # only ingested source text
probe search "orbit AND dir:concepts" wiki/              # notes under concepts/

# Limit results for AI context windows
probe search "constellation" wiki/ --max-tokens 10000
```

`probe` complements `wiki-cli`: use `wiki-cli` for exact/audit queries (tags,
backlinks, link health) and `probe` for fuzzy, ranked, token-budgeted retrieval.
