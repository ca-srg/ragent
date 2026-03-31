resource "aws_lb" "ragent" {
  name_prefix        = local.alb_name_prefix
  internal           = var.alb_internal
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = var.subnet_ids

  tags = merge(local.common_tags, { Name = "${var.name_prefix}-alb" })
}

resource "aws_lb_target_group" "ragent_mcp" {
  name_prefix = local.alb_name_prefix
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = local.is_ec2 ? "instance" : "ip"

  health_check {
    path                = "/health"
    port                = "8080"
    protocol            = "HTTP"
    matcher             = "200,401"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  deregistration_delay = 300

  tags = merge(local.common_tags, { Name = "${var.name_prefix}-mcp" })
}

resource "aws_lb_listener" "ragent_https" {
  load_balancer_arn = aws_lb.ragent.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = var.certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.ragent_mcp.arn
  }

  tags = merge(local.common_tags, { Name = "${var.name_prefix}-https" })
}

resource "aws_lb_listener" "ragent_http_redirect" {
  load_balancer_arn = aws_lb.ragent.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"

    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }

  tags = merge(local.common_tags, { Name = "${var.name_prefix}-http-redirect" })
}

resource "aws_lb_target_group_attachment" "ragent_ec2" {
  count            = local.is_ec2 ? 1 : 0
  target_group_arn = aws_lb_target_group.ragent_mcp.arn
  target_id        = aws_instance.ragent[0].id
  port             = 8080
}
