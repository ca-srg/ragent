resource "terraform_data" "validate_mode_combinations" {
  lifecycle {
    precondition {
      condition     = !(var.compute_type == "fargate" && var.vector_backend == "sqlite")
      error_message = "Fargate + SQLite is not supported (no EFS). Use EC2 or switch to S3 Vectors."
    }

    precondition {
      condition     = !(var.compute_type == "fargate" && var.opensearch_mode == "docker")
      error_message = "Fargate + Docker OpenSearch is not supported. Use AWS OpenSearch."
    }

    precondition {
      condition     = !(var.compute_type == "fargate" && (var.container_image_uri == null || var.container_image_uri == ""))
      error_message = "container_image_uri is required when compute_type is fargate."
    }
  }
}
