---
name: wiki
description: Create or maintain notes in a project's durable knowledge wiki (concepts, problems, research, sources, people). Use whenever adding, updating, renaming, or linking wiki notes, ingesting sources, or working with tags.
---

# Wiki

The wiki (`wiki/`) is a durable, source-traceable knowledge base for the project's domain. Notes are living documents that get revised and superseded, and every factual claim must be traceable to a real source. Project plans, submissions, meeting operations, and other time-bound material belong outside the wiki (e.g. in `docs/`).

`wiki/index.md` is the entry point and map of content. `wiki/log.md` is the append-only change log. Read `wiki/index.md` before working — it also records any project-specific conventions (taxonomy, scope) that extend this skill.

Folders identify a note's role. Frontmatter identifies its type and state. Tags provide cross-cutting classification. Wiki links express specific relationships. This structure is an Obsidian-style vault and is portable across projects; the domain vocabulary (tags, note titles) is whatever the vault already contains — discover it, don't assume it.

**Setting up a new wiki or improving the wiki platform itself** (structure, schema, tooling, workflows) — read [`references/karpathy-llm-wiki.md`](references/karpathy-llm-wiki.md) first. It is the design rationale this skill implements: an LLM-maintained, compounding knowledge base of interlinked markdown, with ingest / query / lint operations, a content index, and an append-only log.

## Boundaries

- Durable knowledge → `wiki/`.
- Frozen, time-bounded snapshots (reports, digests) → outside the wiki. If the project provides a `reports` skill, use it.
- Bounded investigations and time-boxed spikes → `wiki/research/`. If the project provides a `research` skill, use it.

## Folder responsibilities

### `concepts/`

Durable explanations of the things in the domain — topics, systems, processes, resources, metrics, methods, materials, and constraints.

A concept note explains what something is, how it works, why it matters, and how it relates to other concepts.

### `problems/`

Persistent scientific or engineering challenges in the domain. Describe the problem and competing approaches without assuming a preferred solution.

### `research/`

Bounded investigations, including:

* questions;
* hypotheses;
* literature reviews;
* evidence syntheses;
* trade studies;
* feasibility studies;
* experiment summaries;
* model summaries;
* time-boxed spikes.

Use `research_kind` to distinguish these rather than creating more folders.

### `sources/`

A source is a self-contained directory containing the original evidence and a searchable representation. Name the directory with a date-prefixed slug: `<YYYY-MM-DD>-<slug>` using the artifact's own publication or recording date, so sources sort chronologically.

```text
sources/
└── <YYYY-MM-DD>-<slug>/
    ├── source.md      # the source NOTE — frontmatter, tags, provenance, summary, links
    ├── source.pdf     # canonical original (any descriptive name / extension)
    ├── content.md     # searchable text extracted from the original (docling, OCR)
    ├── transcript.md  # diarized transcript, for audio/video
    └── figures/
```

Not every source requires every file, but **every source directory must contain a `source.md` note**. `source.md` is the graph node: it carries the frontmatter (`type: source`, tags, dates) that puts the source into the knowledge graph, and its body records provenance, summary, findings, limitations, and `[[wiki links]]` to related concepts, problems, and research. The extracted `content.md` / `transcript.md` are the searchable representations, not the note.

Tag every `source.md` with three to seven material tags — inspect the existing taxonomy first (`wiki-cli tags`; see Searching the wiki) and reuse tags. Source tags must describe material content, not subjects mentioned only in passing. Add `source_kind` (`paper`, `meeting`, `dataset`, `presentation`, …) to classify the artifact.

The original PDF, audio, video, presentation, dataset, or other artifact is canonical and must not be modified after ingestion. OCR and transcripts may be corrected without changing the source's meaning. If the project provides a `source-ingest` skill, use it to produce `content.md` / `transcript.md` from the original rather than hand-rolling `docling` / `whisper` invocations; it owns the toolchain setup. Large source artifacts are stored with Git LFS.

Example `source.md` frontmatter:

```yaml
---
title: <Source Title>
type: source
source_kind: paper
status: reviewed
authors: [<Author One>, <Author Two>]
publication: <Journal / venue>
published: 2019-07-15
doi: 10.0000/example
tags:
  - topic/<subject>
  - method/<method>
  - discipline/<field>
  - evidence/laboratory
created: 2026-07-14
updated: 2026-07-14
---
```

### `people/`

Notes for people named in the sources — authors, principal investigators, subjects. Each note is one person: what the sources say about them and `[[wiki links]]` back to the sources they appear in. Record only what the sources state; leave a note as `status: stub` until verified biography is added. Never invent affiliations, titles, or contact details.

One note per person, filename = the person's name; consolidate name variants into a single note rather than duplicating. `people/index.md` lists everyone, grouped by role or source.

Frontmatter uses `type: person` with `role:`, a free-text `affiliation:`, an optional `email:`, and an `org/` tag (plus any other material tags) so people join the graph and can be grouped by organization:

```yaml
---
title: <Full Name>
type: person
status: stub
role: researcher
affiliation: <Institution / division>
email: <optional>
tags:
  - org/<organization>
  - discipline/<field>
created: 2026-07-14
updated: 2026-07-14
---
```

## Frontmatter

Every substantive note must include:

```yaml
---
title: <Note Title>
type: problem
status: open
tags:
  - topic/<subject>
  - system/<system>
created: 2026-07-14
updated: 2026-07-14
---
```

Primary note types:

* `concept`
* `problem`
* `research`
* `source`
* `person`
* `reference`

Common status values:

* `stub`
* `draft`
* `active`
* `open`
* `reviewed`
* `resolved`
* `superseded`
* `archived`

Status represents workflow state, not scientific certainty.

Optional subtype fields include:

```yaml
research_kind: trade-study
source_kind: paper
role: researcher      # person notes
```

## Tagging conventions

Use hierarchical tags in lowercase kebab-case, `namespace/value`:

```text
topic/<subject>
system/<system>
metric/<indicator>
```

Do not use tags for values already represented by `type`, `status`, `research_kind`, or `source_kind`.

Most notes should use three to seven material tags describing material content, not subjects mentioned only in passing. **Before creating a tag, inspect the existing taxonomy (`wiki-cli tags`) and reuse an equivalent tag when one fits.** The concrete namespaces are project-specific: a vault grows its own vocabulary, so the authoritative taxonomy is whatever `wiki-cli tags` reports and whatever `wiki/index.md` documents — follow that over any list here.

A general-purpose starting set of namespaces, useful for most knowledge wikis and extended per project:

| Namespace     | Use                                                            |
| ------------- | ------------------------------------------------------------- |
| `topic/`      | Subjects, materials, and entities in the domain               |
| `system/`     | Engineered or organizational systems                          |
| `process/`    | Physical, chemical, biological, or operational processes      |
| `method/`     | Techniques, procedures, and instruments                       |
| `metric/`     | Measured or calculated performance indicators                 |
| `resource/`   | Inputs consumed or produced (energy, materials, time, …)      |
| `environment/`| Physical conditions and settings                              |
| `hazard/`     | Risks, failures, contamination, and safety concerns           |
| `control/`    | Manual, remote, autonomous, and closed-loop control           |
| `discipline/` | Scientific and engineering disciplines                        |
| `evidence/`   | Field, analog, laboratory, modeled, review, or hypothetical   |
| `org/`        | Organizations — affiliations of people and origins of sources |

Keep namespaces disjoint by role — e.g. equipment under `system/`, an input under `resource/`, its measurement under `sensor/` or `metric/`. Use `org/` to group notes by organization so they can be queried as a set (`wiki-cli tag org/<name>`); on person notes keep the free-text `affiliation:` field alongside the tag for the lab/division the tag omits. Add or rename a namespace only when the existing structure no longer fits, and record the convention in `wiki/index.md`.

## Searching the wiki

**Always search the wiki before answering a question about its contents, adding a note, or introducing a tag — and always search the sources, not just the notes.** A source's evidence lives in its `content.md` / `transcript.md`, so a plain-note grep silently misses it.

For routine retrieval, make **one agent tool call containing one `wiki-cli search` command**. Compose the primary term, synonyms, and related phrases into one ripgrep expression instead of issuing a sequence of searches:

```bash
./scripts/wiki-cli search 'primary term|synonym|related phrase'
```

Do not precede it with `fd`, `find`, `ls`, or another `rg`; do not loop over terms, folders, files, or note types; and do not separately search notes and sources. `wiki-cli search` already makes one scan across the whole wiki. Run taxonomy and link audits (`tags`, `backlinks`, `check`, and related commands) only when the task actually needs those results, such as preparing a wiki modification or performing an explicit health check—not during routine reading.

### `wiki-cli` (primary — always available)

`./scripts/wiki-cli` is a ripgrep + fd tool that audits and searches the vault from any shell, needs no running app, and is confined to the repository's `wiki/` tree (so it runs the same from any directory without scanning `docs/`, `code/`, or the caller's working directory). Its `search` command executes exactly one `rg` process with that canonical `wiki/` directory as its only search root. It reads only `*.md`, `*.mdx`, `*.yaml`, `*.yml`, and `*.json`, covering source `content.md` / `transcript.md` alongside notes without opening PDFs, audio, video, images, or other binary artifacts. **Prefer it** — it is the reliable and efficient path in agent and headless environments where the Obsidian CLI is unavailable.

Setup: `search` needs only `ripgrep` (`rg`) on `PATH`; audit commands also need `fd`. The script is committed executable and intentionally does not accept an alternate vault path.

```bash
./scripts/wiki-cli search 'x|alias|y'  # DEFAULT: one call/scan over notes + source text
./scripts/wiki-cli tags                # every tag with a count — reuse before adding
./scripts/wiki-cli tag topic/<x>       # a tag and all nested descendants
./scripts/wiki-cli tag-exact topic/<x> # only the exact tag
./scripts/wiki-cli backlinks <Note>    # notes linking to a note (check before renaming)
./scripts/wiki-cli links [<Note>]      # internal links, optionally from one note
./scripts/wiki-cli orphans             # notes with no incoming links
./scripts/wiki-cli check               # missing + ambiguous internal links (exit 1 if any)
./scripts/wiki-cli stats               # note / link / tag counts
./scripts/wiki-cli --help
```

Paths are relative to this skill directory. `tags` (taxonomy before tagging) and `backlinks` / `check` (link health before and after renaming) cover the checks the agent rules require. For anything `wiki-cli` does not expose, fall back to one batched `rg` invocation in one agent tool call, with the repository's canonical `wiki/` directory as its explicit and only search root. Apply the same text-file globs unless the task explicitly requires inspecting another known file type; never aim the fallback at the repository root.

### `probe` (optional — fuzzy / ranked AI search)

`probe` is an AI-friendly local search CLI (Elasticsearch-style boolean queries, ranked results, token budgeting) — reach for it when you want fuzzy "where is X discussed" retrieval rather than the exact matching and link audits `wiki-cli` gives. Point it at `wiki/` so it covers notes and extracted source text (`content.md` / `transcript.md`) together. It is **not in nixpkgs**; the project devenv installs it from npm — see [`references/environment-setup.md`](references/environment-setup.md) to set it up (`devenv shell -- probe …`, or `npx -y @buger/probe …`).

```bash
# Basic search
probe search "authentication" wiki/

# Boolean operators (Elasticsearch syntax)
probe search "error AND handling" wiki/
probe search "login OR auth" wiki/concepts/
probe search "database NOT sqlite" wiki/

# Search hints (file filters)
probe search "function AND ext:md" wiki/                 # only .md files
probe search "class AND file:sources/**/content.md" wiki/  # only ingested source text
probe search "error AND dir:research" wiki/              # notes under research/

# Limit results for AI context windows
probe search "API" wiki/ --max-tokens 10000
```

### Obsidian CLI (secondary — only when the desktop app is running)

The Obsidian CLI (`obsidian …`) queries the vault through the **running desktop app**; it produces no output — and may crash — when the app is not running, so it is unavailable in headless/agent sessions. Use it only interactively when the app is open (e.g. `obsidian search query="…"`, `obsidian backlinks file="…"`), targeting a named vault with `vault=<name>`. Otherwise reach for `wiki-cli`.

Prefer editing note files directly for content changes; use search primarily to query state the filesystem alone does not surface at a glance — tag taxonomy, backlinks, unresolved links, and orphans.

## Tags versus links

Use tags to group many notes by a shared classification (`resource/water`, `environment/microgravity`). Use wiki links for relationships to specific records (`[[Some Concept]]`, `[[Some Problem]]`). Do not use tags as a substitute for readable prose or meaningful links.

## Change log

`wiki/log.md` is the append-only change log for the wiki. After any meaningful change — creating, renaming, moving, archiving, or superseding a note, or ingesting a source — append one entry at the bottom of `log.md` in its documented format. Never edit or delete existing entries; record corrections as new entries.

## Agent rules

When working in the wiki:

1. Search existing notes and tags before creating new ones.
2. Prefer updating an existing note over creating a duplicate.
3. Keep frontmatter valid and update the `updated` date.
4. Use established tag namespaces and canonical spelling; discover them with `wiki-cli tags`.
5. Link related concepts, problems, research, and sources.
6. Make factual claims traceable to source directories.
7. Distinguish observations, source claims, calculations, interpretations, hypotheses, and recommendations.
8. State evidence class, assumptions, and limitations where relevant.
9. Never invent citations, source metadata, measurements, or results.
10. Never treat an LLM-generated summary as evidence.
11. Preserve original source artifacts and use Git LFS for large files.
12. Avoid new folders or taxonomy levels unless the existing structure has become difficult to use.
13. Keep `wiki/index.md` current when notes are added, renamed, or removed.
14. Append every meaningful change to `wiki/log.md`; never rewrite its history.
