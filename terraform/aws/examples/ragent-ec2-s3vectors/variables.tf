variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "ragent_version" {
  type        = string
  description = "RAGent release version"
}

variable "vpc_id" {
  type        = string
  description = "VPC ID"
}

variable "subnet_ids" {
  type        = list(string)
  description = "Subnet IDs"
}

variable "s3vectors_bucket_name" {
  type        = string
  description = "S3 Vectors bucket name"
}

variable "s3vectors_index_name" {
  type        = string
  description = "S3 Vectors index name"
}

variable "tags" {
  type    = map(string)
  default = {}
}
