terraform {
  required_providers {
    ghapp = {
      source = "registry.terraform.io/google/gh-app-unofficial"
    }
  }
}

variable "target_org" {
  type        = string
  description = "The target GitHub organization slug to list installations from."
  default     = "my-org-slug"
}

provider "ghapp" {}

data "ghapp_installations" "all" {
  target_org = var.target_org
}
