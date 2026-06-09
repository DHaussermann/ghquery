locals {
  address = var.assign_eip ? aws_eip.ghquery[0].public_ip : aws_instance.ghquery.public_ip
}

output "instance_id" {
  description = "EC2 instance ID."
  value       = aws_instance.ghquery.id
}

output "public_ip" {
  description = "Public IP for SSH/management (the app itself is reached over Tailscale)."
  value       = local.address
}

output "ssh" {
  description = "SSH command for bootstrap."
  value       = "ssh ubuntu@${local.address}"
}

output "next_steps" {
  description = "Post-apply checklist."
  value       = <<-EOT
    1. ssh ubuntu@${local.address}
    2. sudo tailscale up --ssh           (if you didn't pass tailscale_auth_key)
    3. Upload the binary + prompt:
         scp ghquery-linux-amd64 ubuntu@${local.address}:/tmp/ghquery
         sudo install -m 0755 /tmp/ghquery /usr/local/bin/ghquery
         scp -r .claude ubuntu@${local.address}:/tmp/.claude
         sudo cp -r /tmp/.claude/agents /opt/ghquery/.claude/ && sudo chown -R ghquery:ghquery /opt/ghquery
    4. sudo nano /etc/ghquery/ghquery.env     (fill GITHUB_TOKEN + ANTHROPIC_API_KEY)
    5. sudo nano /etc/ghquery/catalog.yaml    (your repos + teams)
    6. sudo systemctl restart ghquery
    7. Browse to http://<tailscale-ip>:${var.app_port} from a device on the tailnet.
  EOT
}
