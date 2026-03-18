mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }
}

override_data {
  target = data.aws_ami.al2023_arm64
  values = {
    id = "ami-mock12345"
  }
}

run "ec2_s3vectors_aws_opensearch" {
  command = plan

  variables {
    compute_type          = "ec2"
    vector_backend        = "s3"
    opensearch_mode       = "aws"
    vpc_id                = "vpc-12345678"
    subnet_ids            = ["subnet-12345678"]
    ragent_version        = "v1.0.0"
    s3vectors_bucket_name = "test-vectors-bucket"
    s3vectors_index_name  = "test-vectors-index"
  }

  assert {
    condition     = length(aws_instance.ragent) == 1
    error_message = "EC2 instance should be created in ec2 mode"
  }

  assert {
    condition     = length(aws_ecs_cluster.ragent) == 0
    error_message = "ECS cluster should not be created in ec2 mode"
  }

  assert {
    condition     = length(aws_s3vectors_vector_bucket.ragent) == 1
    error_message = "S3 Vectors bucket should be created with s3 backend"
  }

  assert {
    condition     = length(aws_opensearch_domain.ragent) == 1
    error_message = "OpenSearch domain should be created in aws mode"
  }

  assert {
    condition     = aws_secretsmanager_secret.ragent.name == "ragent/app"
    error_message = "Secrets Manager secret should always be created"
  }
}
