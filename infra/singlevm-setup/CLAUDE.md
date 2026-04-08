# singlevm-setup

Terraform config for a single GCP VM with Caddy gateway.

## Resources managed

- `google_compute_address.vm_static_ip` — static external IP
- `google_compute_instance.vm` — the VM (openclaw-vm, e2-standard-4, Ubuntu 24.04)
- `google_compute_firewall.allow_ssh` — SSH from anywhere (port 22)
- `google_compute_firewall.allow_http_https` — HTTP/HTTPS from anywhere (ports 80, 443)

## Importing existing resources

On the current VM, these resources already exist and need to be imported before first `terraform apply`:

```bash
terraform init
terraform import google_compute_instance.vm petprojects-488115/europe-west1-b/openclaw-vm
terraform import google_compute_firewall.allow_ssh petprojects-488115/allow-ssh
```

The static IP and HTTPS firewall rule are new and will be created by `terraform apply`.

Note: importing the VM will change its external IP from ephemeral to the new static IP.

## Gateway

The `gateway/` subfolder contains the Caddy config and setup script. After `terraform apply`:

```bash
sudo gateway/setup.sh
```

This installs Caddy, deploys the Caddyfile, and starts the service. Hello world is then
accessible at `https://{static-ip}.sslip.io` with basic auth (Testuser/password123).

## State

Terraform state is local (in this directory). It is gitignored — if lost, re-import the resources above.
