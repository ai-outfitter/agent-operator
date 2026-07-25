# link-operator — the Kubernetes controller manager.
#
# NOTE (in-env): replace `vendorHash` with the value reported by the first
# `nix build .#operator`. Requires a nixpkgs pin that ships Go 1.26 (the module
# declares `go 1.26.0`); if the pin is older, bump it or add a Go overlay.
{ lib, buildGoModule }:

buildGoModule {
  pname = "link-operator";
  version = "0.1.0";

  # Kubernetes manifests and local build artifacts do not affect the Go
  # binary. Excluding them keeps image rebuilds cached while iterating on
  # deployment configuration.
  src = lib.cleanSourceWith {
    src = ../code/operator;
    filter = path: type:
      type == "directory"
      || lib.hasSuffix ".go" path
      || builtins.elem (baseNameOf path) [ "go.mod" "go.sum" ];
  };

  vendorHash = "sha256-XSWt//AB/jLz0BjHHtjH/ai6gase4JfNB4bBHhgewew=";

  subPackages = [ "cmd" ];

  # The main package lives at ./cmd, so the binary is named `cmd`; the image
  # entrypoint (and the upstream Dockerfile) expect `/bin/manager`.
  postInstall = ''
    mv "$out/bin/cmd" "$out/bin/manager"
  '';

  meta = {
    description = "Link Operator Kubernetes controller";
    mainProgram = "manager";
  };
}
