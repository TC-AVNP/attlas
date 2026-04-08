# Static external IP
resource "google_compute_address" "vm_static_ip" {
  name         = "${var.vm_name}-static-ip"
  region       = var.region
  network_tier = "PREMIUM"
}

# Compute instance
resource "google_compute_instance" "vm" {
  name         = var.vm_name
  machine_type = var.machine_type
  zone         = var.zone

  tags = ["http-server", "https-server"]

  boot_disk {
    auto_delete = true
    initialize_params {
      image = var.disk_image
      size  = var.disk_size_gb
      type  = "pd-standard"
    }
  }

  network_interface {
    network    = "default"
    subnetwork = "default"

    access_config {
      nat_ip       = google_compute_address.vm_static_ip.address
      network_tier = "PREMIUM"
    }
  }

  scheduling {
    automatic_restart   = true
    on_host_maintenance = "MIGRATE"
    preemptible         = false
  }

  service_account {
    email  = "710670943493-compute@developer.gserviceaccount.com"
    scopes = [
      "https://www.googleapis.com/auth/devstorage.read_only",
      "https://www.googleapis.com/auth/logging.write",
      "https://www.googleapis.com/auth/monitoring.write",
      "https://www.googleapis.com/auth/pubsub",
      "https://www.googleapis.com/auth/service.management.readonly",
      "https://www.googleapis.com/auth/servicecontrol",
      "https://www.googleapis.com/auth/trace.append",
    ]
  }

  # Prevent terraform from recreating the VM due to image drift or metadata changes
  lifecycle {
    ignore_changes = [
      boot_disk[0].initialize_params[0].image,
      metadata,
    ]
  }
}
