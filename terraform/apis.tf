# terraform/apis.tf
# Required GCP API enablements.
#
# disable_on_destroy = false prevents `terraform destroy` from disabling
# APIs that other resources or manual configuration depend on.

locals {
  required_apis = [
    "compute.googleapis.com",             # Compute Engine (VM, firewall, static IP)
    "dns.googleapis.com",                 # Cloud DNS
    "secretmanager.googleapis.com",       # Secret Manager (Resend API key)
    "monitoring.googleapis.com",          # Cloud Monitoring (uptime check, alerts)
    "logging.googleapis.com",             # Cloud Logging
    "storage.googleapis.com",             # Cloud Storage (backups, tf state)
    "orgpolicy.googleapis.com",           # Org Policy guardrails
    "iam.googleapis.com",                 # Service account management
    "cloudresourcemanager.googleapis.com" # Required by the Terraform provider
  ]
}

resource "google_project_service" "apis" {
  for_each = toset(local.required_apis)

  project            = var.project_id
  service            = each.key
  disable_on_destroy = false
}
