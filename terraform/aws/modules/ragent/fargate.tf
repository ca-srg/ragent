resource "aws_ecs_cluster" "ragent" {
  count = local.is_fargate ? 1 : 0
  name  = var.name_prefix
  tags  = merge(local.common_tags, { Name = "${var.name_prefix}-cluster" })
}

resource "aws_cloudwatch_log_group" "ragent" {
  count             = local.is_fargate ? 1 : 0
  name              = "/ecs/${var.name_prefix}"
  retention_in_days = 30
  tags              = local.common_tags
}

resource "aws_ecs_task_definition" "ragent" {
  count                    = local.is_fargate ? 1 : 0
  family                   = var.name_prefix
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.cpu
  memory                   = var.memory
  execution_role_arn       = aws_iam_role.ragent_execution[0].arn
  task_role_arn            = aws_iam_role.ragent_task[0].arn

  container_definitions = jsonencode(concat(
    [
      {
        name      = "${var.name_prefix}-mcp"
        image     = var.container_image_uri
        essential = true
        portMappings = [
          {
            containerPort = 8080
            protocol      = "tcp"
          }
        ]
        command = concat(
          ["mcp-server", "--host", "0.0.0.0", "--auth-method", var.mcp_auth_method],
          flatten([for cidr in var.mcp_bypass_ip_ranges : ["--bypass-ip-range", cidr]])
        )
        environment = [for k, v in local.sm_bootstrap_env : { name = k, value = v }]
        logConfiguration = {
          logDriver = "awslogs"
          options = {
            "awslogs-group"         = aws_cloudwatch_log_group.ragent[0].name
            "awslogs-region"        = data.aws_region.current.region
            "awslogs-stream-prefix" = "mcp"
          }
        }
        healthCheck = {
          command     = ["CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"]
          interval    = 30
          timeout     = 5
          retries     = 3
          startPeriod = 60
        }
      }
    ],
    var.slack_bot_enabled ? [
      {
        name        = "${var.name_prefix}-slack"
        image       = var.container_image_uri
        essential   = false
        command     = ["slack-bot", "--context-size", "10"]
        environment = [for k, v in local.sm_bootstrap_env : { name = k, value = v }]
        logConfiguration = {
          logDriver = "awslogs"
          options = {
            "awslogs-group"         = aws_cloudwatch_log_group.ragent[0].name
            "awslogs-region"        = data.aws_region.current.region
            "awslogs-stream-prefix" = "slack"
          }
        }
      }
    ] : [],
    var.vectorize_enabled ? [
      {
        name      = "${var.name_prefix}-vectorize"
        image     = var.container_image_uri
        essential = false
        command = concat(
          ["vectorize"],
          var.vectorize_s3_source_bucket != null ? ["--enable-s3", "--s3-bucket", var.vectorize_s3_source_bucket] : [],
          var.vectorize_github_repos != null ? ["--github-repos", var.vectorize_github_repos] : [],
          ["--follow"]
        )
        environment = [for k, v in local.sm_bootstrap_env : { name = k, value = v }]
        logConfiguration = {
          logDriver = "awslogs"
          options = {
            "awslogs-group"         = aws_cloudwatch_log_group.ragent[0].name
            "awslogs-region"        = data.aws_region.current.region
            "awslogs-stream-prefix" = "vectorize"
          }
        }
      }
    ] : []
  ))

  tags = merge(local.common_tags, { Name = "${var.name_prefix}-task" })
}

resource "aws_ecs_service" "ragent" {
  count           = local.is_fargate ? 1 : 0
  name            = var.name_prefix
  cluster         = aws_ecs_cluster.ragent[0].id
  task_definition = aws_ecs_task_definition.ragent[0].arn
  desired_count   = var.desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = var.subnet_ids
    security_groups  = [aws_security_group.compute.id]
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.ragent_mcp.arn
    container_name   = "${var.name_prefix}-mcp"
    container_port   = 8080
  }

  tags = merge(local.common_tags, { Name = "${var.name_prefix}-service" })
}
