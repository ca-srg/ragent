variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "ap-northeast-1"
}

variable "aws_account_id" {
  description = "AWS Account ID"
  type        = string
}

variable "github_org" {
  description = "GitHub organization name"
  type        = string
  default     = "ca-srg"
}

variable "github_repo" {
  description = "GitHub repository name"
  type        = string
  default     = "ragent"
}

variable "github_oidc_subjects" {
  description = "GitHub OIDC subject claims to allow (e.g. environment, branch, pull_request)"
  type        = list(string)
  default = [
    "repo:ca-srg/ragent:environment:e2e-test",
  ]
}

variable "s3vectors_region" {
  description = "AWS region for S3 Vectors"
  type        = string
  default     = "ap-northeast-1"
}

variable "s3vectors_bucket_name" {
  description = "S3 Vectors bucket name"
  type        = string
  default     = "ragent"
}

variable "s3vectors_index_name" {
  description = "S3 Vectors index name"
  type        = string
  default     = "ragent"
}

variable "s3_source_bucket_name" {
  description = "S3 bucket name for vectorize source files"
  type        = string
  default     = "vectorize-source"
}
