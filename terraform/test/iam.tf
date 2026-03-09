# =============================================================================
# GitHub Actions OIDC — E2E Test IAM Role
# =============================================================================

# Reference existing OIDC provider (must be created once per AWS account)
data "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"
}

# -----------------------------------------------------------------------------
# IAM Role with OIDC trust policy for GitHub Actions
# -----------------------------------------------------------------------------
resource "aws_iam_role" "e2e_test" {
  name = "ragent-e2e-test-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "GitHubActionsOIDC"
        Effect = "Allow"
        Principal = {
          Federated = data.aws_iam_openid_connect_provider.github.arn
        }
        Action = "sts:AssumeRoleWithWebIdentity"
        Condition = {
          StringEquals = {
            "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
          }
          StringLike = {
            "token.actions.githubusercontent.com:sub" = var.github_oidc_subjects
          }
        }
      },
    ]
  })

  tags = {
    Project     = "ragent"
    Environment = "test"
    ManagedBy   = "terraform"
  }
}

# -----------------------------------------------------------------------------
# Bedrock — model invocation & discovery
# -----------------------------------------------------------------------------
resource "aws_iam_role_policy" "bedrock_invoke" {
  name = "AllowBedrockInvoke"
  role = aws_iam_role.e2e_test.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "BedrockInvokeAllViaProfiles"
        Effect = "Allow"
        Action = [
          "bedrock:InvokeModel",
          "bedrock:InvokeModelWithResponseStream",
          "bedrock:Converse",
          "bedrock:ConverseStream",
        ]
        Resource = [
          "arn:aws:bedrock:*::foundation-model/*",
          "arn:aws:bedrock:*:*:inference-profile/*",
        ]
      },
      {
        Sid    = "BedrockModelDiscovery"
        Effect = "Allow"
        Action = [
          "bedrock:ListFoundationModels",
          "bedrock:GetFoundationModel",
          "bedrock:GetInferenceProfile",
          "bedrock:ListInferenceProfiles",
        ]
        Resource = ["*"]
      },
    ]
  })
}

# -----------------------------------------------------------------------------
# S3 Vectors — bucket & index access
# -----------------------------------------------------------------------------
resource "aws_iam_role_policy" "s3vectors" {
  name = "AllowS3Vectors"
  role = aws_iam_role.e2e_test.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "S3VectorsBucketAccess"
        Effect = "Allow"
        Action = [
          "s3vectors:GetVectorBucket",
          "s3vectors:ListIndexes",
        ]
        Resource = [
          "arn:aws:s3vectors:${var.s3vectors_region}:${var.aws_account_id}:bucket/${var.s3vectors_bucket_name}",
        ]
      },
      {
        Sid    = "S3VectorsIndexAccess"
        Effect = "Allow"
        Action = [
          "s3vectors:GetIndex",
          "s3vectors:CreateIndex",
          "s3vectors:DeleteIndex",
          "s3vectors:PutVectors",
          "s3vectors:DeleteVectors",
          "s3vectors:ListVectors",
          "s3vectors:GetVectors",
          "s3vectors:QueryVectors",
        ]
        Resource = [
          "arn:aws:s3vectors:${var.s3vectors_region}:${var.aws_account_id}:bucket/${var.s3vectors_bucket_name}/index/${var.s3vectors_index_name}",
        ]
      },
    ]
  })
}

# -----------------------------------------------------------------------------
# S3 — vectorize source bucket read access
# -----------------------------------------------------------------------------
resource "aws_iam_role_policy" "s3_vectorize_source_read" {
  name = "AllowS3VectorizeSourceRead"
  role = aws_iam_role.e2e_test.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "ListVectorizeSourceBucket"
        Effect = "Allow"
        Action = [
          "s3:ListBucket",
        ]
        Resource = [
          "arn:aws:s3:::${var.s3_source_bucket_name}",
        ]
      },
      {
        Sid    = "GetObjectFromVectorizeSource"
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:HeadObject",
        ]
        Resource = [
          "arn:aws:s3:::${var.s3_source_bucket_name}/*",
        ]
      },
    ]
  })
}

# -----------------------------------------------------------------------------
# Secrets Manager — read access to E2E test secrets
# -----------------------------------------------------------------------------
resource "aws_iam_role_policy" "secrets_manager_read" {
  name = "AllowSecretsManagerRead"
  role = aws_iam_role.e2e_test.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "SecretsManagerGetValue"
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret",
        ]
        Resource = [
          aws_secretsmanager_secret.e2e_test.arn,
        ]
      },
    ]
  })
}
