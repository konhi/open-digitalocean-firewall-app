plugin "terraform" {
  enabled = true
  preset  = "all"
}

# Root configuration with fixed values, not a reusable module: no
# variables.tf or locals.tf needed.
rule "terraform_standard_module_structure" {
  enabled = false
}
