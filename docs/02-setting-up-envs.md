# Setting up environment variables

Part of [Contributing to Open DigitalOcean Firewall App](../CONTRIBUTING.md).

## DigitalOcean API token

Open DigitalOcean Firewall App talks to DigitalOcean with a personal access token, which is read from the `DIGITALOCEAN_TOKEN` environment variable (also used by the OpenTofu DigitalOcean provider). DigitalOcean has no API for creating these tokens, so you create one in the control panel.

### Create the token

1. Open <https://cloud.digitalocean.com/account/api/tokens/new>.
2. Give it a name and an expiration, then choose **Custom Scopes**.
3. Enable the scopes below and generate the token. DigitalOcean only shows it once.

| Scope | Used by | Why |
| --- | --- | --- |
| `database:read` | App, OpenTofu, `doctl` | List database clusters and read their trusted sources (firewall rules); refresh OpenTofu state |
| `database:update` | App, OpenTofu | Add and remove trusted sources. OpenTofu uses it once to seed the default deny rule (see [03-testing.md](03-testing.md)) |
| `database:create` | OpenTofu | Create the Valkey test cluster |
| `database:delete` | OpenTofu | `tofu destroy` the Valkey test cluster |
| `project:read` | OpenTofu, `doctl` | Refresh OpenTofu state and list the project's resources |
| `project:create` | OpenTofu | Create the test project |
| `project:update` | OpenTofu | Assign the database cluster to the test project |
| `project:delete` | OpenTofu | `tofu destroy` the test project |
| `regions:read`, `sizes:read` | — | Selected automatically by the control panel together with the database scopes |

The app itself only needs `database:read` and `database:update`. The other scopes are for the [test infrastructure](03-testing.md).

Trusted sources that reference Droplets, tags, Kubernetes clusters or App Platform apps can only be resolved to names with the matching `:read` scope (e.g. `droplet:read`, `tag:read`).

### Store the token

Copy [`.env.local.example`](../.env.local.example) to `.env.local` and paste the token in. `.env.local` is git-ignored and direnv loads it automatically.

`doctl` does not read `DIGITALOCEAN_TOKEN`; it uses its own stored credentials. Point it at the same token:

```sh
doctl auth init --access-token "$DIGITALOCEAN_TOKEN"
```
