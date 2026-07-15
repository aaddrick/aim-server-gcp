# terraform/variables.tf
# Shared variable declarations. Values are set in terraform.tfvars
# (copy terraform.tfvars.example and fill in your own).

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region. us-west1, us-central1, or us-east1 for the always-free e2-micro."
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "GCP zone for the VM. Must be inside var.region."
  type        = string
  default     = "us-central1-a"
}

variable "environment" {
  description = "Deployment environment label"
  type        = string
  default     = "prod"
}

variable "machine_type" {
  description = "VM machine type. e2-micro is always-free in us-west1/us-central1/us-east1."
  type        = string
  default     = "e2-micro"
}

variable "server_fqdn" {
  description = "Public hostname of the AIM server (OSCAR on 5190, signup site on 443), e.g. aim.example.com"
  type        = string
}

variable "dns_zone_root" {
  description = "Root domain of the Cloud DNS zone, e.g. example.com. server_fqdn must live under it."
  type        = string
}

variable "dns_zone_name" {
  description = "Cloud DNS managed zone name (GCP resource name, not the domain)"
  type        = string
  default     = "aim"
}

variable "create_dns_zone" {
  description = "Create the Cloud DNS zone. Set false to manage records in an existing zone named var.dns_zone_name."
  type        = bool
  default     = true
}

variable "resend_dkim_key" {
  description = <<-EOT
    DKIM public key from the Resend dashboard for the server_fqdn domain,
    including the leading "p=". Leave empty to skip creating the Resend DNS
    records (e.g. before the domain is added in Resend).
  EOT
  type        = string
  default     = ""
}

variable "alert_email" {
  description = "Email address for monitoring alerts and DMARC aggregate reports"
  type        = string
}

variable "enable_toc" {
  description = "Open firewall port 9898 for the TOC protocol (third-party/legacy clients)"
  type        = bool
  default     = false
}

variable "ssh_source_ranges" {
  description = "CIDR ranges allowed to SSH to the VM. Default is IAP's range — use `gcloud compute ssh --tunnel-through-iap`."
  type        = list(string)
  default     = ["35.235.240.0/20"]
}

variable "enable_org_policies" {
  description = "Apply project-level org policy guardrails. Requires the project to belong to a GCP organization — standalone/personal projects cannot set org policies."
  type        = bool
  default     = false
}

variable "backup_retention_days" {
  description = "Days to keep SQLite backups in the GCS bucket"
  type        = number
  default     = 90
}
