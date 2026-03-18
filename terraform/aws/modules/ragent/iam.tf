data "aws_caller_identity" "current" {}

data "aws_region" "current" {}

data "aws_partition" "current" {}

locals {
  iam_runtime_role_name = local.is_ec2 ? aws_iam_role.ragent_ec2[0].name : aws_iam_role.ragent_task[0].name

  iam_secrets_manager_region = coalesce(var.secrets_manager_region, data.aws_region.current.region)

  iam_opensearch_domain_arn = try(format("%s/*", aws_opensearch_domain.ragent[0].arn), null)

  iam_s3vectors_bucket_arn = try(aws_s3vectors_vector_bucket.ragent[0].vector_bucket_arn, null)

  iam_s3vectors_index_arn = try(aws_s3vectors_index.ragent[0].index_arn, null)
}

data "aws_iam_policy_document" "ragent_ec2_assume_role" {
  count = local.is_ec2 ? 1 : 0

  statement {
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }

    actions = ["sts:AssumeRole"]
  }
}

data "aws_iam_policy_document" "ragent_task_assume_role" {
  count = local.is_fargate ? 1 : 0

  statement {
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }

    actions = ["sts:AssumeRole"]
  }
}

data "aws_iam_policy_document" "ragent_execution_assume_role" {
  count = local.is_fargate ? 1 : 0

  statement {
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }

    actions = ["sts:AssumeRole"]
  }
}

data "aws_iam_policy_document" "ragent_bedrock" {
  statement {
    effect = "Allow"
    actions = [
      "bedrock:InvokeModel",
      "bedrock:InvokeModelWithResponseStream",
      "bedrock:Converse",
      "bedrock:ConverseStream",
    ]
    resources = ["*"]
  }
}

data "aws_iam_policy_document" "ragent_opensearch" {
  count = local.is_aws_opensearch ? 1 : 0

  statement {
    effect    = "Allow"
    actions   = ["es:ESHttp*"]
    resources = [local.iam_opensearch_domain_arn]
  }
}

data "aws_iam_policy_document" "ragent_s3vectors" {
  count = local.is_s3_vectors ? 1 : 0

  statement {
    effect    = "Allow"
    actions   = ["s3vectors:*"]
    resources = [local.iam_s3vectors_bucket_arn, local.iam_s3vectors_index_arn]
  }
}

data "aws_iam_policy_document" "ragent_secrets_manager" {
  statement {
    effect = "Allow"
    actions = [
      "secretsmanager:GetSecretValue",
      "secretsmanager:DescribeSecret",
    ]
    resources = [format(
      "arn:%s:secretsmanager:%s:%s:secret:%s*",
      data.aws_partition.current.partition,
      local.iam_secrets_manager_region,
      data.aws_caller_identity.current.account_id,
      var.secrets_manager_secret_name,
    )]
  }
}

data "aws_iam_policy_document" "ragent_s3_source" {
  count = var.vectorize_s3_source_bucket != null ? 1 : 0

  statement {
    effect  = "Allow"
    actions = ["s3:ListBucket"]
    resources = [format(
      "arn:%s:s3:::%s",
      data.aws_partition.current.partition,
      var.vectorize_s3_source_bucket,
    )]
  }

  statement {
    effect  = "Allow"
    actions = ["s3:GetObject"]
    resources = [format(
      "arn:%s:s3:::%s/*",
      data.aws_partition.current.partition,
      var.vectorize_s3_source_bucket,
    )]
  }
}

resource "aws_iam_role" "ragent_ec2" {
  count = local.is_ec2 ? 1 : 0

  name               = "ragent-ec2"
  assume_role_policy = data.aws_iam_policy_document.ragent_ec2_assume_role[0].json

  tags = merge(local.common_tags, {
    Name = "ragent-ec2"
  })
}

resource "aws_iam_instance_profile" "ragent" {
  count = local.is_ec2 ? 1 : 0

  name = "ragent"
  role = aws_iam_role.ragent_ec2[0].name

  tags = merge(local.common_tags, {
    Name = "ragent"
  })
}

resource "aws_iam_role_policy_attachment" "ragent_ec2_ssm" {
  count = local.is_ec2 ? 1 : 0

  role       = aws_iam_role.ragent_ec2[0].name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role" "ragent_task" {
  count = local.is_fargate ? 1 : 0

  name               = "ragent-task"
  assume_role_policy = data.aws_iam_policy_document.ragent_task_assume_role[0].json

  tags = merge(local.common_tags, {
    Name = "ragent-task"
  })
}

resource "aws_iam_role" "ragent_execution" {
  count = local.is_fargate ? 1 : 0

  name               = "ragent-execution"
  assume_role_policy = data.aws_iam_policy_document.ragent_execution_assume_role[0].json

  tags = merge(local.common_tags, {
    Name = "ragent-execution"
  })
}

resource "aws_iam_role_policy_attachment" "ragent_execution_managed" {
  count = local.is_fargate ? 1 : 0

  role       = aws_iam_role.ragent_execution[0].name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "ragent_bedrock" {
  name   = "ragent-bedrock"
  role   = local.iam_runtime_role_name
  policy = data.aws_iam_policy_document.ragent_bedrock.json
}

resource "aws_iam_role_policy" "ragent_opensearch" {
  count = local.is_aws_opensearch ? 1 : 0

  name   = "ragent-opensearch"
  role   = local.iam_runtime_role_name
  policy = data.aws_iam_policy_document.ragent_opensearch[0].json
}

resource "aws_iam_role_policy" "ragent_s3vectors" {
  count = local.is_s3_vectors ? 1 : 0

  name   = "ragent-s3vectors"
  role   = local.iam_runtime_role_name
  policy = data.aws_iam_policy_document.ragent_s3vectors[0].json
}

resource "aws_iam_role_policy" "ragent_secrets_manager" {
  name   = "ragent-secrets-manager"
  role   = local.iam_runtime_role_name
  policy = data.aws_iam_policy_document.ragent_secrets_manager.json
}

resource "aws_iam_role_policy" "ragent_s3_source" {
  count = var.vectorize_s3_source_bucket != null ? 1 : 0

  name   = "ragent-s3-source"
  role   = local.iam_runtime_role_name
  policy = data.aws_iam_policy_document.ragent_s3_source[0].json
}
