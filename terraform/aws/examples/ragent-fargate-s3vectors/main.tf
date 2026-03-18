terraform {
  required_version = ">= 1.5"
}

provider "aws" {
  region = var.aws_region
}

module "ragent" {
  source = "../../modules/ragent"

  compute_type        = "fargate"
  container_image_uri = var.container_image_uri
  ragent_version      = var.ragent_version

  vpc_id     = var.vpc_id
  subnet_ids = var.subnet_ids

  vector_backend        = "s3"
  s3vectors_bucket_name = var.s3vectors_bucket_name
  s3vectors_index_name  = var.s3vectors_index_name

  opensearch_mode = "aws"

  tags = var.tags
}
