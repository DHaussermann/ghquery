variable "aws_region" {
  description = "AWS region to deploy into."
  type        = string
  default     = "us-east-1"
}

variable "instance_type" {
  description = "EC2 instance type. t3.small is ample for this workload."
  type        = string
  default     = "t3.small"
}

variable "instance_name" {
  description = "Name tag for the instance and related resources."
  type        = string
  default     = "ghquery"
}

variable "key_name" {
  description = "Name of an existing EC2 key pair for SSH access (bootstrap)."
  type        = string
}

variable "admin_ssh_cidr" {
  description = "CIDR allowed to SSH in for bootstrap, e.g. your office/VPN IP as 203.0.113.4/32. This is the ONLY public ingress; the app itself is reached over Tailscale."
  type        = string
}

variable "subnet_id" {
  description = "Subnet to launch into. Leave empty to use a subnet in the default VPC."
  type        = string
  default     = ""
}

variable "assign_eip" {
  description = "Attach an Elastic IP so the SSH/management address is stable across restarts."
  type        = bool
  default     = true
}

variable "app_port" {
  description = "Port ghquery's web UI listens on (reached over Tailscale, never exposed publicly)."
  type        = number
  default     = 8080
}

variable "tailscale_auth_key" {
  description = "Optional Tailscale auth key. If set, the instance joins the tailnet automatically at boot. If empty, run `sudo tailscale up --ssh` via SSH after boot. NOTE: setting this puts the key in Terraform state — prefer an ephemeral/reusable key, or leave empty and run tailscale up manually."
  type        = string
  default     = ""
  sensitive   = true
}

variable "ghquery_binary_url" {
  description = "Optional URL to fetch the linux-amd64 ghquery binary at boot. If empty, upload the binary manually (see deploy/README.md). No public release contains hosted mode yet, so this is normally left empty for the first deploy."
  type        = string
  default     = ""
}

variable "anthropic_model" {
  description = "Default Anthropic model the API backend uses. Overridable later by editing /etc/ghquery/ghquery.env."
  type        = string
  default     = "claude-opus-4-8"
}
