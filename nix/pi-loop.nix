# Prebuilt Outfitter/Pi extension cache entry for @pi-agents/loop.
#
# Outfitter projects `npm:` extension loadouts by looking in its XDG cache and
# invokes `pi install` only when an entry is missing. Building the package with
# Nix and installing it at that exact cache-relative path lets the agent run
# with PI_OFFLINE=1: extension resolution is deterministic and startup never
# reaches npm.
{ lib, buildNpmPackage, fetchFromGitHub, importNpmLock }:

let
  src = fetchFromGitHub {
    owner = "ArtemisAI";
    repo = "pi-loop";
    rev = "9c72768c69934d811aaa8dea998a3c72e4ab5cb4";
    hash = "sha256-K6bvk4A/Kq6QQk+kZjHUh8li52Rg0XpMLeWXzGgkbvo=";
  };
  package = builtins.fromJSON (builtins.readFile "${src}/package.json");
in
buildNpmPackage {
  pname = "pi-agents-loop-cache";
  inherit (package) version;
  inherit src;

  npmDeps = importNpmLock { npmRoot = src; };
  npmConfigHook = importNpmLock.npmConfigHook;

  npmBuildScript = "build";

  installPhase = ''
    runHook preInstall

    extension_dir="$out/outfitter/pi-extensions/npm/node_modules/@pi-agents/loop"
    mkdir -p "$extension_dir"
    cp package.json README.md LICENSE "$extension_dir/"
    cp -r dist skills config "$extension_dir/"

    runHook postInstall
  '';

  meta = {
    description = "Prebuilt cache entry for the generic Pi recurring-prompt extension";
    homepage = "https://github.com/ArtemisAI/pi-loop";
    license = lib.licenses.mit;
  };
}
