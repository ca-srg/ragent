locals {
  is_ec2               = var.compute_type == "ec2"
  is_fargate           = var.compute_type == "fargate"
  is_s3_vectors        = var.vector_backend == "s3"
  is_sqlite            = var.vector_backend == "sqlite"
  is_aws_opensearch    = var.opensearch_mode == "aws"
  is_docker_opensearch = var.opensearch_mode == "docker"
}

locals {
  opensearch_endpoint = local.is_aws_opensearch ? "https://${aws_opensearch_domain.ragent[0].endpoint}" : "http://localhost:9200"

  # ALB/TG name_prefix has a 6-character limit
  alb_name_prefix = substr(var.name_prefix, 0, 6)

  common_tags = merge(var.tags, {
    ManagedBy = "terraform"
    Module    = var.name_prefix
  })

  ragent_version_trimmed = trimprefix(var.ragent_version, "v")
  ragent_binary_url      = "https://github.com/ca-srg/ragent/releases/download/${var.ragent_version}/ragent_${local.ragent_version_trimmed}_linux_arm64.tar.gz"
}

locals {
  sm_bootstrap_env = {
    SECRET_MANAGER_SECRET_ID = var.secrets_manager_secret_name
    SECRET_MANAGER_REGION    = coalesce(var.secrets_manager_region, data.aws_region.current.region)
  }

  base_env = {
    OPENSEARCH_ENDPOINT = local.opensearch_endpoint
    OPENSEARCH_INDEX    = var.opensearch_index_name
    BEDROCK_REGION      = var.bedrock_region
    EMBEDDING_PROVIDER  = var.embedding_provider
    VECTOR_DB_BACKEND   = var.vector_backend
  }

  s3_vectors_env = local.is_s3_vectors ? {
    AWS_S3_VECTOR_BUCKET = var.s3vectors_bucket_name
    AWS_S3_VECTOR_INDEX  = var.s3vectors_index_name
    S3_VECTOR_REGION     = var.s3vectors_region
  } : {}

  sqlite_env = local.is_sqlite ? {
    SQLITE_VEC_DB_PATH = "~/.ragent/vectors.db"
  } : {}

  docker_opensearch_env = local.is_docker_opensearch ? {
    OPENSEARCH_INSECURE_SKIP_TLS = "true"
  } : {}

  sm_seed_env = merge(
    local.base_env,
    local.s3_vectors_env,
    local.sqlite_env,
    local.docker_opensearch_env,
    var.ragent_env,
  )
}
