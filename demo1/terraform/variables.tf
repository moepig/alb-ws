variable "region" {
  description = "AWS region"
  type        = string
  default     = "ap-northeast-1"
}

variable "prefix" {
  description = "Resource name prefix"
  type        = string
  default     = "alb-ws-test"
}

variable "alb_idle_timeout" {
  description = "ALB idle timeout in seconds (default ALB value is 60)"
  type        = number
  default     = 60
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.micro"
}

variable "ssh_cidr_blocks" {
  description = "CIDR blocks allowed to SSH into the EC2 instance"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}
