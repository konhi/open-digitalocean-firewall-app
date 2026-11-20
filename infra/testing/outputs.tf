output "database_cluster_id" {
  description = "ID of the Valkey database cluster"
  value       = digitalocean_database_cluster.valkey.id
}

output "database_cluster_name" {
  description = "Name of the Valkey database cluster"
  value       = digitalocean_database_cluster.valkey.name
}

output "database_host" {
  description = "Public hostname of the Valkey database cluster"
  value       = digitalocean_database_cluster.valkey.host
}

output "database_port" {
  description = "Port of the Valkey database cluster"
  value       = digitalocean_database_cluster.valkey.port
}

output "database_uri" {
  description = "Full connection URI of the Valkey database cluster, including credentials"
  value       = digitalocean_database_cluster.valkey.uri
  sensitive   = true
}

output "project_id" {
  description = "ID of the DigitalOcean project holding the test resources"
  value       = digitalocean_project.main.id
}
