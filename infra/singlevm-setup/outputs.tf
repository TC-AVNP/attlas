output "vm_external_ip" {
  description = "Static external IP of the VM"
  value       = google_compute_address.vm_static_ip.address
}

output "sslip_domain" {
  description = "sslip.io domain for HTTPS access"
  value       = "${replace(google_compute_address.vm_static_ip.address, ".", "-")}.sslip.io"
}
