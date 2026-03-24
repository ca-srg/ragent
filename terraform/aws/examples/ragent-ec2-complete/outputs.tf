output "ec2_instance_id" {
  description = "RAGent EC2 instance ID"
  value       = module.ragent.ec2_instance_id
}

output "ec2_private_ip" {
  description = "Private IP address of the RAGent EC2 instance"
  value       = module.ragent.ec2_private_ip
}

output "alb_dns_name" {
  description = "ALB DNS name"
  value       = module.ragent.alb_dns_name
}

output "alb_arn" {
  description = "ALB ARN"
  value       = module.ragent.alb_arn
}

output "mcp_endpoint" {
  description = "MCP server endpoint URL"
  value       = module.ragent.mcp_endpoint
}

output "iam_role_arn" {
  description = "IAM role ARN used by RAGent"
  value       = module.ragent.iam_role_arn
}

output "iam_role_name" {
  description = "IAM role name used by RAGent"
  value       = module.ragent.iam_role_name
}

output "secrets_manager_secret_arn" {
  description = "Secrets Manager secret ARN"
  value       = module.ragent.secrets_manager_secret_arn
}

output "secrets_manager_secret_name" {
  description = "Secrets Manager secret name"
  value       = module.ragent.secrets_manager_secret_name
}
