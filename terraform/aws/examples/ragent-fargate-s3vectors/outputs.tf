output "mcp_endpoint" {
  description = "MCP endpoint exposed by the ALB"
  value       = module.ragent.mcp_endpoint
}

output "alb_dns_name" {
  description = "ALB DNS name for the deployment"
  value       = module.ragent.alb_dns_name
}

output "ecs_cluster_arn" {
  description = "ECS cluster ARN"
  value       = module.ragent.ecs_cluster_arn
}

output "ecs_service_name" {
  description = "ECS service name"
  value       = module.ragent.ecs_service_name
}

output "opensearch_endpoint" {
  description = "Managed OpenSearch endpoint"
  value       = module.ragent.opensearch_endpoint
}

output "s3vectors_bucket_name" {
  description = "Created S3 Vectors bucket name"
  value       = module.ragent.s3vectors_bucket_name
}

output "s3vectors_index_name" {
  description = "Created S3 Vectors index name"
  value       = module.ragent.s3vectors_index_name
}

output "iam_role_arn" {
  description = "IAM role ARN used by the Fargate task"
  value       = module.ragent.iam_role_arn
}

output "secrets_manager_secret_name" {
  description = "Secrets Manager secret name when enabled"
  value       = module.ragent.secrets_manager_secret_name
}
