# terraform/secrets.tf
# Secret Manager containers. Terraform manages secret STRUCTURE only —
# values never enter terraform state. Set the value out-of-band:
#
#   printf '%s' 're_xxxxxxxx' | gcloud secrets versions add RESEND_API_KEY --data-file=-

resource "google_secret_manager_secret" "resend_api_key" {
  secret_id = "RESEND_API_KEY"
  project   = var.project_id

  annotations = {
    description = "Resend transactional email API key (signup verification emails)"
  }

  labels = {
    project     = "aim"
    environment = var.environment
    managed_by  = "terraform"
  }

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

# The VM reads the key at signup-service start.
resource "google_secret_manager_secret_iam_member" "resend_api_key_vm" {
  secret_id = google_secret_manager_secret.resend_api_key.secret_id
  project   = var.project_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.aim_server.email}"
}
