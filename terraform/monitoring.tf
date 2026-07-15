# terraform/monitoring.tf
# Uptime monitoring: a TCP check against the OSCAR port and an email alert
# when it fails. Free at this scale.

resource "google_monitoring_notification_channel" "email" {
  display_name = "AIM alerts"
  type         = "email"
  project      = var.project_id

  labels = {
    email_address = var.alert_email
  }

  depends_on = [google_project_service.apis]
}

# ─── OSCAR port reachability ───────────────────────────────────────────────
#
# Checks server_fqdn:5190 over TCP, so it also catches DNS breakage.

resource "google_monitoring_uptime_check_config" "oscar" {
  display_name = "oscar-5190"
  project      = var.project_id
  timeout      = "10s"
  period       = "300s"

  tcp_check {
    port = 5190
  }

  monitored_resource {
    type = "uptime_url"
    labels = {
      project_id = var.project_id
      host       = var.server_fqdn
    }
  }
}

resource "google_monitoring_alert_policy" "oscar_down" {
  display_name = "AIM server unreachable (OSCAR 5190)"
  project      = var.project_id
  combiner     = "OR"

  conditions {
    display_name = "Uptime check failing"

    condition_threshold {
      filter = format(
        "metric.type=\"monitoring.googleapis.com/uptime_check/check_passed\" AND metric.labels.check_id=\"%s\" AND resource.type=\"uptime_url\"",
        google_monitoring_uptime_check_config.oscar.uptime_check_id
      )
      comparison      = "COMPARISON_GT"
      threshold_value = 1
      duration        = "600s"

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_NEXT_OLDER"
        cross_series_reducer = "REDUCE_COUNT_FALSE"
        group_by_fields      = ["resource.label.host"]
      }

      trigger {
        count = 1
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]

  documentation {
    content = <<-EOT
      The OSCAR listener on ${var.server_fqdn}:5190 has been unreachable for
      10+ minutes. Check the VM (`gcloud compute ssh aim-server --tunnel-through-iap`)
      and the service (`systemctl status openoscar`).
    EOT
  }
}
