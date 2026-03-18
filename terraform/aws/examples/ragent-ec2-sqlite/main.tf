terraform {
  required_version = ">= 1.5"
}

provider "aws" {
  region = var.aws_region
}

module "ragent" {
  source = "../../modules/ragent"

  compute_type   = "ec2"
  ragent_version = var.ragent_version

  vpc_id     = var.vpc_id
  subnet_ids = var.subnet_ids

  vector_backend  = "sqlite"
  opensearch_mode = "docker"

  tags = var.tags
}
