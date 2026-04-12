# Session transcript storage.
#
# Claude Code session JSONL files are uploaded here by the diary
# wrap-up flow instead of being committed to git. The bucket is private
# (uniform bucket-level access, no public access) so transcripts don't
# need redaction — they're only readable by the VM's service account.
#
# Layout inside the bucket:
#   YYYY/MM/UUID.jsonl
#
# Cost at petboard-scale volumes (~2GB/year) is under $0.50/year.

resource "google_storage_bucket" "session_transcripts" {
  name          = "attlas-session-transcripts"
  project       = var.project_id
  location      = var.region
  force_destroy = false

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = false
  }

  lifecycle_rule {
    condition {
      age = 365
    }
    action {
      type          = "SetStorageClass"
      storage_class = "NEARLINE"
    }
  }
}

# The VM's default compute service account can already read/write GCS
# via the default scopes, but an explicit IAM binding is cleaner and
# survives scope changes.
resource "google_storage_bucket_iam_member" "vm_transcript_writer" {
  bucket = google_storage_bucket.session_transcripts.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_compute_instance.vm.service_account[0].email}"
}
