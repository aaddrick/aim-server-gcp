# terraform/compute.tf
# The AIM server VM, its service account, static IP, and firewall rules.
#
# The VM is an e2-micro in a free-tier region running Debian on the default
# VPC network. The only billed component is the external IPv4 address
# (~$3/month). Provisioning of the software on the VM is done by
# deploy/setup.sh, not by Terraform.

# ─── Service Account ───────────────────────────────────────────────────────
#
# Dedicated SA for the VM. Grants are the minimum the box needs: write logs
# and metrics, upload backups to its bucket, and read the Resend API key.

resource "google_service_account" "aim_server" {
  account_id   = "aim-server"
  display_name = "AIM server VM"
  project      = var.project_id
}

resource "google_project_iam_member" "aim_server_log_writer" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.aim_server.email}"
}

resource "google_project_iam_member" "aim_server_metric_writer" {
  project = var.project_id
  role    = "roles/monitoring.metricWriter"
  member  = "serviceAccount:${google_service_account.aim_server.email}"
}

# ─── Static External IP ────────────────────────────────────────────────────
#
# Reserved so the DNS A record survives VM re-creation. An in-use static IP
# costs the same as an ephemeral one; only unattached IPs cost extra.

resource "google_compute_address" "aim_server" {
  name    = "aim-server"
  project = var.project_id
  region  = var.region
}

# ─── VM ────────────────────────────────────────────────────────────────────

resource "google_compute_instance" "aim_server" {
  name         = "aim-server"
  project      = var.project_id
  zone         = var.zone
  machine_type = var.machine_type
  tags         = ["aim-server"]

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-13"
      size  = 30 # free-tier allowance for standard PD
      type  = "pd-standard"
    }
  }

  network_interface {
    network = "default"
    access_config {
      nat_ip = google_compute_address.aim_server.address
    }
  }

  service_account {
    email  = google_service_account.aim_server.email
    scopes = ["cloud-platform"] # access governed by IAM roles, not scopes
  }

  # Deleting the VM should never be casual — it holds the SQLite database.
  deletion_protection = true

  allow_stopping_for_update = true

  depends_on = [google_project_service.apis]
}

# ─── Firewall ──────────────────────────────────────────────────────────────
#
# The default VPC ships with default-allow-ssh (0.0.0.0/0). These rules are
# scoped to the aim-server tag; SSH is restricted to IAP unless overridden.

resource "google_compute_firewall" "allow_oscar" {
  name    = "aim-allow-oscar"
  project = var.project_id
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["5190"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["aim-server"]
}

# Signup site (Caddy terminates TLS, proxies to the signup service).
resource "google_compute_firewall" "allow_web" {
  name    = "aim-allow-web"
  project = var.project_id
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["80", "443"] # 80 stays open for ACME HTTP-01 + redirect
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["aim-server"]
}

resource "google_compute_firewall" "allow_toc" {
  count   = var.enable_toc ? 1 : 0
  name    = "aim-allow-toc"
  project = var.project_id
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["9898"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["aim-server"]
}

resource "google_compute_firewall" "allow_ssh_iap" {
  name    = "aim-allow-ssh"
  project = var.project_id
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = var.ssh_source_ranges
  target_tags   = ["aim-server"]
}

# ─── Outputs ───────────────────────────────────────────────────────────────

output "server_ip" {
  description = "Static external IP of the AIM server"
  value       = google_compute_address.aim_server.address
}

output "server_fqdn" {
  description = "Hostname clients connect to"
  value       = var.server_fqdn
}
