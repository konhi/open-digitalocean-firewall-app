# Testing

Part of [Contributing to Open DigitalOcean Firewall App](../CONTRIBUTING.md).

Open DigitalOcean Firewall App is tested against a DigitalOcean project created with IaC ([OpenTofu](https://opentofu.org)). Valkey was chosen as the engine for convenience: it is the cheapest and fastest managed database to provision.
## Test infrastructure

The configuration lives in [`infra/testing`](../infra/testing) and creates:

| Resource | Configuration |
| --- | --- |
| Project | `Open DigitalOcean Firewall App (testing)`, environment `Development` |
| Valkey database cluster | `open-digitalocean-firewall-app-testing-valkey`, Valkey 8, `db-s-1vcpu-1gb`, 1 node, `fra1` |
| Database firewall | One trusted source, `ip_addr` `192.0.2.1` |

Values are fixed in [`main.tf`](../infra/testing/main.tf); the setup only exists for testing, so there is nothing to configure.

### Database firewall

A DigitalOcean database with no trusted sources accepts connections from any IP. To block all connections by default, OpenTofu adds a single trusted source for `192.0.2.1`. This address is in TEST-NET-1, which [RFC 5737](https://www.rfc-editor.org/rfc/rfc5737) reserves for documentation, so it is never routed and nobody can connect from it.

Open DigitalOcean Firewall App then adds and removes its own trusted sources next to it. OpenTofu ignores changes to the rules (`lifecycle { ignore_changes = [rule] }`), so `tofu plan` and `tofu apply` leave them alone.

> [!WARNING]
> Keep the `192.0.2.1` rule. If every trusted source is removed, the database accepts connections from any IP again. `tofu apply` won't put the rule back, because it ignores rule changes. Use `doctl databases firewalls append <cluster-id> --rule ip_addr:192.0.2.1`, or `tofu apply -replace=digitalocean_database_firewall.valkey`, which also removes the app's rules.

### Prerequisites

- The development shell from [01-setting-up-environment.md](01-setting-up-environment.md).
- A `DIGITALOCEAN_TOKEN` with the scopes from [02-setting-up-envs.md](02-setting-up-envs.md), and `doctl` signed in with the same token.

### Create

```sh
cd infra/testing
tofu init
tofu plan -out=testing.tfplan
tofu apply testing.tfplan
```

Creating the database takes around 5 minutes. State is stored locally in `infra/testing/terraform.tfstate`, which is git-ignored and contains the database credentials.

### Verify

```sh
doctl projects list
doctl projects resources list "$(tofu output -raw project_id)"
doctl databases get "$(tofu output -raw database_cluster_id)"
doctl databases firewalls list "$(tofu output -raw database_cluster_id)"
```

The project should list the cluster as `do:dbaas:<id>`, the cluster status should be `online`, and the firewall should list only `192.0.2.1`. `tofu plan` should report `No changes.`

To check that the database is unreachable, without needing credentials:

```sh
nix shell nixpkgs#valkey -c valkey-cli --tls -t 10 \
  -h "$(tofu output -raw database_host)" -p "$(tofu output -raw database_port)" PING
```

`Operation timed out` means the firewall blocked you. `NOAUTH Authentication required.` means you got through. That's expected once your IP is a trusted source, and a problem otherwise.

`tofu output -raw database_uri` prints the connection string. It only includes the password if the token has `database:view_credentials`.

### Destroy

Always tear down the test infrastructure when done testing so database instances don't incur ongoing charges:

```sh
cd infra/testing
tofu destroy -auto-approve
```

Without the `:delete` scopes the API returns `403 You are not authorized to perform this operation` and nothing is deleted. A failed destroy still clears the outputs in state; restore them with `tofu apply -refresh-only`.

### Checks

Run these before committing changes to `infra/`:

```sh
tofu fmt -check -recursive
tofu validate
tflint
```
