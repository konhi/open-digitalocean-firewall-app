# Contributing to Open DigitalOcean Firewall App

## Development environment

We use a [Nix flake](flake.nix) to provide the tools you need (OpenTofu, `terraform-ls`, `tflint` and `doctl`), and [direnv](https://direnv.net) to load them automatically when you enter the project directory.

### 1. Install Nix with flakes

The easiest way is the [Determinate Nix installer](https://github.com/DeterminateSystems/nix-installer), which enables flakes out of the box:

```sh
curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install
```

If you already have Nix installed, you can turn on flakes by adding this line to `~/.config/nix/nix.conf`:

```
experimental-features = nix-command flakes
```

### 2. Install direnv

Install direnv with your package manager (or with `nix profile install nixpkgs#direnv nixpkgs#nix-direnv`), then [hook it into your shell](https://direnv.net/docs/hook.html).

### 3. Load the environment

```sh
git clone https://github.com/konhi/open-digitalocean-firewall-app
cd open-digitalocean-firewall-app
direnv allow
```

From now on, the tools are available whenever you're inside the project directory. If you'd rather not use direnv, run `nix develop` to get the same shell.

> Nix flakes only see files that git tracks, so remember to `git add` new files before running Nix commands.

### Everyday use

```sh
tofu init
tofu plan
```

To update the pinned dependencies, run `nix flake update` and commit the changed `flake.lock`.
