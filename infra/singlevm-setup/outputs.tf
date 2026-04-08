output "vm_external_ip" {
  description = "Static external IP of the VM"
  value       = google_compute_address.vm_static_ip.address
}

output "sslip_domain" {
  description = "sslip.io domain for HTTPS access"
  value       = "${replace(google_compute_address.vm_static_ip.address, ".", "-")}.sslip.io"
}

output "ssh_command" {
  description = "SSH command to connect to the VM"
  value       = "gcloud compute ssh ${var.vm_user}@${var.vm_name} --zone=${var.zone}"
}

output "startup_log_command" {
  description = "Command to check startup script log"
  value       = "gcloud compute ssh ${var.vm_user}@${var.vm_name} --zone=${var.zone} --command='sudo tail -50 /var/log/startup-script.log'"
}
