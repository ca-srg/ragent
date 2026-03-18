resource "aws_security_group" "alb" {
  name_prefix = "ragent-alb-"
  vpc_id      = var.vpc_id
  description = "Security group for RAGent ALB"
  tags        = merge(local.common_tags, { Name = "ragent-alb" })
}

resource "aws_vpc_security_group_ingress_rule" "alb_https" {
  security_group_id = aws_security_group.alb.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
  description       = "Allow HTTPS from anywhere"
}

resource "aws_vpc_security_group_ingress_rule" "alb_http_redirect" {
  security_group_id = aws_security_group.alb.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
  description       = "Allow HTTP for HTTPS redirect"
}

resource "aws_vpc_security_group_egress_rule" "alb_to_compute" {
  security_group_id            = aws_security_group.alb.id
  referenced_security_group_id = aws_security_group.compute.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
  description                  = "Allow traffic to compute on port 8080"
}

resource "aws_security_group" "compute" {
  name_prefix = "ragent-compute-"
  vpc_id      = var.vpc_id
  description = "Security group for RAGent compute instances"
  tags        = merge(local.common_tags, { Name = "ragent-compute" })
}

resource "aws_vpc_security_group_ingress_rule" "compute_from_alb" {
  security_group_id            = aws_security_group.compute.id
  referenced_security_group_id = aws_security_group.alb.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
  description                  = "Allow traffic from ALB"
}

resource "aws_vpc_security_group_egress_rule" "compute_all" {
  security_group_id = aws_security_group.compute.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "Allow all outbound traffic"
}

resource "aws_security_group" "opensearch" {
  count       = local.is_aws_opensearch ? 1 : 0
  name_prefix = "ragent-opensearch-"
  vpc_id      = var.vpc_id
  description = "Security group for RAGent OpenSearch domain"
  tags        = merge(local.common_tags, { Name = "ragent-opensearch" })
}

resource "aws_vpc_security_group_ingress_rule" "opensearch_from_compute" {
  count                        = local.is_aws_opensearch ? 1 : 0
  security_group_id            = aws_security_group.opensearch[0].id
  referenced_security_group_id = aws_security_group.compute.id
  from_port                    = 443
  to_port                      = 443
  ip_protocol                  = "tcp"
  description                  = "Allow HTTPS from compute instances"
}
