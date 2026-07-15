# terraform/dns.tf
# Cloud DNS zone and records.
#
# One A record points server_fqdn at the VM's static IP — that hostname
# serves OSCAR (5190) and the signup site (443).
#
# The Resend records authenticate server_fqdn as a sending domain (pattern
# borrowed from flyspacea): DKIM TXT at resend._domainkey.<fqdn>, plus
# SPF TXT and MX on send.<fqdn> for Resend's Amazon SES infrastructure.
# They are created only once resend_dkim_key is set — add the domain in the
# Resend dashboard first, copy the DKIM key, then re-apply.
#
# NS and SOA records are auto-managed by Cloud DNS and not tracked here.

resource "google_dns_managed_zone" "aim" {
  count       = var.create_dns_zone ? 1 : 0
  name        = var.dns_zone_name
  dns_name    = "${var.dns_zone_root}."
  description = "AIM server DNS zone"
  project     = var.project_id
  visibility  = "public"

  depends_on = [google_project_service.apis]
}

locals {
  # Works whether the zone was created here or pre-exists under the same name.
  dns_zone = var.create_dns_zone ? google_dns_managed_zone.aim[0].name : var.dns_zone_name
}

# ─── A Record — the AIM server ─────────────────────────────────────────────

resource "google_dns_record_set" "aim_a" {
  managed_zone = local.dns_zone
  name         = "${var.server_fqdn}."
  type         = "A"
  ttl          = 300
  project      = var.project_id

  rrdatas = [google_compute_address.aim_server.address]
}

# ─── Resend Email Authentication ───────────────────────────────────────────

resource "google_dns_record_set" "resend_dkim" {
  count        = var.resend_dkim_key != "" ? 1 : 0
  managed_zone = local.dns_zone
  name         = "resend._domainkey.${var.server_fqdn}."
  type         = "TXT"
  ttl          = 300
  project      = var.project_id

  rrdatas = ["\"${var.resend_dkim_key}\""]
}

resource "google_dns_record_set" "resend_mx" {
  count        = var.resend_dkim_key != "" ? 1 : 0
  managed_zone = local.dns_zone
  name         = "send.${var.server_fqdn}."
  type         = "MX"
  ttl          = 300
  project      = var.project_id

  rrdatas = ["10 feedback-smtp.us-east-1.amazonses.com."]
}

resource "google_dns_record_set" "resend_spf" {
  count        = var.resend_dkim_key != "" ? 1 : 0
  managed_zone = local.dns_zone
  name         = "send.${var.server_fqdn}."
  type         = "TXT"
  ttl          = 300
  project      = var.project_id

  rrdatas = ["\"v=spf1 include:amazonses.com ~all\""]
}

# Monitor-only DMARC (p=none) — collects reports without rejecting mail.
resource "google_dns_record_set" "dmarc" {
  count        = var.resend_dkim_key != "" ? 1 : 0
  managed_zone = local.dns_zone
  name         = "_dmarc.${var.server_fqdn}."
  type         = "TXT"
  ttl          = 300
  project      = var.project_id

  rrdatas = ["\"v=DMARC1; p=none; rua=mailto:${var.alert_email}\""]
}

output "zone_nameservers" {
  description = "NS records to add at the parent domain to delegate the zone"
  value       = var.create_dns_zone ? google_dns_managed_zone.aim[0].name_servers : []
}
