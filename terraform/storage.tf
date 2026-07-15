# terraform/storage.tf
# GCS bucket for SQLite backups uploaded by deploy/backup.sh on the VM.

resource "google_storage_bucket" "backups" {
  name     = "${var.project_id}-aim-backups"
  project  = var.project_id
  location = var.region

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      age = var.backup_retention_days
    }
    action {
      type = "Delete"
    }
  }

  depends_on = [google_project_service.apis]
}

resource "google_storage_bucket_iam_member" "backups_vm_writer" {
  bucket = google_storage_bucket.backups.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.aim_server.email}"
}

output "backup_bucket" {
  description = "GCS bucket receiving SQLite backups"
  value       = google_storage_bucket.backups.name
}
