---
trigger: glob
globs: "**"
---

# Project Rules

- The project name is always written **Open DigitalOcean Firewall App**, with exactly this capitalization, in code, docs, comments and UI text.
- Always destroy test infrastructure (`cd infra/testing && tofu destroy -auto-approve`) after finishing tests or tasks so database resources are not left running and incurring costs.
