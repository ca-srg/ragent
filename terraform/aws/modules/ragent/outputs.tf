output "ec2_instance_id" {
  description = "EC2 instance ID"
  value       = try(aws_instance.ragent[0].id, null)
}

output "ec2_private_ip" {
  description = "EC2 instance private IP"
  value       = try(aws_instance.ragent[0].private_ip, null)
}

output "ecs_cluster_arn" {
  description = "ECS cluster ARN"
  value       = try(one(aws_ecs_cluster.ragent[*].arn), null)
}

output "ecs_service_name" {
  description = "ECS service name"
  value       = try(one(aws_ecs_service.ragent[*].name), null)
}

output "alb_dns_name" {
  description = "ALB DNS name"
  value       = try(aws_lb.ragent.dns_name, null)
}

output "alb_arn" {
  description = "ALB ARN"
  value       = try(aws_lb.ragent.arn, null)
}

output "mcp_endpoint" {
  description = "MCP server endpoint URL"
  value       = try(format("https://%s", aws_lb.ragent.dns_name), null)
}

output "opensearch_endpoint" {
  description = "OpenSearch domain endpoint"
  value       = try(one(aws_opensearch_domain.ragent[*].endpoint), null)
}

output "opensearch_domain_arn" {
  description = "OpenSearch domain ARN"
  value       = try(one(aws_opensearch_domain.ragent[*].arn), null)
}

output "s3vectors_bucket_name" {
  description = "S3 Vectors bucket name"
  value       = try(one(aws_s3vectors_vector_bucket.ragent[*].vector_bucket_name), null)
}

output "s3vectors_bucket_arn" {
  description = "S3 Vectors bucket ARN"
  value       = try(one(aws_s3vectors_vector_bucket.ragent[*].vector_bucket_arn), null)
}

output "s3vectors_index_name" {
  description = "S3 Vectors index name"
  value       = try(one(aws_s3vectors_index.ragent[*].index_name), null)
}

output "s3vectors_index_arn" {
  description = "S3 Vectors index ARN"
  value       = try(one(aws_s3vectors_index.ragent[*].index_arn), null)
}

output "iam_role_arn" {
  description = "IAM role ARN used by RAGent compute"
  value       = local.is_ec2 ? try(one(aws_iam_role.ragent_ec2[*].arn), null) : try(one(aws_iam_role.ragent_task[*].arn), null)
}

output "iam_role_name" {
  description = "IAM role name used by RAGent compute"
  value       = local.is_ec2 ? try(one(aws_iam_role.ragent_ec2[*].name), null) : try(one(aws_iam_role.ragent_task[*].name), null)
}

output "secrets_manager_secret_arn" {
  description = "Secrets Manager secret ARN"
  value       = aws_secretsmanager_secret.ragent.arn
}

output "secrets_manager_secret_name" {
  description = "Secrets Manager secret name"
  value       = aws_secretsmanager_secret.ragent.name
}
