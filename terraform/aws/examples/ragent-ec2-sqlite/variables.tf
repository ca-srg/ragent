variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "ragent_version" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "subnet_ids" {
  type = list(string)
}

variable "tags" {
  type    = map(string)
  default = {}
}
