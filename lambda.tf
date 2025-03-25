resource "aws_lambda_function" "cloudwatch_metrics_firehose_prometheus_remote_write" {
  filename         = "${path.module}/lambda_code/payload.zip"
  source_code_hash = filebase64sha256("${path.module}/lambda_code/payload.zip")
  function_name    = var.stream_settings.lambda_name
  role             = aws_iam_role.iam_for_lambda.arn
  handler          = "main"
  timeout          = 60
  memory_size      = 256

  runtime = "provided.al2023"

  dynamic "vpc_config" {
    for_each = var.deploy_in_vpc ? [var.prometheus_settings.vpc_config] : []
    content {
      subnet_ids = vpc_config.value.subnet_ids
      security_group_ids = [length(vpc_config.value.security_group_ids) > 0 ? vpc_config.value.security_group_ids : [aws_security_group.cloudwatch_metrics_firehose_prometheus_remote_write.id]]
    }
  }

  environment {
    variables = {
      PROMETHEUS_REMOTE_WRITE_URL = var.stream_settings.prometheus_settings.writer_endpoint
      AWS_AMP_ROLE_ARN = var.stream_settings.prometheus_settings.role_arn
      PROMETHEUS_REGION = var.region
    }
  }

  tags = var.tags
}

resource "aws_security_group" "this" {
  count = var.deploy_in_vpc && length(try(var.vpc_config.security_group_ids, [])) == 0 ? 1 : 0
  name   = "${var.stream_settings.lambda_name}-security-group"
  vpc_id = var.vpc_config.vpc_id

  tags = var.tags
}

resource "aws_security_group_rule" "this" {
  count = var.deploy_in_vpc && length(try(var.vpc_config.security_group_ids, [])) == 0 ? 1 : 0
  security_group_id = aws_security_group.this[0].id
  type              = "egress"
  from_port         = 0
  to_port           = 0
  protocol          = "all"
  cidr_blocks       = ["0.0.0.0/0"]
}


resource "aws_iam_role" "iam_for_lambda" {
  name = "${var.stream_settings.lambda_name}-lambda-role"

  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Principal": {
        "Service": "lambda.amazonaws.com"
      },
      "Effect": "Allow",
      "Sid": ""
    }
  ]
}
EOF

  tags = var.tags
}

data "aws_iam_policy" "lambda_basic_execution_role_policy_vpc" {
  count = var.deploy_in_vpc ? 1 : 0
  arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
  tags = var.tags
}

data "aws_iam_policy_document" "assume_cross_account_role_policy" {
  statement {
    effect = "Allow"
    actions = ["sts:AssumeRole"]
    resources = [var.stream_settings.prometheus_settings.role_arn]
  }
}

resource "aws_iam_role_policy_attachment" "assume_cross_account_role" {
  role       = aws_iam_role.iam_for_lambda.name
  policy_arn = aws_iam_policy.assume_cross_account_policy.arn
}

resource "aws_iam_policy" "assume_cross_account_policy" {
  name = "${var.stream_settings.lambda_name}-assume-cross-account"
  policy = data.aws_iam_policy_document.assume_cross_account_role_policy.json
  tags = merge(var.tags, {
    Name = "${var.stream_settings.lambda_name}-assume-cross-account"
  })
}

data "aws_iam_policy" "lambda_basic_execution_role_policy" {
  arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"

  tags = var.tags
}
data "aws_iam_policy" "aps_remote_write_policy" {
  arn = "arn:aws:iam::aws:policy/AmazonPrometheusRemoteWriteAccess"

}

resource "aws_iam_role_policy_attachment" "vpc" {
  count = var.deploy_in_vpc ? 1 : 0
  role       = aws_iam_role.iam_for_lambda.name
  policy_arn = data.aws_iam_policy.lambda_basic_execution_role_policy_vpc.arn
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.iam_for_lambda.name
  policy_arn = data.aws_iam_policy.lambda_basic_execution_role_policy.arn
}
resource "aws_iam_role_policy_attachment" "aps" {
  role       = aws_iam_role.iam_for_lambda.name
  policy_arn = data.aws_iam_policy.aps_remote_write_policy.arn
}


resource "aws_cloudwatch_log_group" "logs" {
  name              = "/aws/lambda/${aws_lambda_function.cloudwatch_metrics_firehose_prometheus_remote_write.function_name}"
  retention_in_days = 30
  tags = var.tags
}
