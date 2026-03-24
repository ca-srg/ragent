variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "ap-northeast-1"
}

variable "ragent_version" {
  description = "RAGent release version (e.g. v1.0.0)"
  type        = string

  validation {
    condition     = can(regex("^v[0-9]+\\.[0-9]+\\.[0-9]+$", var.ragent_version))
    error_message = "ragent_version must be in vX.Y.Z format."
  }
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t4g.medium"
}

variable "root_volume_size" {
  description = "Root EBS volume size in GB"
  type        = number
  default     = 30
}

variable "vpc_id" {
  description = "VPC ID for RAGent resources"
  type        = string
}

variable "subnet_ids" {
  description = "Subnet IDs for compute placement"
  type        = list(string)
}

variable "secrets_manager_secret_name" {
  description = "Secrets Manager secret name"
  type        = string
  default     = "ragent/app"
}

variable "slack_bot_enabled" {
  description = "Enable Slack Bot service"
  type        = bool
  default     = true
}

variable "vectorize_enabled" {
  description = "Enable Vectorize service"
  type        = bool
  default     = true
}

variable "vectorize_s3_source_bucket" {
  description = "S3 bucket name for vectorize source files"
  type        = string
  default     = null
}

variable "vectorize_github_repos" {
  description = "Comma-separated GitHub repos for vectorize (e.g. org/repo1,org/repo2)"
  type        = string
  default     = null
}

variable "mcp_auth_method" {
  description = "Authentication method for MCP access"
  type        = string
  default     = "oidc"

  validation {
    condition     = contains(["ip", "oidc", "both", "either"], var.mcp_auth_method)
    error_message = "mcp_auth_method must be one of: ip, oidc, both, either."
  }
}

variable "mcp_bypass_ip_ranges" {
  description = "CIDR ranges to bypass MCP authentication"
  type        = list(string)
  default     = []
}

variable "ragent_env" {
  description = "Additional environment variables for RAGent"
  type        = map(string)
  default     = {}
}

variable "tags" {
  description = "Additional tags for all resources"
  type        = map(string)
  default     = {}
}
