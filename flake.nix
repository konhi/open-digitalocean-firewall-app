{
  description = "Open DigitalOcean Firewall App";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    llm-agents.url = "github:numtide/llm-agents.nix";
  };

  outputs = { self, nixpkgs, flake-utils, llm-agents }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            opentofu
            terraform-ls
            tflint
            doctl
            llm-agents.packages.${system}.apm
          ];

          # Regenerate AGENTS.md from .apm/ whenever the dev shell is entered (direnv).
          shellHook = ''
            apm compile >/dev/null 2>&1 || echo "warning: 'apm compile' failed; run it manually" >&2
          '';
        };

        formatter = pkgs.nixpkgs-fmt;
      });
}
