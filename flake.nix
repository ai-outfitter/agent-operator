{
  description = "Agent Operator — operator and agent-runtime packages";

  # Pinned to the same nixos-unstable rev already locked in devenv.lock so the
  # flake and the dev cluster share one nixpkgs.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/241313f4e8e508cb9b13278c2b0fa25b9ca27163";
  # Pinned to a release tag, not a branch. Tracking `main` reads as "always
  # current" while flake.lock silently freezes it: this input sat on the commit
  # that first packaged Outfitter as a flake (2026-07-21, v0.11.0) for six weeks
  # while Outfitter reached 1.3.1, and nothing in this repository would have
  # moved it. A tag makes the version legible in review, so a bump is a
  # one-line diff rather than a lockfile churn nobody reads.
  #
  # 0.11.0 could not project the `mcp` loadout element for the pi harness, so
  # any profile declaring `mcp:` failed `outfitter run --strict`. That is why
  # this bump matters beyond hygiene.
  inputs.outfitter = {
    url = "github:ai-outfitter/outfitter/v1.3.1";
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
