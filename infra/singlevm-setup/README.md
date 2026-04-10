# Infrastructure — Single VM Setup

Terraform configuration that provisions a GCP VM with a static IP and firewall rules.

## What It Creates

| Resource | Description |
|----------|-------------|
| Static IP | Premium-tier external IP for the VM |
| VM Instance | e2-standard-4, Ubuntu 24.04, europe-west1-b |
| Firewall: SSH | Allow TCP:22 from anywhere |
| Firewall: HTTP/HTTPS | Allow TCP:80,443 from anywhere |

## Prerequisites

1. Install [Terraform](https://developer.hashicorp.com/terraform/install) and [gcloud CLI](https://cloud.google.com/sdk/docs/install)
2. Authenticate: `gcloud auth login && gcloud auth application-default login`
3. Create the GitHub PAT secret in Secret Manager:
   ```bash
   gcloud services enable secretmanager.googleapis.com --project=petprojects-488115
   printf '%s' 'YOUR_GITHUB_PAT' | gcloud secrets create github-pat --data-file=- --project=petprojects-488115
   gcloud secrets add-iam-policy-binding github-pat \
     --member="serviceAccount:710670943493-compute@developer.gserviceaccount.com" \
     --role="roles/secretmanager.secretAccessor" --project=petprojects-488115
   ```

## Usage

```bash
cd infra/singlevm-setup
terraform init
terraform apply
```

After apply, SSH into the VM (`gcloud compute ssh simple-zombie --zone=europe-west1-b`) and run:

```bash
sudo bash /home/agnostic-user/iapetus/attlas/base-setup/setup.sh
```

This installs all packages, clones dotfiels, builds the dashboard, and sets up every systemd service in one shot. `startup.sh` has already created the `agnostic-user`, `alive-svc`, and `openclaw-svc` accounts and cloned attlas. OS Login is enabled on the instance, so `gcloud compute ssh` uses your IAM-derived username; to land in the service-owning account for interactive work, run `sudo -iu agnostic-user` after connecting.

## Terraform state

The state file (`terraform.tfstate`) is committed to git. To use on a new machine:

```bash
git clone <repo-url>
cd attlas/infra/singlevm-setup
gcloud auth login && gcloud auth application-default login
gcloud config set project petprojects-488115
terraform init    # downloads providers, uses committed state
terraform plan    # should show "No changes" if in sync
```

If the state is missing or corrupted, `terraform destroy && terraform apply` from scratch — setup scripts handle full provisioning.
