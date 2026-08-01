{
  description = "Agent Operator — operator and agent-runtime packages";

  # Pinned to the same nixos-unstable rev already locked in devenv.lock so the
  # flake and the dev cluster share one nixpkgs.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/241313f4e8e508cb9b13278c2b0fa25b9ca27163";
  inputs.outfitter = {
    url = "github:ai-outfitter/outfitter/main";
    inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = inputs@{ nixpkgs, ... }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = f:
        nixpkgs.lib.genAttrs systems (system: f (import nixpkgs { inherit system; }));
    in
    {
      packages = forAllSystems (pkgs: rec {
        # Component packages (also useful for `nix build .#xin` etc.)
        xin = pkgs.callPackage ./nix/xin.nix { };
        channels = pkgs.callPackage ./nix/channels.nix { };
        outfitter = inputs.outfitter.packages.${pkgs.stdenv.hostPlatform.system}.outfitter;
        operator = pkgs.callPackage ./nix/operator.nix { };

        default = operator;
      });
    };
}
