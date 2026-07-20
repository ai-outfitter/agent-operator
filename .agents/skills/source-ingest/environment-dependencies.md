# Environment dependencies

Nuances for getting the source-ingest toolchain working. Read this whenever the preflight check in `SKILL.md` reports something missing, or a tool errors at runtime (e.g. docling's `libxcb` ImportError).

## On a Nix system: set up the project devenv first

If this is a Nix system (`nix` on PATH — check with `command -v nix`), do **not** improvise per-command `LD_LIBRARY_PATH` juggling or scatter `nix profile add` calls. Instead set up a [devenv](https://devenv.sh) (v2) at the **project root** that provides the whole ingest toolchain — ffmpeg, whisper.cpp, `uv`, and the system libraries the docling OpenCV wheel needs — then run every ingest tool inside it. This makes the toolchain reproducible and fixes the `libxcb`/`libGL`/`glib` wheel errors once, for everyone.

`command -v devenv` confirms devenv is installed (this machine has 2.1.2). If it is missing, install it: `nix profile add nixpkgs#devenv --impure`.

### Create the devenv files (only if the root has none)

Check `devenv.nix` at the repo root. If it already exists, add the packages/libs below to it rather than overwriting. Otherwise create both files.

`devenv.yaml`:

```yaml
inputs:
  nixpkgs:
    url: github:cachix/devenv-nixpkgs/rolling
```

`devenv.nix`:

```nix
{ pkgs, lib, ... }:

let
  # System libraries the docling OpenCV/manylinux wheels expect but NixOS
  # does not provide globally. Add more here if a new `libXXX.so` error appears.
  doclingLibs = [
    pkgs.libxcb
    pkgs.libglvnd   # libGL
    pkgs.glib
    pkgs.zlib
    pkgs.stdenv.cc.cc.lib   # libstdc++
  ];
in
{
  packages = [
    pkgs.ffmpeg
    pkgs.whisper-cpp   # provides whisper-cli
    pkgs.uv            # docling is installed through uv (nixpkgs#docling is broken)
  ] ++ doclingLibs;

  # docling's bundled wheels dlopen these at runtime.
  env.LD_LIBRARY_PATH = lib.makeLibraryPath doclingLibs;

  # Install docling into the uv tool store on first shell entry, idempotently.
  enterShell = ''
    command -v docling >/dev/null 2>&1 || uv tool install docling
  '';
}
```

### Run ingest tools inside the devenv

Prefix any ingest command with `devenv shell --`:

```bash
devenv shell -- docling <source>/source.pdf --to md --output <source>/ --image-export-mode placeholder
devenv shell -- whisper-cli -m ~/.cache/whisper/ggml-small.en-tdrz.bin --tinydiarize -f <scratch>/audio.wav ...
```

`devenv shell` sets `LD_LIBRARY_PATH` from the config, so no per-command library juggling is needed. The first `devenv shell` builds the environment (and runs `uv tool install docling`) — run it as a background task.

If a docling run still fails with a different `libXXX.so.N` ImportError, find the providing nixpkgs package and add it to `doclingLibs` in `devenv.nix`, then re-enter the shell.

## Fallback: no devenv (non-Nix, or devenv unavailable)

### ffmpeg and whisper.cpp

Install into the user profile:

```bash
NIXPKGS_ALLOW_UNFREE=1 nix profile add nixpkgs#ffmpeg nixpkgs#whisper-cpp --impure
```

- On a NixOS machine with a GPU, prefer `nixpkgs#whisper-cpp-vulkan` for hardware acceleration.
- The nixpkgs `obsidian`/media packages are unfree — hence `NIXPKGS_ALLOW_UNFREE=1 --impure` when needed.

### docling

`nixpkgs#docling` is currently unbuildable (`docling-parse` is marked broken in nixpkgs), so install through uv:

```bash
uv tool install docling      # installs docling + docling-tools into ~/.local/bin
```

Ensure `~/.local/bin` is on PATH.

On NixOS without the devenv, docling's bundled OpenCV wheel fails with `ImportError: libxcb.so.1 ...` because manylinux wheels expect system libraries NixOS does not provide globally. Supply them via `LD_LIBRARY_PATH` for the one command:

```bash
export DOCLING_LIBS="$(nix build --print-out-paths --no-link nixpkgs#libxcb.out)/lib:$(nix build --print-out-paths --no-link nixpkgs#libglvnd)/lib:$(nix build --print-out-paths --no-link nixpkgs#glib.out)/lib"
LD_LIBRARY_PATH="$DOCLING_LIBS" docling --version
```

Prefer the project devenv above — this manual path is only for systems where devenv is not available.

## Whisper models

Convention: `~/.cache/whisper/`. Model downloads are large — run them as background tasks.

```bash
mkdir -p ~/.cache/whisper

# Two-speaker diarization (tinydiarize), English-only, ~490 MB — default for meetings
curl -sL -o ~/.cache/whisper/ggml-small.en-tdrz.bin \
  https://huggingface.co/akashmjn/tinydiarize-whisper.cpp/resolve/main/ggml-small.en-tdrz.bin

# Best accuracy, no diarization, ~3 GB — ask before downloading
curl -sL -o ~/.cache/whisper/ggml-large-v3.bin \
  https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3.bin
```

## docling models

The first conversion downloads layout/OCR models into `~/.cache/docling/` and `~/.local/share/uv/tools/docling/.../rapidocr/models/` — run the first conversion as a background task.

## WhisperX (optional, >2 speakers)

Only needed when tinydiarize output is inadequate:

```bash
uvx --from whisperx whisperx <audio> --diarize --hf_token <token>
```

Requires a Hugging Face token with access accepted for the pyannote speaker-diarization models. On Nix, add `whisperx` needs the same `LD_LIBRARY_PATH` treatment — run it inside `devenv shell`.
