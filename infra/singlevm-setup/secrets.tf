# IAM bindings so the VM's service account can read secrets from Secret Manager.
# The secrets themselves are created manually (they contain sensitive values).

locals {
  vm_service_account = "serviceAccount:710670943493-compute@developer.gserviceaccount.com"
  secrets = [
    "github-pat",
    "cloudflare-dns-token",
    "attlas-server-config",
    "openclaw-config",
  ]
}

data "google_secret_manager_secret" "secrets" {
  for_each  = toset(local.secrets)
  secret_id = each.value
}

resource "google_secret_manager_secret_iam_member" "vm_access" {
  for_each  = toset(local.secrets)
  secret_id = data.google_secret_manager_secret.secrets[each.value].id
  role      = "roles/secretmanager.secretAccessor"
  member    = local.vm_service_account
}
