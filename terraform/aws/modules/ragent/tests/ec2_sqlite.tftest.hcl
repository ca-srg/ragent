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

run "ec2_sqlite_docker_opensearch" {
  command = plan

  variables {
    compute_type    = "ec2"
    vector_backend  = "sqlite"
    opensearch_mode = "docker"
    vpc_id          = "vpc-12345678"
    subnet_ids      = ["subnet-12345678"]
    ragent_version  = "v1.0.0"
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
    condition     = length(aws_s3vectors_vector_bucket.ragent) == 0
    error_message = "S3 Vectors should not be created with sqlite backend"
  }

  assert {
    condition     = length(aws_opensearch_domain.ragent) == 0
    error_message = "OpenSearch domain should not be created in docker mode"
  }

  assert {
    condition     = can(regex("init-opensearch\\.sh", aws_instance.ragent[0].user_data))
    error_message = "Cloud-init should include OpenSearch init script"
  }

  assert {
    condition     = can(regex("ragent-init\\.service", aws_instance.ragent[0].user_data))
    error_message = "Cloud-init should include ragent-init systemd service"
  }

  assert {
    condition     = can(regex("knn_vector", aws_instance.ragent[0].user_data))
    error_message = "Init script should contain knn_vector mapping"
  }

  assert {
    condition     = aws_secretsmanager_secret.ragent.name == "ragent/app"
    error_message = "Secrets Manager secret should always be created"
  }
}
