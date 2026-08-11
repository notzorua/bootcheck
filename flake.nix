{
  description = "Check whether a NixOS machine will boot, before you reboot it";

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
      packages = forAllSystems (pkgs: rec {
        bootcheck = pkgs.callPackage ./default.nix { };
        default = bootcheck;
      });

      # Lets other configurations pull the package in as pkgs.bootcheck.
      overlays.default = _final: prev: {
        bootcheck = prev.callPackage ./default.nix { };
      };

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.gotools
          ];
        };
      });

      checks = forAllSystems (pkgs: {
        build = self.packages.${pkgs.stdenv.hostPlatform.system}.bootcheck;
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);
    };
}
