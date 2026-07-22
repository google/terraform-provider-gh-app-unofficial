terraform {
  required_providers {
    gh-app-unofficial = {
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

variable "client_id" {
  type        = string
  description = "The client ID of the app."
  default     = "Iv1.0123456789abcdef"
}

variable "repository_selection" {
  type        = string
  description = "The type of repository selection for the app installation. Can be 'all' or 'selected'."
  default     = "all"
}

variable "selected_repositories" {
  type        = set(string)
  description = "The list of repository names the installation has access to. Required when repository_selection is 'selected'."
  default     = null
}

provider "gh-app-unofficial" {
  enterprise_slug = var.enterprise_slug
}

resource "gh-app-unofficial_installation" "test-app" {
  target_org            = var.target_org
  client_id             = var.client_id
  repository_selection  = var.repository_selection
  selected_repositories = var.selected_repositories
}

output "installation" {
  value = gh-app-unofficial_installation.test-app
}
