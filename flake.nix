{
  description = "tether — Gmail/Calendar → LLM triage comms hub";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "tether";
          version = "0.1.0";
          src = ./.;
          vendorHash = null;
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}
