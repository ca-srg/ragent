mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }
}

run "invalid_compute_type" {
  command = plan

  variables {
    compute_type          = "invalid"
    vpc_id                = "vpc-12345678"
    subnet_ids            = ["subnet-12345678"]
    ragent_version        = "v1.0.0"
    certificate_arn       = "arn:aws:acm:ap-northeast-1:123456789012:certificate/test"
    s3vectors_bucket_name = "test-vectors-bucket"
    s3vectors_index_name  = "test-vectors-index"
  }

  expect_failures = [
    var.compute_type,
  ]
}

run "invalid_vector_backend" {
  command = plan

  variables {
    vector_backend        = "invalid"
    vpc_id                = "vpc-12345678"
    subnet_ids            = ["subnet-12345678"]
    ragent_version        = "v1.0.0"
    certificate_arn       = "arn:aws:acm:ap-northeast-1:123456789012:certificate/test"
    s3vectors_bucket_name = "test-vectors-bucket"
    s3vectors_index_name  = "test-vectors-index"
  }

  expect_failures = [
    var.vector_backend,
  ]
}

run "invalid_opensearch_mode" {
  command = plan

  variables {
    opensearch_mode       = "invalid"
    vpc_id                = "vpc-12345678"
    subnet_ids            = ["subnet-12345678"]
    ragent_version        = "v1.0.0"
    certificate_arn       = "arn:aws:acm:ap-northeast-1:123456789012:certificate/test"
    s3vectors_bucket_name = "test-vectors-bucket"
    s3vectors_index_name  = "test-vectors-index"
  }

  expect_failures = [
    var.opensearch_mode,
  ]
}

run "fargate_sqlite_rejected" {
  command = plan

  variables {
    compute_type        = "fargate"
    vector_backend      = "sqlite"
    container_image_uri = "ghcr.io/ca-srg/ragent:latest"
    vpc_id              = "vpc-12345678"
    subnet_ids          = ["subnet-12345678"]
    ragent_version      = "v1.0.0"
    certificate_arn     = "arn:aws:acm:ap-northeast-1:123456789012:certificate/test"
  }

  expect_failures = [
    terraform_data.validate_mode_combinations,
  ]
}

run "fargate_docker_opensearch_rejected" {
  command = plan

  variables {
    compute_type          = "fargate"
    opensearch_mode       = "docker"
    container_image_uri   = "ghcr.io/ca-srg/ragent:latest"
    vpc_id                = "vpc-12345678"
    subnet_ids            = ["subnet-12345678"]
    ragent_version        = "v1.0.0"
    certificate_arn       = "arn:aws:acm:ap-northeast-1:123456789012:certificate/test"
    s3vectors_bucket_name = "test-vectors-bucket"
    s3vectors_index_name  = "test-vectors-index"
  }

  expect_failures = [
    terraform_data.validate_mode_combinations,
  ]
}

run "fargate_no_container_image_rejected" {
  command = plan

  variables {
    compute_type          = "fargate"
    vpc_id                = "vpc-12345678"
    subnet_ids            = ["subnet-12345678"]
    ragent_version        = "v1.0.0"
    certificate_arn       = "arn:aws:acm:ap-northeast-1:123456789012:certificate/test"
    s3vectors_bucket_name = "test-vectors-bucket"
    s3vectors_index_name  = "test-vectors-index"
  }

  expect_failures = [
    terraform_data.validate_mode_combinations,
  ]
}
