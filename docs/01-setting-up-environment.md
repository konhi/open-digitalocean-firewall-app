# Setting up the environment

Part of [Contributing to Open DigitalOcean Firewall App](../CONTRIBUTING.md).

## Development environment

We use a [Nix flake](../flake.nix) to provide the tools you need †([OpenTofu](https://opentofu.org), [`terraform-ls`](https://github.com/hashicorp/terraform-ls), [`tflint`](https://github.com/terraform-linters/tflint), [`doctl`](https://docs.digitalocean.com/reference/doctl/) and [`apm`](https://github.com/microsoft/apm)), and [direnv](https://direnv.net) to load them automatically when you enter the project directory.

## Setup

1. Install Nix with flakes enabled: see the [Determinate Nix installer](https://github.com/DeterminateSystems/nix-installer), or the [official Nix installation guide](https://nixos.org/download/) and [flakes documentation](https://nixos.wiki/wiki/Flakes#Enable_flakes_permanently).
2. Install direnv: see the [direnv installation guide](https://direnv.net/docs/installation.html), then [hook it into your shell](https://direnv.net/docs/hook.html).
3. Clone the repository (`https://github.com/konhi/open-digitalocean-firewall-app`) and run `direnv allow` in it. If you'd rather not use direnv, [`nix develop`](https://nix.dev/manual/nix/latest/command-ref/new-cli/nix3-develop) gives you the same shell.

> Nix flakes only see files that git tracks, so remember to `git add` new files before running Nix commands.
