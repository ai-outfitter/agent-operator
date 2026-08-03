# chrome-devtools-mcp — Chrome DevTools MCP server
# (github.com/ChromeDevTools/chrome-devtools-mcp).
#
# Baked into the agent image so a resident agent can attach to the browser
# sidecar's CDP endpoint (AGENT_BROWSER_CDP_URL) with PI_OFFLINE=1 and no
# runtime npm fetch. Installed from the published npm tarball, which ships
# the compiled build/ output: building from the GitHub source fails on a
# type collision between the vendored chrome-devtools-frontend sources and
# @paulirish/trace_engine. The lockfile project in ./chrome-devtools-mcp/
# pins the tarball and its transitive dependencies; regenerate it with
# `npm install --package-lock-only` after bumping the version there.
{ lib, buildNpmPackage, nodejs_22 }:

buildNpmPackage {
  pname = "chrome-devtools-mcp";
  version = "1.6.0";

  src = ./chrome-devtools-mcp;
  nodejs = nodejs_22;

  npmDepsHash = "sha256-PlYaUaz+6aCtXh4jW+sH7eN/zsu6U/SJGzcbCAUBVyY=";

  # The server never downloads a browser: the sidecar supplies Chrome over
  # --browser-url, so skip puppeteer's install script.
  npmFlags = [ "--ignore-scripts" ];
  env.PUPPETEER_SKIP_DOWNLOAD = "1";
  dontNpmBuild = true;
  dontNpmPrune = true;

  installPhase = ''
    runHook preInstall

    mkdir -p "$out/lib" "$out/bin"
    cp -r node_modules "$out/lib/node_modules"
    makeWrapper ${lib.getExe nodejs_22} "$out/bin/chrome-devtools-mcp" \
      --add-flags "$out/lib/node_modules/chrome-devtools-mcp/build/src/bin/chrome-devtools-mcp.js"

    runHook postInstall
  '';

  meta = {
    description = "MCP server exposing Chrome DevTools to coding agents";
    homepage = "https://github.com/ChromeDevTools/chrome-devtools-mcp";
    mainProgram = "chrome-devtools-mcp";
    license = lib.licenses.asl20;
  };
}
