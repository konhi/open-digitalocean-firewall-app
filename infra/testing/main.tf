resource "digitalocean_project" "main" {
  name        = "Open DigitalOcean Firewall App (testing)"
  description = "Resources used to test Open DigitalOcean Firewall App. Managed by OpenTofu in infra/testing."
  purpose     = "Operational / Developer tooling"
  environment = "Development"
}

resource "digitalocean_database_cluster" "valkey" {
  name       = "open-digitalocean-firewall-app-testing-valkey"
  engine     = "valkey"
  version    = "8"
  size       = "db-s-1vcpu-1gb"
  region     = "fra1"
  node_count = 1
  project_id = digitalocean_project.main.id
}

# A cluster without trusted sources accepts connections from any IP. This seeds
# a single rule for 192.0.2.1 (TEST-NET-1, reserved for documentation by
# RFC 5737 and never routed), so by default nothing can connect.
#
# Later rules are added and removed by Open DigitalOcean Firewall App, so
# OpenTofu ignores changes to them instead of reverting them.
resource "digitalocean_database_firewall" "valkey" {
  cluster_id = digitalocean_database_cluster.valkey.id

  rule {
    type  = "ip_addr"
    value = "192.0.2.1"
  }

  lifecycle {
    ignore_changes = [rule]
  }
}
