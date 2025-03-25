variable "region" {
  type = string
  description = "The region to create the lambda in"
}
variable "deploy_in_vpc" {
  type = bool
  description = "Whether to deploy the lambda in a VPC"
  default = false
}

variable "vpc_config" {
  type = object({
    vpc_id = string
    subnet_ids = list(string)
    security_group_ids = optional(list(string),[])
  })
  description = <<EOT
  The VPC config to create the lambda in.
  If deploy_in_vpc is false, this will be ignored.
  If deploy_in_vpc is true, this will be used to create the lambda in the VPC.

  vpc_id: The VPC to create the lambda in.
  subnet_ids: The subnet ids to create the lambda in.
  security_group_ids: (optional) The security group ids to use for the lambda. If not provided, a new security group will be created.
  EOT
  default = null
}

variable "stream_settings" {
  type = object({
    firehose_stream_name = string
    metric_stream_name = string
    s3_bucket_name = string
    metric_filters = list(object({
      namespace = string
      metric_names = list(string)
    }))
    include_linked_accounts_metrics = bool
  })
  description = <<EOT
  The settings for the stream to create.
  firehose_stream_name: The name of the firehose stream to create.
  metric_stream_name: The name of the cloudwatch metric stream to create.
  s3_bucket_name: The name of the s3 bucket to create.
  metric_filters: The list of metric filters to include in the stream.
  include_linked_accounts_metrics: Whether to include linked account metrics in the stream.
  EOT
}

variable "prometheus_settings"{
  type = object({
    lambda_name = string
    writer_endpoint = string
    role_arn = optional(string)
  })
  description = <<EOT
  The prometheus settings to use for the lambda.
  lambda_name: The name of the lambda to create.
  writer_endpoint: The prometheus remote write endpoint to write metrics
  role_arn: (optional) The role arn to assume to write to the prometheus remote write endpoints. If not provided, the lambda will use credentials from its execution role.
  EOT
}

variable "tags" {
  type        = map(string)
  description = "The standard tags to apply to every AWS resource."

  default = {}
}



