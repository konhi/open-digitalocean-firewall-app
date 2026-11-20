---
description: Nix environment and tooling rules
applyTo: "**"
---
# Environment & Tooling

Nix is installed on this system. Do not install packages globally with apt, brew, npm -g, or pip. Shell should be automatically ran if using an agent with properly configured `direnv`.

- To run a missing CLI tool once: `nix run nixpkgs#<package> -- <args>`
- To search for package names: `nix search nixpkgs <query>`
- To see available packages, analyse `flake.nix`
