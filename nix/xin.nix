# xin — agent-first JMAP email CLI (github.com/onevcat/xin).
#
# Built from a pinned revision until it is packaged in nixpkgs. TLS is rustls,
# so no OpenSSL is required.
#
# NOTE (in-env): the two `lib.fakeHash` placeholders below must be replaced with
# the real hashes reported by the first `nix build .#xin`:
#   1. `src.hash` — the fetchFromGitHub content hash.
#   2. `cargoLock.outputHashes."jmap-client-<version>"` — for the git fork the
#      repo pins via `[patch.crates-io]` (github.com/onevcat/jmap-client @ 61a2304).
#      The exact key is "<name>-<version>" as it appears in xin's Cargo.lock.
{ lib, rustPlatform, fetchFromGitHub }:

rustPlatform.buildRustPackage rec {
  pname = "xin";
  version = "0.1.3-unstable-2026-02-17";

  src = fetchFromGitHub {
    owner = "onevcat";
    repo = "xin";
    rev = "cb348b24735d2bddbbc56a8b86f5c4bcc136b802";
    hash = "sha256-lFdQeu/ILLf5NgRk/crKxUY7R2wxRpHmR/5q7snlKtQ=";
  };

  cargoLock = {
    lockFile = "${src}/Cargo.lock";
    outputHashes = {
      # TODO(in-env): key is "jmap-client-<version-in-Cargo.lock>"; value is the
      # hash reported by the first build.
      "jmap-client-0.4.0" = "sha256-O9IVECsNgulx6YJtitiwf4ii95J5wpstUCQcg54QB2I=";
    };
  };

  # Only the main binary is needed in the image (skip xin-feature test harness).
  cargoBuildFlags = [ "--bin" "xin" ];
  doCheck = false;

  meta = {
    description = "Agent-first JMAP email CLI";
    homepage = "https://github.com/onevcat/xin";
    mainProgram = "xin";
    license = lib.licenses.mit;
  };
}
