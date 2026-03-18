variable "aws_region" {
  type        = string
  description = "AWS region for the example deployment"
}

variable "container_image_uri" {
  type        = string
  description = "Container image URI for the Fargate task"
}

variable "ragent_version" {
  type        = string
  description = "RAGent release version, for example v1.0.0"
}

variable "vpc_id" {
  type        = string
  description = "Existing VPC ID for RAGent resources"
}

variable "subnet_ids" {
  type        = list(string)
  description = "Existing subnet IDs for the ECS service and ALB"
}

variable "s3vectors_bucket_name" {
  type        = string
  description = "S3 Vectors bucket name to create"
}

variable "s3vectors_index_name" {
  type        = string
  description = "S3 Vectors index name to create"
}

variable "tags" {
  type        = map(string)
  description = "Common tags applied to all created resources"
  default     = {}
}
