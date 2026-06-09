terraform {
  required_version = ">= 1.3"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# Latest Ubuntu 24.04 LTS (amd64), published by Canonical.
data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"]

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd*/ubuntu-noble-24.04-amd64-server-*"]
  }
  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# Default VPC + a subnet within it, used when var.subnet_id is empty.
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

locals {
  subnet_id = var.subnet_id != "" ? var.subnet_id : data.aws_subnets.default.ids[0]
}

# Security group: the ONLY public ingress is SSH from the admin CIDR (for
# bootstrap). The app port is never opened — the UI is reached over Tailscale.
# Egress is open so the box can reach Tailscale, GitHub, and the Anthropic API.
resource "aws_security_group" "ghquery" {
  name        = "${var.instance_name}-sg"
  description = "ghquery: SSH from admin only; no public app ingress"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "SSH (bootstrap) from admin CIDR"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.admin_ssh_cidr]
  }

  egress {
    description = "All outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.instance_name}-sg"
  }
}

resource "aws_instance" "ghquery" {
  ami                         = data.aws_ami.ubuntu.id
  instance_type               = var.instance_type
  subnet_id                   = local.subnet_id
  vpc_security_group_ids      = [aws_security_group.ghquery.id]
  key_name                    = var.key_name
  associate_public_ip_address = true

  user_data = templatefile("${path.module}/user_data.sh.tftpl", {
    app_port          = var.app_port
    tailscale_authkey = var.tailscale_auth_key
    binary_url        = var.ghquery_binary_url
    anthropic_model   = var.anthropic_model
  })

  root_block_device {
    volume_size = 20
    volume_type = "gp3"
  }

  tags = {
    Name = var.instance_name
  }
}

resource "aws_eip" "ghquery" {
  count    = var.assign_eip ? 1 : 0
  domain   = "vpc"
  instance = aws_instance.ghquery.id

  tags = {
    Name = var.instance_name
  }
}
