locals {
  ragent_env_file_content = templatefile("${path.module}/templates/ragent.env.tpl", {
    environment_variables = local.sm_bootstrap_env
  })

  ragent_init_script_content = templatefile("${path.module}/templates/ragent-init.sh.tpl", {
    is_docker_opensearch = local.is_docker_opensearch
    opensearch_endpoint  = local.opensearch_endpoint
    opensearch_index_name = var.opensearch_index_name
    embedding_dimension  = var.opensearch_embedding_dimension
  })

  ragent_init_service_content = templatefile("${path.module}/templates/ragent-init.service.tpl", {
    is_docker_opensearch = local.is_docker_opensearch
  })

  ragent_mcp_service_content = templatefile("${path.module}/templates/ragent-mcp.service.tpl", {
    mcp_auth_method      = var.mcp_auth_method
    mcp_bypass_ip_ranges = var.mcp_bypass_ip_ranges
  })

  ragent_slack_service_content = var.slack_bot_enabled ? templatefile(
    "${path.module}/templates/ragent-slack.service.tpl",
    {},
  ) : ""

  ragent_vectorize_service_content = var.vectorize_enabled ? templatefile(
    "${path.module}/templates/ragent-vectorize.service.tpl",
    {
      vectorize_s3_source_bucket = var.vectorize_s3_source_bucket
      vectorize_github_repos     = var.vectorize_github_repos
    },
  ) : ""
}

data "aws_ami" "al2023_arm64" {
  count       = local.is_ec2 && var.ami_id == null ? 1 : 0
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-arm64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  filter {
    name   = "root-device-type"
    values = ["ebs"]
  }
}

resource "aws_instance" "ragent" {
  count = local.is_ec2 ? 1 : 0

  ami           = var.ami_id != null ? var.ami_id : data.aws_ami.al2023_arm64[0].id
  instance_type = var.instance_type

  subnet_id                   = var.subnet_ids[0]
  vpc_security_group_ids      = [aws_security_group.compute.id]
  iam_instance_profile        = aws_iam_instance_profile.ragent[0].name
  key_name                    = var.key_name
  ebs_optimized               = true
  associate_public_ip_address = false

  user_data = templatefile("${path.module}/templates/cloud-init.sh.tpl", {
    ragent_binary_url        = local.ragent_binary_url
    is_docker_opensearch     = local.is_docker_opensearch
    ragent_env_file          = local.ragent_env_file_content
    ragent_init_script       = local.ragent_init_script_content
    ragent_init_service      = local.ragent_init_service_content
    ragent_mcp_service       = local.ragent_mcp_service_content
    slack_bot_enabled        = var.slack_bot_enabled
    ragent_slack_service     = local.ragent_slack_service_content
    vectorize_enabled        = var.vectorize_enabled
    ragent_vectorize_service = local.ragent_vectorize_service_content
  })

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 2
  }

  root_block_device {
    volume_type = "gp3"
    volume_size = var.root_volume_size
    encrypted   = true
  }

  tags = merge(local.common_tags, { Name = "ragent" })

  depends_on = [
    aws_iam_role_policy.ragent_bedrock,
    aws_iam_role_policy.ragent_secrets_manager,
    aws_iam_role_policy.ragent_opensearch,
    aws_iam_role_policy.ragent_s3vectors,
    aws_iam_role_policy.ragent_s3_source,
  ]
}
