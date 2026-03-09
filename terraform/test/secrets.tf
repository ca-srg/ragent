# =============================================================================
# AWS Secrets Manager — Application & E2E Test Secrets
# =============================================================================
#
# Secret values are managed OUTSIDE Terraform (AWS Console / CLI / CI).
# Terraform only provisions the secret containers and IAM access.

# -----------------------------------------------------------------------------
# Application secrets
# -----------------------------------------------------------------------------
resource "aws_secretsmanager_secret" "app" {
  name        = "ragent/app"
  description = "RAGent application secrets (Slack tokens, OIDC, OpenSearch, etc.)"

  tags = {
    Project     = "ragent"
    Environment = "production"
    ManagedBy   = "terraform"
  }
}

# -----------------------------------------------------------------------------
# E2E test secrets
# -----------------------------------------------------------------------------
resource "aws_secretsmanager_secret" "e2e_test" {
  name        = "ragent/e2e-test"
  description = "RAGent E2E test secrets (used by GitHub Actions)"

  tags = {
    Project     = "ragent"
    Environment = "test"
    ManagedBy   = "terraform"
  }
}
