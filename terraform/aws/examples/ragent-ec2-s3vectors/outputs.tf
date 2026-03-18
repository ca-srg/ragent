output "mcp_endpoint" {
  description = "MCP server endpoint URL"
  value       = module.ragent.mcp_endpoint
}

output "alb_dns_name" {
  description = "ALB DNS name"
  value       = module.ragent.alb_dns_name
}

output "opensearch_endpoint" {
  description = "OpenSearch domain endpoint"
  value       = module.ragent.opensearch_endpoint
}
