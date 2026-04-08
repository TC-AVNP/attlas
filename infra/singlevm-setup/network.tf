# SSH via IAP only (gcloud compute ssh uses Identity-Aware Proxy)
resource "google_compute_firewall" "allow_ssh_iap" {
  name    = "allow-ssh-iap"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  # Google's IAP IP range — only gcloud compute ssh can reach port 22
  source_ranges = ["35.235.240.0/20"]
  target_tags   = ["ssh-iap"]
  direction     = "INGRESS"
  priority      = 1000
}

# HTTP + HTTPS for Caddy (port 80 needed for ACME TLS cert challenge)
resource "google_compute_firewall" "allow_https" {
  name    = "allow-https"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["80", "443"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["https-server"]
  direction     = "INGRESS"
  priority      = 1000
}
