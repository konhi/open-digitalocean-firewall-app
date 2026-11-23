# Open DigitalOcean Firewall App

Open DigitalOcean Firewall App lets you easily add your device IPs to managed databases network access list in DigitalOcean from your tray menu. This is faster and more convenient than doing it manually via dashboard or `doctl`.

This is extremely useful for letting your AI agents use MCPs and work with your deployed databases.

## Features

- **Cross-platform system tray app** (macOS, Linux, Windows) built with Go and `fyne.io/systray`.
- **Per-database toggles** directly in the tray menu, plus **Allow My IP on All**, **Remove My IP from All**, and **Refresh**.
- **Multi-user safe**: tracks the exact IP added by this app per cluster and always fetches the latest rules before updating, so other users' IPs and non-IP rules (Droplets, tags, Kubernetes clusters, App Platform apps, or sentinel rules like `192.0.2.1`) are never disturbed.
- **Auto-update on IP change**: automatically replaces your previously added IP with your new public IPv4 address when switching networks.
- **Temporary access timers**: choose `15 minutes`, `1 hour`, `4 hours`, or `Until quit (no timer)` to automatically revoke access.
- **Auto-cleanup on quit**: removes IPs added by this app when quitting (configurable in Settings).
- **Secure token storage**: reads `DIGITALOCEAN_TOKEN` from the environment if set, otherwise stores and loads your token from the OS keyring (macOS Keychain, Linux Secret Service, Windows Credential Manager) with a native GUI prompt on first run.

## Running

```sh
go run ./cmd/open-digitalocean-firewall-app
```

Or build a binary:

```sh
go build -o bin/open-digitalocean-firewall-app ./cmd/open-digitalocean-firewall-app
./bin/open-digitalocean-firewall-app
```

On first launch, if `DIGITALOCEAN_TOKEN` is not set and no token is saved in your OS keyring, Open DigitalOcean Firewall App will prompt you for your DigitalOcean personal access token (`database:read` and `database:update` scopes).

## Development & Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for environment setup, DigitalOcean token scopes, and testing instructions.
