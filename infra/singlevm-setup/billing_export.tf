# Cloud Billing BigQuery export setup.
#
# Terraform can not actually FLIP THE SWITCH that sends billing data
# to BigQuery — that's a one-time manual action in the Cloud Console
# (Billing → Billing export → BigQuery export). What Terraform CAN
# do is stand up everything the export needs on the landing side:
# the APIs it depends on, the dataset the export writes into, and
# the IAM bindings that let the VM's service account read from it.
#
# After `terraform apply`, open the Cloud Console once and point the
# billing account at the `billing_export` dataset in this project.
# From then on the export is populated daily, and the dashboard's
# /api/cloud-spend endpoint queries it directly.

# 1. Enable the APIs the rest of this file depends on.
#
# `cloudbilling` is the admin surface used by gcloud/console pages.
# `cloudresourcemanager` is needed for project-level IAM bindings.
# `bigquery` is the dataset + query API we actually hit at runtime.
# `logging` powers the daily-uptime replay over audit events.
# `billingbudgets` is optional but cheap — enables future use of
# the Budgets API if we ever want spend alerts on the dashboard.
resource "google_project_service" "enabled" {
  for_each = toset([
    "cloudbilling.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "bigquery.googleapis.com",
    "logging.googleapis.com",
    "billingbudgets.googleapis.com",
  ])
  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

# 2. The dataset that the Cloud Billing export writes into.
#
# Location is pinned to EU so query egress stays free within the same
# region as the VM. Default table expiration is left unset (null) so
# the export tables live forever — billing history is useful even
# for old months.
resource "google_bigquery_dataset" "billing_export" {
  dataset_id                  = "billing_export"
  friendly_name               = "Cloud Billing BigQuery export"
  description                 = "Destination for Cloud Billing daily cost export. Manual console step wires the billing account to this dataset."
  location                    = "EU"
  default_table_expiration_ms = null

  depends_on = [google_project_service.enabled]
}

# 3. Grant the VM's service account read access on the dataset plus
#    the project-level BigQuery Job User role it needs to actually
#    run SELECT queries.
resource "google_bigquery_dataset_iam_member" "vm_data_viewer" {
  dataset_id = google_bigquery_dataset.billing_export.dataset_id
  role       = "roles/bigquery.dataViewer"
  member     = local.vm_service_account
}

resource "google_project_iam_member" "vm_bq_job_user" {
  project = var.project_id
  role    = "roles/bigquery.jobUser"
  member  = local.vm_service_account
}
