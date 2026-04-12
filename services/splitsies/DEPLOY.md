# Deploying Splitsies to splitsies.attlas.uk

Splitsies runs on the attlas VM behind its own subdomain with its own
Google OAuth — fully independent from attlas.uk's auth. This document
lists every step from "nothing" to "`https://splitsies.attlas.uk` loads
and accepts my login".

## One-time manual setup

These steps require human judgment / web consoles and are NOT automated
by any install script.

### 1. Create a Google OAuth 2.0 client

Splitsies has its own OAuth client because it has its own user whitelist,
separate from attlas.uk's.

1. Go to Google Cloud Console → APIs & Services → Credentials →
   `+ Create Credentials` → `OAuth client ID`.
2. Application type: **Web application**.
3. Name: `Splitsies`.
4. **Authorised redirect URIs** — add BOTH:
   - `https://splitsies.attlas.uk/api/auth/callback` (production)
   - `http://localhost:7691/api/auth/callback` (local dev, optional)
5. Save. Copy the **client ID** and **client secret**.

### 2. Store credentials in GCP Secret Manager

Splitsies reads everything from one JSON secret called `splitsies-config`:

```bash
gcloud secrets create splitsies-config --replication-policy=automatic

echo '{
  "client_id": "<PASTE_CLIENT_ID>",
  "client_secret": "<PASTE_CLIENT_SECRET>",
  "initial_admin": "you@gmail.com"
}' | gcloud secrets versions add splitsies-config --data-file=-
```

Fields:
- `client_id`, `client_secret` — from step 1.
- `initial_admin` — your Google email. On first run, splitsies seeds
  this email as the first admin so you can log in. The field is ignored
  on subsequent starts (once any admin exists).

Grant the VM's service account read access (only needed once — if other
attlas secrets already work, the binding exists):

```bash
gcloud secrets add-iam-policy-binding splitsies-config \
  --member="serviceAccount:<vm-service-account>@<project>.iam.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"
```

### 3. Cloudflare DNS

`install-splitsies-gateway.sh` creates/updates the `splitsies.attlas.uk`
A record automatically using the existing `cloudflare-dns-token` secret,
assuming that token has `Zone:DNS:Edit` permission (which it should — the
existing base-setup script already uses it the same way). **No manual
Cloudflare work needed.**

If you prefer to create the record by hand, it's:
- Type: `A`, Name: `splitsies`, IPv4: `<VM external IP>`, Proxy: off.

## Deploying

On the VM, from the repo root:

```bash
sudo bash attlas/services/install.sh
```

Select `splitsies-gateway` and `splitsies` from the menu (or `a` for
all). The alphabetical sort puts `install-splitsies-gateway.sh` before
`install-splitsies.sh`, so the gateway starts first and splitsies
registers its route with the already-running gateway.

What each script does:

- `install-splitsies-gateway.sh`
  - Builds and installs the gateway binary at `/usr/local/bin/splitsies-gateway`.
  - Creates the routes directory at `/etc/splitsies-gateway.d/`.
  - Drops the Caddy site block at `/etc/caddy/sites.d/splitsies-gateway.caddy`.
  - Upserts the `splitsies.attlas.uk` A record in Cloudflare.
  - Writes and starts `splitsies-gateway.service`.

- `install-splitsies.sh`
  - Builds the React frontend and Go backend.
  - Reads `splitsies-config` secret and injects it as env vars into the
    systemd unit.
  - Writes and starts `splitsies.service`.
  - Drops `/etc/splitsies-gateway.d/splitsies.conf` so the gateway
    routes `/` → `127.0.0.1:7691`.
  - Reloads the gateway (SIGHUP) so the new route takes effect.

`install.sh` reloads Caddy at the end so the new `splitsies.attlas.uk`
site block is picked up.

## Verification

```bash
# On the VM
systemctl status splitsies-gateway splitsies

# From anywhere
curl -sI https://splitsies.attlas.uk | head -1        # should be 200
curl -s  https://splitsies.attlas.uk/api/auth/me      # should be 401 (not logged in)
```

Open `https://splitsies.attlas.uk` in a browser, click "Sign in with
Google", and log in with the `initial_admin` email. You should land on
the dashboard with admin permissions.

## Adding more users

Once you're logged in as admin:
- Navigate to `/admin`.
- Add emails to the whitelist.
- Those users can then sign in with Google.

## Adding more services under splitsies.attlas.uk

Any future service that wants to live at `splitsies.attlas.uk/<prefix>/`
drops a file in `/etc/splitsies-gateway.d/` with the format:

```
# /etc/splitsies-gateway.d/my-service.conf
/my-service http://127.0.0.1:<port>
```

Then `systemctl reload splitsies-gateway` to pick up the new route. No
Caddy changes needed — the gateway handles all routing within the
subdomain.

## Rolling back

Uninstall scripts aren't provided yet. To fully remove:

```bash
sudo systemctl disable --now splitsies splitsies-gateway
sudo rm -f /etc/systemd/system/splitsies.service /etc/systemd/system/splitsies-gateway.service
sudo rm -f /usr/local/bin/splitsies /usr/local/bin/splitsies-gateway
sudo rm -rf /usr/local/share/splitsies /var/lib/splitsies /etc/splitsies-gateway.d
sudo rm -f /etc/caddy/sites.d/splitsies-gateway.caddy
sudo systemctl daemon-reload
sudo systemctl reload caddy
# (Cloudflare DNS record can stay — it's harmless if the VM isn't listening.)
```
