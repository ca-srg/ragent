terraform {
  required_version = ">= 1.5"
}

provider "aws" {
  region = var.aws_region
}

module "ragent" {
  source = "../../modules/ragent"

  compute_type     = "ec2"
  ragent_version   = var.ragent_version
  instance_type    = var.instance_type
  root_volume_size = var.root_volume_size

  vpc_id     = var.vpc_id
  subnet_ids = var.subnet_ids

  vector_backend              = "sqlite"
  opensearch_mode             = "docker"
  secrets_manager_secret_name = var.secrets_manager_secret_name

  slack_bot_enabled          = var.slack_bot_enabled
  vectorize_enabled          = var.vectorize_enabled
  vectorize_s3_source_bucket = var.vectorize_s3_source_bucket
  vectorize_github_repos     = var.vectorize_github_repos

  mcp_auth_method      = var.mcp_auth_method
  mcp_bypass_ip_ranges = var.mcp_bypass_ip_ranges

  ragent_env = var.ragent_env

  # Override systemd service units via drop-in files
  # systemd_service_overrides = {
  #   "ragent-mcp" = <<-EOT
  #     [Service]
  #     ExecStart=
  #     ExecStart=/usr/local/bin/ragent mcp-server --host 0.0.0.0 --auth-method ip
  #   EOT
  # }

  tags = merge(var.tags, {
    Project     = "ragent"
    Environment = "production"
  })
}
