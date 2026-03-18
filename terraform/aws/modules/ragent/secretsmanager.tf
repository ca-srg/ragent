resource "aws_secretsmanager_secret" "ragent" {
  name        = var.secrets_manager_secret_name
  description = "RAGent application secrets (populate via AWS CLI or Console)"

  tags = merge(local.common_tags, {
    Name = var.secrets_manager_secret_name
  })
}

resource "aws_secretsmanager_secret_version" "ragent" {
  secret_id     = aws_secretsmanager_secret.ragent.id
  secret_string = jsonencode(local.sm_seed_env)

  lifecycle {
    ignore_changes = [secret_string]
  }
}
