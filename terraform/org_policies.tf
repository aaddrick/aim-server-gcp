# terraform/org_policies.tf
# Project-level security guardrails (pattern borrowed from flyspacea).

# Forces all service account auth through OAuth2 tokens / attached service
# accounts. No downloadable JSON key files can be created.
resource "google_org_policy_policy" "disable_sa_keys" {
  count  = var.enable_org_policies ? 1 : 0
  name   = "projects/${data.google_project.current.number}/policies/iam.disableServiceAccountKeyCreation"
  parent = "projects/${data.google_project.current.number}"

  spec {
    rules {
      enforce = "TRUE"
    }
  }

  depends_on = [google_project_service.apis]
}

# All GCS access control must use IAM policies, never per-object ACLs.
resource "google_org_policy_policy" "uniform_bucket_access" {
  count  = var.enable_org_policies ? 1 : 0
  name   = "projects/${data.google_project.current.number}/policies/storage.uniformBucketLevelAccess"
  parent = "projects/${data.google_project.current.number}"

  spec {
    rules {
      enforce = "TRUE"
    }
  }

  depends_on = [google_project_service.apis]
}
