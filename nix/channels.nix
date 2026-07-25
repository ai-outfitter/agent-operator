# Prebuilt Outfitter/Pi extension cache entry for ai-outfitter/channels.
#
# Outfitter resolves git extensions under the XDG cache at
# pi-extensions/git/<host>/<owner>/<repo>. Keeping the immutable Channels
# checkout and its production dependencies at that exact path lets resident
# agents start with PI_OFFLINE=1 and never fetch executable code at runtime.
{ lib, buildNpmPackage, fetchFromGitHub }:

let
  revision = "cac964724f149208a4d0fe2aca39e3e0a234045d";
  src = fetchFromGitHub {
    owner = "ai-outfitter";
    repo = "channels";
    rev = revision;
    hash = "sha256-OtOus6JkveNAGETcRXFWhn4hscpD+C8CpifnlwaQz3Q=";
  };
  package = builtins.fromJSON (builtins.readFile "${src}/package.json");
in
buildNpmPackage {
  pname = "outfitter-channels-cache";
  inherit (package) version;
  inherit src;

  # The upstream lock embeds npm's peer-package shrinkwrap, whose three nested
  # records omit integrity fields. npm accepts those records, but Nix's npm
  # fetcher correctly requires integrity for every registry tarball. Patch in
  # the registry-published SRI values before both dependency fetching and npm
  # installation; omit the Pi peer because Outfitter supplies it at runtime.
  postPatch = ''
    substituteInPlace package-lock.json \
      --replace-fail \
        '"resolved": "https://registry.npmjs.org/@earendil-works/pi-agent-core/-/pi-agent-core-0.81.1.tgz",' \
        '"resolved": "https://registry.npmjs.org/@earendil-works/pi-agent-core/-/pi-agent-core-0.81.1.tgz", "integrity": "sha512-yqbh68CyhqxMov/jUogFJfMqlu2Gd37GAki+tr59YCmAPHfomiCA5ESzusXtpGzABeiZFC/OrRdQ4GwCCOMIHA==",'
    substituteInPlace package-lock.json \
      --replace-fail \
        '"resolved": "https://registry.npmjs.org/@earendil-works/pi-ai/-/pi-ai-0.81.1.tgz",' \
        '"resolved": "https://registry.npmjs.org/@earendil-works/pi-ai/-/pi-ai-0.81.1.tgz", "integrity": "sha512-hzHE7Z8l5mgJk+ke67Lge0rwS2+wbKJrFKl9o5M1R1rh33+cCT7D1AHz1OAtX5wFs90E1/BTGhyJRTUHaMxGvQ==",'
    substituteInPlace package-lock.json \
      --replace-fail \
        '"resolved": "https://registry.npmjs.org/@earendil-works/pi-tui/-/pi-tui-0.81.1.tgz",' \
        '"resolved": "https://registry.npmjs.org/@earendil-works/pi-tui/-/pi-tui-0.81.1.tgz", "integrity": "sha512-OMEe+Zt8oQYi/rCq3upxsTlIScWL0FPhXwQus34TbQb3EmTx88S7Uzx32JxvQiEeWOw8eDCdJf2PBUBE9r6wIg==",'
  '';
  npmDepsHash = "sha256-Y2lkvsmuLkXKNlvg/uMj8tiH3So2D6TBk95SjkRysI4=";
  npmFlags = [ "--omit=dev" "--omit=peer" "--ignore-scripts" ];
  dontNpmBuild = true;

  installPhase = ''
    runHook preInstall

    extension_dir="$out/outfitter/pi-extensions/git/github.com/ai-outfitter/channels"
    mkdir -p "$extension_dir"
    cp package.json package-lock.json README.md "$extension_dir/"
    cp -r extensions node_modules "$extension_dir/"
    printf '%s\n' ${lib.escapeShellArg revision} > "$extension_dir/REVISION"

    runHook postInstall
  '';

  meta = {
    description = "Offline cache entry for the Outfitter Channels Pi extension";
    homepage = "https://github.com/ai-outfitter/channels";
    license = lib.licenses.mit;
  };
}
