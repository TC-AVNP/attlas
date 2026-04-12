# Splitsies Infra Handover

Self-contained brief for provisioning the GCP Secret Manager secret and
IAM binding that Splitsies needs before `install-splitsies.sh` can run.
Idempotent — works whether or not the secret already exists.

## Context

- **GCP project**: `petprojects-488115`
- **Target secret name**: `splitsies-config`
- **VM service account that must read it**: `710670943493-compute@developer.gserviceaccount.com`
- **Terraform file that owns IAM bindings**: `attlas/infra/singlevm-setup/secrets.tf`
  (already includes `"splitsies-config"` in `local.secrets`)

## Step 1 — Ensure the secret exists with the correct payload

Run from anywhere with `gcloud` authed as a user who can manage secrets
in `petprojects-488115`:

```bash
gcloud config set project petprojects-488115

# Create the secret if missing. Silence ALREADY_EXISTS so this is idempotent.
gcloud secrets create splitsies-config --replication-policy=automatic 2>/dev/null || true

# Write (or overwrite) the payload. The credentials are NOT checked into
# the repo; fill them in when handing this brief off. The client_id /
# client_secret come from the Google OAuth client named "Splitsies" in
# Google Cloud Console → APIs & Services → Credentials.
cat <<'EOF' | gcloud secrets versions add splitsies-config --data-file=-
{
  "client_id":     "<GOOGLE_OAUTH_CLIENT_ID>",
  "client_secret": "<GOOGLE_OAUTH_CLIENT_SECRET>",
  "initial_admin": "<EMAIL_TO_SEED_AS_FIRST_ADMIN>"
}
EOF
```

## Step 2 — Grant the VM service account read access

Pick one option:

### Option A — via Terraform (preferred, keeps IaC in sync)

The splitsies-config entry is already declared in
`attlas/infra/singlevm-setup/secrets.tf`. Just apply:

```bash
cd attlas/infra/singlevm-setup
terraform init     # only if this is a fresh checkout
terraform plan     # expect: +1 resource google_secret_manager_secret_iam_member.vm_access["splitsies-config"]
terraform apply
```

### Option B — direct gcloud (faster, creates Terraform drift)

A subsequent `terraform plan` will show this binding as a no-op match
(it exists and matches what Terraform wants) rather than proposing a
change. Safe but slightly less clean than going through Terraform.

```bash
gcloud secrets add-iam-policy-binding splitsies-config \
  --member='serviceAccount:710670943493-compute@developer.gserviceaccount.com' \
  --role='roles/secretmanager.secretAccessor' \
  --project=petprojects-488115
```

## Step 3 — Verify from the VM

SSH to the attlas VM, then:

```bash
gcloud secrets versions access latest --secret=splitsies-config | python3 -m json.tool
```

Expected: JSON with `client_id`, `client_secret`, `initial_admin` keys.
No `PERMISSION_DENIED`.

## Rollback

Only if something goes wrong — otherwise leave everything in place.

```bash
gcloud secrets delete splitsies-config --project=petprojects-488115
# Then remove "splitsies-config" from attlas/infra/singlevm-setup/secrets.tf and re-apply.
```
