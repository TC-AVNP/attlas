output "vm_external_ip" {
  description = "Static external IP of the VM"
  value       = google_compute_address.vm_static_ip.address
}

output "domain" {
  description = "Domain for HTTPS access"
  value       = "attlas.uk"
}

output "ssh_command" {
  description = "SSH command to connect to the VM (OS Login derives the username from your IAM identity). Use `sudo -iu agnostic-user` to land in the service-owning account."
  value       = "gcloud compute ssh ${var.vm_name} --zone=${var.zone}"
}

output "startup_log_command" {
  description = "Command to check startup script log"
  value       = "gcloud compute ssh ${var.vm_name} --zone=${var.zone} --command='sudo tail -50 /var/log/startup-script.log'"
}
