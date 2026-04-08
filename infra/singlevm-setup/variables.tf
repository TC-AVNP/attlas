variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
}

variable "zone" {
  description = "GCP zone"
  type        = string
}

variable "vm_name" {
  description = "Name of the compute instance"
  type        = string
}

variable "machine_type" {
  description = "GCE machine type"
  type        = string
}

variable "disk_size_gb" {
  description = "Boot disk size in GB"
  type        = number
}

variable "disk_image" {
  description = "Boot disk image"
  type        = string
}

variable "vm_user" {
  description = "Non-root user account to create on the VM"
  type        = string
  default     = "condecopedro"
}

variable "attlas_repo" {
  description = "GitHub HTTPS URL for the attlas repo (without PAT)"
  type        = string
  default     = "https://github.com/TC-AVNP/attlas.git"
}
