# terraform/main.tf
# Core Terraform config for the AIM server project.
#
# State lives in a GCS bucket with versioning enabled (required for the GCS
# backend's native state locking and for state recovery). Bootstrap the
# bucket once before `terraform init` — see README.md.
#
# Initialize with a partial backend config:
#   terraform init -backend-config=backends/prod.hcl

terraform {
  required_version = ">= 1.7"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }

  backend "gcs" {}
}

provider "google" {
  project               = var.project_id
  region                = var.region
  user_project_override = true
  billing_project       = var.project_id
}

# Looked up from project_id so the project number never has to be typed
# into tfvars. depends_on defers the read to apply time so a fresh project
# doesn't chicken-and-egg on the Resource Manager API that apis.tf enables.
data "google_project" "current" {
  project_id = var.project_id
  depends_on = [google_project_service.apis]
}
