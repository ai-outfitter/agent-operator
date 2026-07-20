---
name: source-ingest
description: Convert source artifacts in wiki/sources/ into searchable text — transcribe audio/video with whisper.cpp (including two-speaker diarization) and convert PDFs to markdown with docling. Use when ingesting a recording, meeting, paper, or media file into the wiki.
---

# Source ingest

Turn original source artifacts (audio, video, PDFs) into searchable representations alongside them in their `wiki/sources/<source>/` directory, per the `wiki` skill: the original artifact is canonical and never modified; transcripts and OCR may be corrected without changing meaning; large binaries go through Git LFS.

## Tool check

Run this preflight first — it prints nothing when the toolchain is complete:

```bash
ffmpeg -version >/dev/null 2>&1 || echo "missing: ffmpeg"
ffprobe -version >/dev/null 2>&1 || echo "missing: ffprobe"
whisper-cli --help >/dev/null 2>&1 || echo "missing: whisper-cli"
ls ~/.cache/whisper/ggml-*.bin >/dev/null 2>&1 || echo "missing: whisper model"
docling --version >/dev/null 2>&1 || echo "missing: docling"
```

If anything is missing — or a tool errors at runtime (e.g. docling's `libxcb` ImportError on NixOS) — read `environment-dependencies.md` in this skill for setup nuance before improvising.

**On a Nix system, do not hand-patch the toolchain.** The reproducible fix for missing tools *and* runtime library errors is the project's devenv: run ingest commands through `devenv shell -- <cmd>` (from the repo root, where `devenv.nix` lives). If `devenv.nix` is absent, create it first — `environment-dependencies.md` has the exact file — then re-run the preflight inside the shell:

```bash
devenv shell -- bash -c 'docling --version && ffmpeg -version >/dev/null && whisper-cli --help >/dev/null'
```

## Transcribing audio/video

1. Extract 16 kHz mono WAV (what whisper.cpp expects). The WAV is disposable — write it to a scratch directory, never commit it:

   ```bash
   ffmpeg -y -loglevel error -i <source>/audio.m4a -vn -ar 16000 -ac 1 <scratch>/audio.wav
   ```

2. Transcribe. Run as a **background task** — expect minutes of wall time per ten minutes of audio. For a two-person conversation, use the tinydiarize model with `--tinydiarize`, which inserts `[SPEAKER_TURN]` markers at speaker changes:

   ```bash
   whisper-cli -m ~/.cache/whisper/ggml-small.en-tdrz.bin --tinydiarize \
     -f <scratch>/audio.wav --output-srt --output-json --output-file <scratch>/transcript
   ```

   For single-speaker or accuracy-critical material, use `ggml-large-v3.bin` without `--tinydiarize`.

3. Verify: the SRT starts at `00:00:00` and spans the full duration (`ffprobe -show_format` on the original); spot-check segments against the audio.

4. Write `transcript.md` in the source directory: split the text at `[SPEAKER_TURN]` markers and label turns (`**Speaker A:**` / `**Speaker B:**`), keeping timestamps at reasonable intervals. Identify speakers by name in the text only when the user confirms who is who. Keep the raw SRT/JSON alongside as `transcript.srt` / `transcript.json` if useful.

### Diarization notes

- `--tinydiarize` (tdrz models) detects speaker *turns*, not identities, and works best for two speakers; it is English-only and exists only at `small.en-tdrz` quality. It marks where the speaker changes — attributing turns to A/B and keeping that assignment consistent is the agent's editing job.
- For more than two speakers or higher-fidelity attribution, use WhisperX (see `environment-dependencies.md`). Reach for this only when tinydiarize output is inadequate.

## Converting PDFs to markdown (docling)

```bash
docling <source>/source.pdf --to md --output <source>/ --image-export-mode placeholder
```

On a Nix system, run it inside the project devenv — `devenv shell -- docling <source>/source.pdf ...` — which supplies the system libraries the OpenCV wheel needs; see `environment-dependencies.md` to set the devenv up. The first run downloads models — run it as a background task.

Docling writes `<basename>.md`. Rename it to `content.md` — the searchable text representation. `source.md` is reserved for the source note (see below), so do not let docling's output take that name.

1. Verify the markdown against the PDF: section structure survived, tables are intact, no garbled OCR runs. Correct obvious OCR errors only where the source's meaning is unambiguous.
2. Keep the PDF canonical and untouched.
3. Default to `--image-export-mode placeholder` so the markdown stays text-sized; use `--image-export-mode referenced` to extract figures into the source's `figures/` directory when they matter.

## After ingesting — the source note

Every source directory must contain a `source.md` **note** that joins the Obsidian knowledge graph. Following the `wiki` skill:

1. Write `source.md` with frontmatter: `type: source`, `source_kind`, real provenance (authors, publication, `published` date, `doi`/URL), and **three to seven material tags** from the wiki namespaces so the source is graphed alongside concepts and problems. Check the existing tag taxonomy first (`obsidian tags counts sort=count`) and reuse tags. Never invent metadata — verify publication dates/DOIs against the artifact or an authoritative index.
2. In the body, summarize the source and add `[[wiki links]]` to related concepts, problems, and research.
3. Name the directory `<YYYY-MM-DD>-<slug>` using the artifact's publication/recording date.
4. Update `wiki/index.md` and append an `ingest` entry to `wiki/log.md`.

## Finally — check for new or updated concepts

Ingestion is not finished when the source note is written. As the **last step**, review what the source actually covers and reconcile it against `concepts/` (and `problems/`), because a source only earns its place in the graph once the durable ideas it introduces are captured:

1. List the substantive concepts the source explains — systems, organisms, processes, environments, metrics (e.g. a growth system, a cultivation method, a hazard).
2. For each, search existing notes and tags (`obsidian search`, `obsidian tags counts`). If a concept note exists, **update it**: add what the source contributes and a `[[wiki link]] `/ backlink to the source. If it does not and the concept is durable (not source-specific trivia), **create a stub concept note** per the `wiki` skill and link it both ways.
3. Prefer updating an existing note over creating a near-duplicate; reuse canonical tags.
4. Wiki-link the source note to every concept it touches, and append the concept `create`/`update` entries to `wiki/log.md`.

Only promote genuinely durable knowledge to `concepts/`; leave one-off details in the source note.
