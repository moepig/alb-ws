output "alb_dns_name" {
  description = "ALB DNS name"
  value       = aws_lb.main.dns_name
}

output "ec2_public_ip" {
  description = "EC2 public IP address"
  value       = aws_instance.main.public_ip
}

output "alb_idle_timeout" {
  description = "ALB idle timeout (seconds)"
  value       = aws_lb.main.idle_timeout
}

output "websocket_url" {
  description = "WebSocket endpoint via ALB"
  value       = "ws://${aws_lb.main.dns_name}/ws"
}
