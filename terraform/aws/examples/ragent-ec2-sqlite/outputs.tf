output "ec2_instance_id" {
  description = "EC2 instance ID for the RAGent deployment"
  value       = module.ragent.ec2_instance_id
}

output "ec2_private_ip" {
  description = "Private IP address of the RAGent EC2 instance"
  value       = module.ragent.ec2_private_ip
}

output "alb_dns_name" {
  description = "Internal ALB DNS name for MCP access"
  value       = module.ragent.alb_dns_name
}

output "mcp_endpoint" {
  description = "HTTP endpoint for the MCP server"
  value       = module.ragent.mcp_endpoint
}

output "iam_role_arn" {
  description = "IAM role ARN attached to the EC2 instance"
  value       = module.ragent.iam_role_arn
}

output "iam_role_name" {
  description = "IAM role name attached to the EC2 instance"
  value       = module.ragent.iam_role_name
}
