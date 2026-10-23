terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}
provider "aws" {
  region              = "us-east-1"
  profile             = var.aws_profile
  allowed_account_ids = ["216577824627"]
}
variable "aws_profile" {
  type    = string
  default = "unsup-nonprod-admin"
}
variable "existing_node_security_group_ids" {
  type        = set(string)
  description = "Verified SGs on existing ingress-nginx worker nodes; only signaling TCP7880 admitted."
  validation {
    condition     = length(var.existing_node_security_group_ids) > 0
    error_message = "Discover and supply the actual existing ingress worker SG IDs before planning."
  }
}
variable "cluster_name" { type = string }
variable "public_subnet_ids" {
  type        = set(string)
  description = "Existing public subnets with IGW routes AND MapPublicIpOnLaunch=true."
  validation {
    condition     = length(var.public_subnet_ids) > 0
    error_message = "Supply at least one verified public subnet."
  }
}
data "aws_eks_cluster" "existing" { name = var.cluster_name }
data "aws_subnet" "public" {
  for_each = var.public_subnet_ids
  id       = each.value
}
# hostNetwork can retrieve this role from IMDS; never reuse the general worker role.
# Current aws-node has no workload identity. Its CNI permissions remain on this
# dedicated role until a separately reviewed cluster identity migration.
resource "aws_iam_role" "media" {
  name = "voice-preview-media-node"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}
resource "aws_iam_role_policy_attachment" "media" {
  for_each = toset([
    "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
    "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryPullOnly",
    "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
  ])
  role       = aws_iam_role.media.name
  policy_arn = each.value
}
resource "aws_security_group" "media" {
  name_prefix = "voice-preview-media-"
  description = "Dedicated LiveKit public node media only"
  vpc_id      = data.aws_eks_cluster.existing.vpc_config[0].vpc_id
  # Cluster SG is attached separately, preserving existing node/control-plane connectivity.
  dynamic "ingress" {
    for_each = { turn_tls = ["tcp", 443], ice_tcp = ["tcp", 7881], ice_udp = ["udp", 7882], turn_udp = ["udp", 3478] }
    content {
      description = ingress.key
      protocol    = ingress.value[0]
      from_port   = tonumber(ingress.value[1])
      to_port     = tonumber(ingress.value[1])
      cidr_blocks = ["0.0.0.0/0"]
    }
  }
  dynamic "ingress" {
    for_each = var.existing_node_security_group_ids
    iterator = signaling
    content {
      description     = "Private signaling from verified ingress worker node SG"
      protocol        = "tcp"
      from_port       = 7880
      to_port         = 7880
      security_groups = [signaling.value]
    }
  }
  egress {
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = ["0.0.0.0/0"]
  }
}
resource "aws_launch_template" "media" {
  name_prefix = "voice-preview-media-"
  vpc_security_group_ids = [
    data.aws_eks_cluster.existing.vpc_config[0].cluster_security_group_id,
    aws_security_group.media.id,
  ]
  metadata_options {
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }
  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      encrypted   = true
      volume_size = 30
      volume_type = "gp3"
    }
  }
}
resource "aws_eks_node_group" "media" {
  depends_on      = [aws_iam_role_policy_attachment.media]
  cluster_name    = var.cluster_name
  node_group_name = "voice-preview-public-media"
  node_role_arn   = aws_iam_role.media.arn
  subnet_ids      = var.public_subnet_ids
  instance_types  = ["c6i.large"]
  ami_type        = "AL2023_x86_64_STANDARD"
  capacity_type   = "ON_DEMAND"
  labels          = { "aioutfitter.com/voice-media" = "public" }
  taint {
    key    = "aioutfitter.com/voice-media"
    value  = "public"
    effect = "NO_SCHEDULE"
  }
  scaling_config {
    desired_size = 1
    min_size     = 1
    max_size     = 1
  }
  launch_template {
    id      = aws_launch_template.media.id
    version = aws_launch_template.media.latest_version
  }
  lifecycle {
    precondition {
      condition     = alltrue([for s in data.aws_subnet.public : s.map_public_ip_on_launch && s.vpc_id == data.aws_eks_cluster.existing.vpc_config[0].vpc_id])
      error_message = "Media subnets must belong to this cluster VPC and auto-assign public IPv4 addresses. Also verify their IGW route before apply."
    }
  }
}
output "node_group_name" { value = aws_eks_node_group.media.node_group_name }
