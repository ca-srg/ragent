variable "compute_type" {
  type        = string
  default     = "ec2"
  description = "Compute platform for RAGent deployment"

  validation {
    condition     = contains(["ec2", "fargate"], var.compute_type)
    error_message = "compute_type must be one of: ec2, fargate."
  }
}

variable "vector_backend" {
  type        = string
  default     = "s3"
  description = "Vector storage backend for RAGent"

  validation {
    condition     = contains(["s3", "sqlite"], var.vector_backend)
    error_message = "vector_backend must be one of: s3, sqlite."
  }
}

variable "opensearch_mode" {
  type        = string
  default     = "aws"
  description = "How OpenSearch is provided to RAGent"

  validation {
    condition     = contains(["aws", "docker"], var.opensearch_mode)
    error_message = "opensearch_mode must be one of: aws, docker."
  }
}

variable "vpc_id" {
  type        = string
  description = "VPC ID for RAGent resources"

  validation {
    condition     = trimspace(var.vpc_id) != ""
    error_message = "vpc_id must not be empty."
  }
}

variable "subnet_ids" {
  type        = list(string)
  description = "Compute subnet IDs"

  validation {
    condition = length(var.subnet_ids) > 0 && alltrue([
      for subnet_id in var.subnet_ids : trimspace(subnet_id) != ""
    ])
    error_message = "subnet_ids must contain at least one non-empty subnet ID."
  }
}

variable "opensearch_subnet_ids" {
  type        = list(string)
  default     = []
  description = "OpenSearch subnet IDs. If empty, uses first subnet from subnet_ids"

  validation {
    condition = alltrue([
      for subnet_id in var.opensearch_subnet_ids : trimspace(subnet_id) != ""
    ])
    error_message = "opensearch_subnet_ids must contain only non-empty subnet IDs."
  }
}

variable "instance_type" {
  type        = string
  default     = "t4g.medium"
  description = "EC2 instance type for RAGent"

  validation {
    condition     = trimspace(var.instance_type) != ""
    error_message = "instance_type must not be empty."
  }
}

variable "ami_id" {
  type        = string
  default     = null
  description = "AMI ID. If null, uses latest AL2023 ARM64"

  validation {
    condition     = var.ami_id == null || trimspace(var.ami_id) != ""
    error_message = "ami_id must be null or a non-empty string."
  }
}

variable "key_name" {
  type        = string
  default     = null
  description = "EC2 key pair name for SSH access"

  validation {
    condition     = var.key_name == null || trimspace(var.key_name) != ""
    error_message = "key_name must be null or a non-empty string."
  }
}

variable "ragent_version" {
  type        = string
  description = "RAGent release version e.g. v1.0.0"

  validation {
    condition     = trimspace(var.ragent_version) != ""
    error_message = "ragent_version must not be empty."
  }
}

variable "root_volume_size" {
  type        = number
  default     = 30
  description = "Root EBS volume size in GB"

  validation {
    condition     = var.root_volume_size > 0
    error_message = "root_volume_size must be greater than 0."
  }
}

variable "container_image_uri" {
  type        = string
  default     = null
  description = "Container image URI, required when compute_type is fargate"

  validation {
    condition     = var.container_image_uri == null || trimspace(var.container_image_uri) != ""
    error_message = "container_image_uri must be null or a non-empty string."
  }
}

variable "cpu" {
  type        = number
  default     = 1024
  description = "Fargate task CPU units"

  validation {
    condition     = var.cpu > 0
    error_message = "cpu must be greater than 0."
  }
}

variable "memory" {
  type        = number
  default     = 2048
  description = "Fargate task memory in MiB"

  validation {
    condition     = var.memory > 0
    error_message = "memory must be greater than 0."
  }
}

variable "desired_count" {
  type        = number
  default     = 1
  description = "Number of Fargate tasks"

  validation {
    condition     = var.desired_count > 0
    error_message = "desired_count must be greater than 0."
  }
}

variable "opensearch_domain_name" {
  type        = string
  default     = "ragent"
  description = "OpenSearch domain name"

  validation {
    condition     = trimspace(var.opensearch_domain_name) != ""
    error_message = "opensearch_domain_name must not be empty."
  }
}

variable "opensearch_engine_version" {
  type        = string
  default     = "OpenSearch_2.19"
  description = "OpenSearch engine version"

  validation {
    condition     = trimspace(var.opensearch_engine_version) != ""
    error_message = "opensearch_engine_version must not be empty."
  }
}

variable "opensearch_instance_type" {
  type        = string
  default     = "or2.medium.search"
  description = "OpenSearch instance type"

  validation {
    condition     = trimspace(var.opensearch_instance_type) != ""
    error_message = "opensearch_instance_type must not be empty."
  }
}

variable "opensearch_volume_size" {
  type        = number
  default     = 100
  description = "EBS volume size in GB for OpenSearch"

  validation {
    condition     = var.opensearch_volume_size > 0
    error_message = "opensearch_volume_size must be greater than 0."
  }
}

variable "opensearch_embedding_dimension" {
  type        = number
  default     = 1024
  description = "Vector embedding dimension for OpenSearch knn_vector field. Must match the embedding model output dimension."

  validation {
    condition     = var.opensearch_embedding_dimension > 0 && var.opensearch_embedding_dimension <= 16384
    error_message = "opensearch_embedding_dimension must be between 1 and 16384."
  }
}

variable "s3vectors_bucket_name" {
  type        = string
  default     = null
  description = "S3 Vectors bucket name, required when vector_backend is s3"

  validation {
    condition     = var.s3vectors_bucket_name == null || trimspace(var.s3vectors_bucket_name) != ""
    error_message = "s3vectors_bucket_name must be null or a non-empty string."
  }
}

variable "s3vectors_index_name" {
  type        = string
  default     = null
  description = "S3 Vectors index name, required when vector_backend is s3"

  validation {
    condition     = var.s3vectors_index_name == null || trimspace(var.s3vectors_index_name) != ""
    error_message = "s3vectors_index_name must be null or a non-empty string."
  }
}

variable "s3vectors_region" {
  type        = string
  default     = "us-east-1"
  description = "AWS region for S3 Vectors"

  validation {
    condition     = trimspace(var.s3vectors_region) != ""
    error_message = "s3vectors_region must not be empty."
  }
}

variable "s3vectors_dimension" {
  type        = number
  default     = 1024
  description = "Vector embedding dimension"

  validation {
    condition     = var.s3vectors_dimension > 0
    error_message = "s3vectors_dimension must be greater than 0."
  }
}

variable "secrets_manager_secret_name" {
  type        = string
  default     = "ragent/app"
  description = "Secrets Manager secret name"

  validation {
    condition     = trimspace(var.secrets_manager_secret_name) != ""
    error_message = "secrets_manager_secret_name must not be empty."
  }
}

variable "secrets_manager_region" {
  type        = string
  default     = null
  description = "SM region. If null, uses provider default region"

  validation {
    condition     = var.secrets_manager_region == null || trimspace(var.secrets_manager_region) != ""
    error_message = "secrets_manager_region must be null or a non-empty string."
  }
}

variable "ragent_env" {
  type        = map(string)
  default     = {}
  description = "Additional environment variables for RAGent"

  validation {
    condition = alltrue([
      for key, value in var.ragent_env : trimspace(key) != "" && trimspace(value) != ""
    ])
    error_message = "ragent_env keys and values must be non-empty strings."
  }
}

variable "opensearch_index_name" {
  type        = string
  default     = "ragent"
  description = "OpenSearch index name for RAGent"

  validation {
    condition     = trimspace(var.opensearch_index_name) != ""
    error_message = "opensearch_index_name must not be empty."
  }
}

variable "bedrock_region" {
  type        = string
  default     = "us-east-1"
  description = "AWS region for Bedrock"

  validation {
    condition     = trimspace(var.bedrock_region) != ""
    error_message = "bedrock_region must not be empty."
  }
}

variable "embedding_provider" {
  type        = string
  default     = "bedrock"
  description = "Embedding provider used by RAGent"

  validation {
    condition     = contains(["bedrock", "gemini"], var.embedding_provider)
    error_message = "embedding_provider must be one of: bedrock, gemini."
  }
}

variable "slack_bot_enabled" {
  type        = bool
  default     = true
  description = "Enable Slack Bot service"

  validation {
    condition     = contains([true, false], var.slack_bot_enabled)
    error_message = "slack_bot_enabled must be a boolean value."
  }
}

variable "vectorize_enabled" {
  type        = bool
  default     = true
  description = "Enable Vectorize service"

  validation {
    condition     = contains([true, false], var.vectorize_enabled)
    error_message = "vectorize_enabled must be a boolean value."
  }
}

variable "vectorize_s3_source_bucket" {
  type        = string
  default     = null
  description = "S3 bucket for vectorize source files"

  validation {
    condition     = var.vectorize_s3_source_bucket == null || trimspace(var.vectorize_s3_source_bucket) != ""
    error_message = "vectorize_s3_source_bucket must be null or a non-empty string."
  }
}

variable "vectorize_github_repos" {
  type        = string
  default     = null
  description = "Comma-separated GitHub repos for vectorize"

  validation {
    condition     = var.vectorize_github_repos == null || trimspace(var.vectorize_github_repos) != ""
    error_message = "vectorize_github_repos must be null or a non-empty string."
  }
}

variable "mcp_auth_method" {
  type        = string
  default     = "oidc"
  description = "Authentication mode for MCP access"

  validation {
    condition     = contains(["ip", "oidc", "both", "either"], var.mcp_auth_method)
    error_message = "mcp_auth_method must be one of: ip, oidc, both, either."
  }
}

variable "mcp_bypass_ip_ranges" {
  type        = list(string)
  default     = []
  description = "CIDR ranges to bypass MCP auth"

  validation {
    condition = alltrue([
      for cidr in var.mcp_bypass_ip_ranges : trimspace(cidr) != ""
    ])
    error_message = "mcp_bypass_ip_ranges must contain only non-empty CIDR strings."
  }
}

variable "alb_internal" {
  type        = bool
  default     = true
  description = "Set to true for an internal (private) ALB, false for an internet-facing ALB"
}

variable "certificate_arn" {
  type        = string
  description = "ACM certificate ARN for ALB HTTPS listener"

  validation {
    condition     = trimspace(var.certificate_arn) != ""
    error_message = "certificate_arn must not be empty."
  }
}

variable "systemd_service_overrides" {
  type        = map(string)
  default     = {}
  description = "Systemd drop-in override content per service unit. Key is the service name without '.service' suffix (e.g., 'ragent-mcp'), value is the override.conf content. Overrides are placed in /etc/systemd/system/<key>.service.d/override.conf"

  validation {
    condition = alltrue([
      for key, value in var.systemd_service_overrides : trimspace(key) != "" && trimspace(value) != ""
    ])
    error_message = "systemd_service_overrides keys and values must be non-empty strings."
  }
}

variable "tags" {
  type        = map(string)
  default     = {}
  description = "Additional tags for all resources"

  validation {
    condition = alltrue([
      for key, value in var.tags : trimspace(key) != "" && value != null
    ])
    error_message = "tags keys must be non-empty strings."
  }
}
