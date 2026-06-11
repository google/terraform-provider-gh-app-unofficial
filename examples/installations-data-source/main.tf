terraform {
  required_providers {
    ghapp = {
      source = "registry.terraform.io/google/gh-app-unofficial"
    }
  }
}

variable "enterprise_slug" {
  type        = string
  description = "The enterprise slug."
  default     = "example-enterprise"
}

variable "target_org" {
  type        = string
  description = "The target GitHub organization slug to list installations from."
  default     = "example-org"
}

provider "ghapp" {
  enterprise_slug = var.enterprise_slug
}

data "ghapp_installations" "example" {
  target_org = var.target_org
}

output "installations" {
  value = data.ghapp_installations.example.installations
}
