data "aws_iam_policy_document" "opensearch_access" {
  count = local.is_aws_opensearch ? 1 : 0

  statement {
    effect = "Allow"

    principals {
      type        = "AWS"
      identifiers = [local.is_ec2 ? aws_iam_role.ragent_ec2[0].arn : aws_iam_role.ragent_task[0].arn]
    }

    actions = ["es:ESHttp*"]
    resources = [
      "arn:aws:es:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:domain/${var.opensearch_domain_name}/*",
    ]
  }
}

resource "aws_opensearch_domain" "ragent" {
  count          = local.is_aws_opensearch ? 1 : 0
  domain_name    = var.opensearch_domain_name
  engine_version = var.opensearch_engine_version

  cluster_config {
    instance_type  = var.opensearch_instance_type
    instance_count = 1
  }

  ebs_options {
    ebs_enabled = true
    volume_type = "gp3"
    volume_size = var.opensearch_volume_size
  }

  encrypt_at_rest {
    enabled = true
  }

  node_to_node_encryption {
    enabled = true
  }

  domain_endpoint_options {
    enforce_https       = true
    tls_security_policy = "Policy-Min-TLS-1-2-PFS-2023-10"
  }

  advanced_security_options {
    enabled                        = true
    internal_user_database_enabled = false

    master_user_options {
      master_user_arn = local.is_ec2 ? aws_iam_role.ragent_ec2[0].arn : aws_iam_role.ragent_task[0].arn
    }
  }

  vpc_options {
    subnet_ids         = length(var.opensearch_subnet_ids) > 0 ? var.opensearch_subnet_ids : [var.subnet_ids[0]]
    security_group_ids = [aws_security_group.opensearch[0].id]
  }

  access_policies = data.aws_iam_policy_document.opensearch_access[0].json

  tags = merge(local.common_tags, {
    Name = var.opensearch_domain_name
  })
}
